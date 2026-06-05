package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/delivery"
	"github.com/hsadler/tprompt/internal/handoff"
	"github.com/hsadler/tprompt/internal/picker"
	"github.com/hsadler/tprompt/internal/promptsource"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/submitter"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
	"github.com/hsadler/tprompt/internal/wispr"
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

	ConfigPath        *string
	LoadConfig        func(explicitPath string) (config.Resolved, error)
	LoadPasteConfig   func(explicitPath string) (config.Resolved, error)
	LoadHandoffConfig func(explicitPath string) (config.Resolved, error)
	NewStore          func(cfg config.Resolved) (store.Store, error)
	NewWisprReader    func(dbPath string) wispr.Reader
	NewTmux           func() (tmux.Adapter, error)
	NewClip           func(cfg config.Resolved) (clipboard.Reader, error)
	NewPicker         func(cfg config.Resolved) (picker.Picker, error)
	NewTUIClient      func(cfg config.Resolved) (delivery.Client, error)
	NewRenderer       func(cfg config.Resolved, prompts store.Store, sub submitter.Submitter) (tui.Renderer, error)
	NewSubmitter      func(cfg config.Resolved, prompts store.Store, client delivery.Client, target tmux.TargetContext) submitter.Submitter
}

// ProductionDeps returns a Deps wired for real execution.
func ProductionDeps(stdout, stderr io.Writer, stdin io.Reader) Deps {
	return Deps{
		Stdout:            stdout,
		Stderr:            stderr,
		Stdin:             stdin,
		Env:               lookupEnv,
		LookPath:          exec.LookPath,
		LoadConfig:        productionLoadConfig,
		LoadPasteConfig:   productionLoadPasteConfig,
		LoadHandoffConfig: productionLoadHandoffConfig,
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
		NewWisprReader: func(dbPath string) wispr.Reader {
			return wispr.NewReader(dbPath)
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
		NewTUIClient: productionNewHandoffClient,
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
		NewSubmitter: func(cfg config.Resolved, prompts store.Store, client delivery.Client, target tmux.TargetContext) submitter.Submitter {
			return submitter.New(prompts, client, cfg, target)
		},
	}
}

func productionNewHandoffClient(cfg config.Resolved) (delivery.Client, error) {
	if err := handoff.ValidateConfig(cfg); err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	return handoff.NewClient(cfg, executable, cfg.ConfigPath), nil
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

func productionLoadHandoffConfig(explicitPath string) (config.Resolved, error) {
	cfg, path, err := config.LoadOrDefault(explicitPath, lookupEnv)
	if err != nil {
		return config.Resolved{}, err
	}
	r := config.ResolveHandoff(cfg, path)
	if err := handoff.ValidateConfig(r); err != nil {
		return config.Resolved{}, err
	}
	return r, nil
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
