// Package promptsource resolves the ordered list of prompt sources used by
// the prompt store. Resolve is a pure function over config, environment,
// cwd/home, and an injected stat predicate so it can be exercised by
// table-driven tests without touching the filesystem.
package promptsource

import (
	"fmt"
	"os"
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

// ProjectRootNotFoundError reports that a project-scoped operation was run
// outside a git tree. CWD names the directory where discovery started.
type ProjectRootNotFoundError struct {
	CWD string
}

func (e *ProjectRootNotFoundError) Error() string {
	if e.CWD == "" {
		return "promptsource: no project root found: not inside a git tree"
	}
	return fmt.Sprintf("promptsource: no project root found from %s: not inside a git tree", e.CWD)
}

// Scope identifies which tier a source belongs to.
type Scope string

const (
	// ScopeGlobal is the user-level prompt store (default or explicit
	// prompts_dir, plus future additional_prompts_dirs).
	ScopeGlobal Scope = "global"
	// ScopeProject is the nearest project-local tprompt/ overlay discovered
	// while walking up from cwd inside a git tree.
	ScopeProject Scope = "project"
)

// PathKind is the minimal filesystem shape Resolve needs from its injected
// stat predicate.
type PathKind int

const (
	PathMissing PathKind = iota
	PathFile
	PathDir
)

// StatFunc reports the shape of a path. Missing paths should return
// PathMissing with nil error; other stat failures should return an error.
type StatFunc func(path string) (PathKind, error)

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
// environment variables (typically os.Getenv); homeDir and cwd are
// caller-provided so the function stays pure. stat is used only for project
// overlay discovery; nil stat or empty cwd disables project discovery.
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
//   - Project overlay: append the nearest tprompt/ source discovered while
//     walking up from cwd before .git, home, or filesystem root.
func Resolve(cfg config.Resolved, getenv func(string) string, homeDir, cwd string, stat StatFunc) ([]Source, error) {
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
	project, ok, err := projectSource(cwd, homeDir, stat)
	if err != nil {
		return nil, err
	}
	if ok {
		sources = append(sources, project)
	}
	return sources, nil
}

// ProjectRoot walks upward from cwd until it finds a git root. The walk stops
// before the user's home directory, matching read-time overlay discovery.
// A directory is considered a git root when it contains a .git entry, which
// covers both normal repositories and linked worktrees/submodules.
func ProjectRoot(cwd, homeDir string, stat func(string) (os.FileInfo, error)) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", &ProjectRootNotFoundError{}
	}
	if stat == nil {
		stat = os.Stat
	}

	start, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve current directory: %w", err)
	}
	start, err = filepath.EvalSymlinks(start)
	if err != nil {
		return "", fmt.Errorf("resolve current directory symlinks: %w", err)
	}
	home := ""
	if homeDir != "" {
		if resolvedHome, err := canonicalPath(homeDir); err == nil {
			home = resolvedHome
		}
	}
	dir := start
	for {
		if samePath(dir, home) {
			return "", &ProjectRootNotFoundError{CWD: start}
		}
		if _, err := stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", &ProjectRootNotFoundError{CWD: start}
		}
		dir = parent
	}
}

// ProjectSource resolves the write/read source for a project prompt overlay.
// It does not require the tprompt directory to exist; callers decide whether
// to auto-create it or skip it.
func ProjectSource(cwd, homeDir string) (Source, error) {
	root, err := ProjectRoot(cwd, homeDir, os.Stat)
	if err != nil {
		return Source{}, err
	}
	return Source{
		Path:               filepath.Join(root, "tprompt"),
		Scope:              ScopeProject,
		AutoCreateOnAccess: false,
	}, nil
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

func projectSource(cwd, homeDir string, stat StatFunc) (Source, bool, error) {
	if cwd == "" || stat == nil {
		return Source{}, false, nil
	}
	current, err := canonicalPath(cwd)
	if err != nil {
		return Source{}, false, fmt.Errorf("resolve cwd: %w", err)
	}
	home := ""
	if homeDir != "" {
		home, _ = canonicalPath(homeDir)
	}

	for {
		if samePath(current, home) || isRoot(current) {
			return Source{}, false, nil
		}

		promptDir := filepath.Join(current, "tprompt")
		kind, err := stat(promptDir)
		if err != nil {
			return Source{}, false, err
		}
		if kind == PathDir {
			if ok, err := hasGitAncestor(current, home, stat); err != nil {
				return Source{}, false, err
			} else if ok {
				return Source{Path: promptDir, Scope: ScopeProject}, true, nil
			}
			return Source{}, false, nil
		}

		gitKind, err := stat(filepath.Join(current, ".git"))
		if err != nil {
			return Source{}, false, err
		}
		if gitKind == PathDir || gitKind == PathFile {
			return Source{}, false, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return Source{}, false, nil
		}
		current = parent
	}
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	evaluated, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs, nil
	}
	return evaluated, nil
}

func hasGitAncestor(start, home string, stat StatFunc) (bool, error) {
	current := start
	for {
		if samePath(current, home) || isRoot(current) {
			return false, nil
		}
		kind, err := stat(filepath.Join(current, ".git"))
		if err != nil {
			return false, err
		}
		if kind == PathDir || kind == PathFile {
			return true, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false, nil
		}
		current = parent
	}
}

func samePath(a, b string) bool {
	return a != "" && b != "" && filepath.Clean(a) == filepath.Clean(b)
}

func isRoot(path string) bool {
	return filepath.Dir(path) == path
}
