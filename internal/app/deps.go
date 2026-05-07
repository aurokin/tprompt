package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	applife "github.com/hsadler/tprompt/internal/app/lifecycle"
	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	dlife "github.com/hsadler/tprompt/internal/daemon/lifecycle"
	"github.com/hsadler/tprompt/internal/picker"
	"github.com/hsadler/tprompt/internal/promptsource"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/submitter"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
)

// DaemonLauncher is the seam used by `daemon start` and TUI implicit
// auto-start. Production wires applife.Launcher; tests inject fakes.
type DaemonLauncher interface {
	Start(ctx context.Context, intent applife.StartIntent) dlife.StartResult
}

// Deps provides the capabilities that CLI handlers need. Production code
// supplies real implementations; tests inject fakes. Lazy factory functions
// let each handler pay only for what it uses.
type Deps struct {
	Stdout   io.Writer
	Stderr   io.Writer
	Stdin    io.Reader
	Env      func(string) string
	LookPath func(string) (string, error)

	ConfigPath               *string
	LoadConfig               func(explicitPath string) (config.Resolved, error)
	LoadPasteConfig          func(explicitPath string) (config.Resolved, error)
	LoadDaemonConfig         func(explicitPath string) (config.Resolved, error)
	NewStore                 func(cfg config.Resolved) (store.Store, error)
	NewTmux                  func() (tmux.Adapter, error)
	NewClip                  func(cfg config.Resolved) (clipboard.Reader, error)
	NewPicker                func(cfg config.Resolved) (picker.Picker, error)
	NewDaemonClient          func(cfg config.Resolved) (daemon.Client, error)
	NewDaemonReadinessClient func(cfg config.Resolved, timeout time.Duration) daemon.Client
	NewLauncher              func(cfg config.Resolved, explicitConfigPath string) DaemonLauncher
	NewRenderer              func(cfg config.Resolved, prompts store.Store, sub submitter.Submitter) (tui.Renderer, error)
	NewSubmitter             func(cfg config.Resolved, prompts store.Store, client daemon.Client, target tmux.TargetContext) submitter.Submitter
}

// ProductionDeps returns a Deps wired for real execution.
func ProductionDeps(stdout, stderr io.Writer, stdin io.Reader) Deps {
	return Deps{
		Stdout:           stdout,
		Stderr:           stderr,
		Stdin:            stdin,
		Env:              lookupEnv,
		LookPath:         exec.LookPath,
		LoadConfig:       productionLoadConfig,
		LoadPasteConfig:  productionLoadPasteConfig,
		LoadDaemonConfig: productionLoadDaemonConfig,
		NewStore: func(cfg config.Resolved) (store.Store, error) {
			sources, err := promptSources(cfg)
			if err != nil {
				return nil, err
			}
			return store.NewMultiSourceWithPolicy(
				sources,
				store.ConflictPolicy(activePromptPriority(cfg)),
				cfg.ReservedPrintable,
				cfg.KeybindPool,
			), nil
		},
		NewTmux: func() (tmux.Adapter, error) {
			return tmux.New(tmux.NewExecRunner("")), nil
		},
		NewClip: func(cfg config.Resolved) (clipboard.Reader, error) {
			return newClipboardReader(cfg, lookupEnv)
		},
		NewPicker: func(cfg config.Resolved) (picker.Picker, error) {
			return picker.NewCommand(cfg.PickerArgv), nil
		},
		NewDaemonClient: func(cfg config.Resolved) (daemon.Client, error) {
			return daemon.NewSocketClient(cfg.SocketPath), nil
		},
		NewDaemonReadinessClient: productionNewDaemonReadinessClient,
		NewLauncher:              productionNewLauncher,
		NewRenderer: func(cfg config.Resolved, prompts store.Store, sub submitter.Submitter) (tui.Renderer, error) {
			// Stub renderers (TPROMPT_TEST_RENDERER) never touch the real
			// clipboard, so build the Reader only for the production path.
			// Otherwise hosts without pbpaste/wl-paste/xclip/xsel would fail
			// startup for every stub-renderer testscript.
			if spec := lookupEnv("TPROMPT_TEST_RENDERER"); spec != "" {
				return parseTestRenderer(spec, sub)
			}
			var clip clipboard.Reader
			if c, err := newClipboardReader(cfg, lookupEnv); err == nil {
				// Clipboard selection is recoverable inside the TUI. If no
				// reader is available, leave Clip nil and let the Model show
				// an inline error only when the user selects clipboard.
				clip = c
			}
			return tui.NewRenderer(tui.ModelDeps{
				Submitter:     sub,
				Clip:          clip,
				Store:         prompts,
				MaxPasteBytes: cfg.MaxPasteBytes,
			}, tui.ProgramIO{
				Input:  stdin,
				Output: stdout,
			}), nil
		},
		NewSubmitter: func(cfg config.Resolved, prompts store.Store, client daemon.Client, target tmux.TargetContext) submitter.Submitter {
			return submitter.New(prompts, client, cfg, target)
		},
	}
}

