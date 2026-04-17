package app

import (
	"io"
	"os/exec"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
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

	ConfigPath *string
	LoadConfig func(explicitPath string) (config.Resolved, error)
	NewStore   func(cfg config.Resolved) (store.Store, error)
	NewTmux    func() (tmux.Adapter, error)
	NewClip    func(cfg config.Resolved) (clipboard.Reader, error)
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
	}
}
