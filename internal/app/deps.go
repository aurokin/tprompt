package app

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
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
	LoadDaemonConfig func(explicitPath string) (config.Resolved, error)
	NewStore         func(cfg config.Resolved) (store.Store, error)
	NewTmux          func() (tmux.Adapter, error)
	NewClip          func(cfg config.Resolved) (clipboard.Reader, error)
	NewDaemonClient  func(cfg config.Resolved) (daemon.Client, error)
	NewRenderer      func(cfg config.Resolved) (tui.Renderer, error)
	NewSubmitter     func(cfg config.Resolved, prompts store.Store, client daemon.Client, target tmux.TargetContext) submitter.Submitter
}

// ProductionDeps returns a Deps wired for real execution.
func ProductionDeps(stdout, stderr io.Writer, stdin io.Reader) Deps {
	return Deps{
		Stdout:   stdout,
		Stderr:   stderr,
		Stdin:    stdin,
		Env:      lookupEnv,
		LookPath: exec.LookPath,
		LoadConfig: func(explicitPath string) (config.Resolved, error) {
			cfg, path, err := config.LoadOrDefault(explicitPath, lookupEnv)
			if err != nil {
				return config.Resolved{}, err
			}
			r, err := config.Normalize(cfg, path)
			if err != nil {
				return config.Resolved{}, err
			}
			if err := config.Validate(r); err != nil {
				return config.Resolved{}, err
			}
			return r, nil
		},
		LoadDaemonConfig: func(explicitPath string) (config.Resolved, error) {
			cfg, path, err := config.LoadOrDefault(explicitPath, lookupEnv)
			if err != nil {
				return config.Resolved{}, err
			}
			return config.ResolveDaemon(cfg, path), nil
		},
		NewStore: func(cfg config.Resolved) (store.Store, error) {
			s := store.NewFS(cfg.PromptsDir, cfg.ReservedPrintable, cfg.KeybindPool)
			return s, nil
		},
		NewTmux: func() (tmux.Adapter, error) {
			return tmux.New(tmux.NewExecRunner("")), nil
		},
		NewClip: func(cfg config.Resolved) (clipboard.Reader, error) {
			if len(cfg.ClipboardArgv) > 0 {
				return clipboard.NewCommand(cfg.ClipboardArgv), nil
			}
			return clipboard.NewAutoDetect(lookupEnv, exec.LookPath)
		},
		NewDaemonClient: func(cfg config.Resolved) (daemon.Client, error) {
			return daemon.NewSocketClient(cfg.SocketPath), nil
		},
		NewRenderer: func(cfg config.Resolved) (tui.Renderer, error) {
			if spec := lookupEnv("TPROMPT_TEST_RENDERER"); spec != "" {
				return parseTestRenderer(spec)
			}
			return tui.NewRenderer(tui.ModelDeps{
				MaxPasteBytes: cfg.MaxPasteBytes,
			}), nil
		},
		NewSubmitter: func(cfg config.Resolved, prompts store.Store, client daemon.Client, target tmux.TargetContext) submitter.Submitter {
			return submitter.New(prompts, client, cfg, target)
		},
	}
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
func parseTestRenderer(spec string) (tui.Renderer, error) {
	switch {
	case spec == "cancel":
		return cancelStubRenderer{}, nil
	case strings.HasPrefix(spec, "clipboard:"):
		body := spec[len("clipboard:"):]
		return staticClipboardRenderer{body: []byte(body)}, nil
	default:
		return nil, fmt.Errorf("TPROMPT_TEST_RENDERER: unsupported spec %q", spec)
	}
}

type staticClipboardRenderer struct{ body []byte }

func (r staticClipboardRenderer) Run(tui.State) (tui.Result, error) {
	return tui.Result{Action: tui.ActionClipboard, ClipboardBody: r.body}, nil
}