func productionNewDaemonReadinessClient(cfg config.Resolved, timeout time.Duration) daemon.Client {
	dialTimeout := daemon.DefaultDialTimeout
	if timeout > 0 && timeout < dialTimeout {
		dialTimeout = timeout
	}
	return daemon.NewSocketClientWithTimeouts(cfg.SocketPath, dialTimeout, timeout)
}

// productionNewLauncher wires applife.Launcher with the production
// status prober (a fresh socket client per probe with a tight readiness
// timeout), the production spawner (setsid + stderr → daemon log), and a
// pre-spawn diagnostic appender that writes a single logfmt line to the
// daemon log so spawn-time failures still leave evidence.
func productionNewLauncher(cfg config.Resolved, explicitConfigPath string) DaemonLauncher {
	executable, _ := os.Executable()
	return applife.New(applife.Options{
		SocketPath: cfg.SocketPath,
		LogPath:    cfg.LogPath,
		ConfigPath: explicitConfigPath,
		Executable: executable,
		Status: productionStatusProber{
			socketPath:  cfg.SocketPath,
			dialTimeout: daemon.DefaultDialTimeout,
			rpcTimeout:  500 * time.Millisecond,
			constructor: daemon.NewSocketClientWithTimeouts,
		},
		Spawner:  applife.ProductionSpawner{},
		Assessor: applife.ProductionAssessor(),
		LogPreSpawn: func(line string) {
			if cfg.LogPath == "" {
				return
			}
			// First-run spawns: the daemon log's parent dir may not
			// exist yet (daemon.NewLogger normally creates it). Match
			// that behavior here so the diagnostic isn't silently lost
			// on the user's very first start, which is exactly when
			// they're most likely to need it.
			if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o700); err != nil {
				return
			}
			f, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return
			}
			defer func() { _ = f.Close() }()
			_, _ = fmt.Fprintf(f, "time=%s %s\n", time.Now().UTC().Format(time.RFC3339Nano), line)
		},
	})
}

// productionStatusProber dials the daemon socket once per probe with
// short timeouts and runs the Status RPC. ProbeOK on success;
// ProbeUnreachable when the dial step itself failed (no socket, ENOENT,
// ECONNREFUSED); ProbeReachableBroken when the dial succeeded but the
// RPC could not complete — that is, some process is bound but cannot
// answer Status, and the launcher must NOT respawn over it.
type productionStatusProber struct {
	socketPath  string
	dialTimeout time.Duration
	rpcTimeout  time.Duration
	constructor func(path string, dial, rpc time.Duration) daemon.Client
}

func (p productionStatusProber) Probe(context.Context) (applife.ProbeResult, error) {
	client := p.constructor(p.socketPath, p.dialTimeout, p.rpcTimeout)
	_, err := client.Status()
	if err == nil {
		return applife.ProbeOK, nil
	}
	var sue *daemon.SocketUnavailableError
	if errors.As(err, &sue) {
		return applife.ProbeUnreachable, err
	}
	return applife.ProbeReachableBroken, err
}

