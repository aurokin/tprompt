package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/picker"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/submitter"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
)

// Deps provides the capabilities that CLI handlers need. Production code
// supplies real implementations; tests inject fakes. Lazy factory functions
// let each handler pay only for what it uses.
type Deps struct {
	Stdout   io.Writer
	Stderr   io.Writer
	Stdin    io.Reader
	Env      func(string) string
	LookPath func(string) (string, error)

	ConfigPath       *string
	LoadConfig       func(explicitPath string) (config.Resolved, error)
	LoadPasteConfig  func(explicitPath string) (config.Resolved, error)
	LoadDaemonConfig func(explicitPath string) (config.Resolved, error)
	NewStore         func(cfg config.Resolved) (store.Store, error)
	NewTmux          func() (tmux.Adapter, error)
	NewClip          func(cfg config.Resolved) (clipboard.Reader, error)
	NewPicker        func(cfg config.Resolved) (picker.Picker, error)
	NewDaemonClient  func(cfg config.Resolved) (daemon.Client, error)
	StartDaemon      func(cfg config.Resolved, explicitConfigPath string) error
	NewRenderer      func(cfg config.Resolved, prompts store.Store, sub submitter.Submitter) (tui.Renderer, error)
	NewSubmitter     func(cfg config.Resolved, prompts store.Store, client daemon.Client, target tmux.TargetContext) submitter.Submitter
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
			s := store.NewFS(cfg.PromptsDir, cfg.ReservedPrintable, cfg.KeybindPool)
			return s, nil
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
		StartDaemon: productionStartDaemon,
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

func productionStartDaemon(_ config.Resolved, explicitConfigPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	var args []string
	if explicitConfigPath != "" {
		args = append(args, "--config", explicitConfigPath)
	}
	args = append(args, "daemon", "start")
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
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
