// Package promptsource resolves the ordered list of prompt sources used by
// the prompt store. Resolve is a pure function over (config, env getter, home
// directory) so it can be exercised by table-driven tests without touching
// the filesystem.
//
// Later milestone slices extend Resolve with a walk-up project overlay; this
// slice owns the primary global source plus additional global folders.
package promptsource

import (
	"path/filepath"
	"strings"

	"github.com/hsadler/tprompt/internal/config"
)

// UnresolvedDefaultDirError reports that prompts_dir was unset and no default
// could be derived because both XDG_CONFIG_HOME and the home directory were
// unavailable. Surfaced as a usage error (exit code 2) by app.ExitCode.
type UnresolvedDefaultDirError struct{}

func (e *UnresolvedDefaultDirError) Error() string {
	return "promptsource: cannot resolve default prompts directory: XDG_CONFIG_HOME unset and home directory unknown"
}

// Scope identifies which tier a source belongs to. Today only ScopeGlobal is
// produced; ScopeProject lands with the project-overlay slice.
type Scope string

const (
	// ScopeGlobal is the user-level prompt store (default or explicit
	// prompts_dir, plus future additional_prompts_dirs).
	ScopeGlobal Scope = "global"
)

// Source describes one resolved prompt directory. AutoCreateOnAccess is true
// only when the path was filled in by default-resolution; explicit user
// settings retain the existing "missing dir is a hard error" contract.
// Optional is true for additional global sources: missing optional sources are
// skipped by the runtime store and surfaced as non-fatal doctor warnings.
type Source struct {
	Path               string
	Scope              Scope
	AutoCreateOnAccess bool
	Optional           bool
}

// Resolve returns the ordered list of prompt sources for cfg. getenv reads
// environment variables (typically os.Getenv); homeDir is the absolute path
// of the user's home directory (caller-provided so the function stays pure).
//
// The result starts with the primary global directory, followed by each
// configured additional global directory:
//
//   - cfg.PromptsDir set: used verbatim, AutoCreateOnAccess=false.
//   - cfg.PromptsDir empty + XDG_CONFIG_HOME set: <xdg>/tprompt/prompts,
//     AutoCreateOnAccess=true.
//   - cfg.PromptsDir empty + homeDir set: <home>/.config/tprompt/prompts,
//     AutoCreateOnAccess=true.
//   - cfg.PromptsDir empty + neither: error.
//   - cfg.AdditionalPromptsDirs: appended in order, AutoCreateOnAccess=false,
//     Optional=true.
func Resolve(cfg config.Resolved, getenv func(string) string, homeDir string) ([]Source, error) {
	primary, err := primarySource(cfg, getenv, homeDir)
	if err != nil {
		return nil, err
	}
	sources := []Source{primary}
	for _, path := range cfg.AdditionalPromptsDirs {
		if strings.TrimSpace(path) == "" {
			continue
		}
		sources = append(sources, Source{
			Path:               path,
			Scope:              ScopeGlobal,
			AutoCreateOnAccess: false,
			Optional:           true,
		})
	}
	return sources, nil
}

func primarySource(cfg config.Resolved, getenv func(string) string, homeDir string) (Source, error) {
	if cfg.PromptsDir != "" {
		return Source{
			Path:               cfg.PromptsDir,
			Scope:              ScopeGlobal,
			AutoCreateOnAccess: false,
		}, nil
	}

	if getenv != nil {
		if xdg := strings.TrimSpace(getenv("XDG_CONFIG_HOME")); xdg != "" {
			return Source{
				Path:               filepath.Join(xdg, "tprompt", "prompts"),
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: true,
			}, nil
		}
	}

	if homeDir != "" {
		return Source{
			Path:               filepath.Join(homeDir, ".config", "tprompt", "prompts"),
			Scope:              ScopeGlobal,
			AutoCreateOnAccess: true,
		}, nil
	}

	return Source{}, &UnresolvedDefaultDirError{}
}