func productionLoadConfig(explicitPath string) (config.Resolved, error) {
	return loadNormalizedConfig(explicitPath, config.Validate)
}

func productionLoadPasteConfig(explicitPath string) (config.Resolved, error) {
	return loadNormalizedConfig(explicitPath, config.ValidatePaste)
}

func loadNormalizedConfig(explicitPath string, validate func(config.Resolved) error) (config.Resolved, error) {
	cfg, path, err := config.LoadOrDefault(explicitPath, lookupEnv)
	if err != nil {
		return config.Resolved{}, err
	}
	r, err := config.Normalize(cfg, path)
	if err != nil {
		return config.Resolved{}, err
	}
	if err := validate(r); err != nil {
		return config.Resolved{}, err
	}
	return r, nil
}

func productionLoadDaemonConfig(explicitPath string) (config.Resolved, error) {
	cfg, path, err := config.LoadOrDefault(explicitPath, lookupEnv)
	if err != nil {
		return config.Resolved{}, err
	}
	return config.ResolveDaemon(cfg, path), nil
}

// promptSources resolves all configured prompt sources for cfg.
// homeDir is read from the host environment via os.UserHomeDir; if it is
// unavailable and prompts_dir is unset, the resolver returns a clear error.
func promptSources(cfg config.Resolved) ([]promptsource.Source, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return promptsource.Resolve(cfg, lookupEnv, home, cwd, osStatSource)
}

func osStatSource(path string) (promptsource.PathKind, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return promptsource.PathMissing, nil
		}
		return promptsource.PathMissing, err
	}
	if info.IsDir() {
		return promptsource.PathDir, nil
	}
	return promptsource.PathFile, nil
}

// newClipboardReader builds the clipboard.Reader used by both `Deps.NewClip`
// (for non-TUI commands like `paste`/`doctor`) and the production TUI
// Renderer. Extracted so the TUI factory can defer construction until after
// the `TPROMPT_TEST_RENDERER` shortcut, sparing stub-renderer testscripts
// from hard-failing on hosts with no pbpaste/wl-paste/xclip/xsel.
func newClipboardReader(cfg config.Resolved, getenv func(string) string) (clipboard.Reader, error) {
	if len(cfg.ClipboardArgv) > 0 {
		return clipboard.NewCommand(cfg.ClipboardArgv), nil
	}
	return clipboard.NewAutoDetect(getenv, exec.LookPath)
}

// cancelStubRenderer is retained for TPROMPT_TEST_RENDERER=cancel; production
// now uses tui.NewRenderer.
type cancelStubRenderer struct{}

func (cancelStubRenderer) Run(tui.State) (tui.Result, error) {
	return tui.Result{Action: tui.ActionCancel}, nil
}

// parseTestRenderer decodes TPROMPT_TEST_RENDERER into a stub Renderer for
// testscript coverage of the TUI submit paths. Never set in production.
//
// Spec grammar:
//
//	cancel              → ActionCancel
//	clipboard:<body>    → ActionClipboard with ClipboardBody = <body>
//
// The clipboard stub performs its own Submit so runTUI's ActionClipboard
// branch can mirror the real-renderer path (no direct submit), matching the
// AUR-24 ActionPrompt pattern where the Model owns submission.
func parseTestRenderer(spec string, sub submitter.Submitter) (tui.Renderer, error) {
	switch {
	case spec == "cancel":
		return cancelStubRenderer{}, nil
	case strings.HasPrefix(spec, "clipboard:"):
		body := spec[len("clipboard:"):]
		return staticClipboardRenderer{body: []byte(body), sub: sub}, nil
	default:
		return nil, fmt.Errorf("TPROMPT_TEST_RENDERER: unsupported spec %q", spec)
	}
}

type staticClipboardRenderer struct {
	body []byte
	sub  submitter.Submitter
}

func (r staticClipboardRenderer) Run(tui.State) (tui.Result, error) {
	result := tui.Result{Action: tui.ActionClipboard, ClipboardBody: r.body}
	if r.sub == nil {
		return result, nil
	}
	return result, r.sub.Submit(result)
}
