package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/promptsource"
	"github.com/hsadler/tprompt/internal/store"
)

// scaffoldTemplate is the literal body written by `tprompt new`. Every
// currently-supported frontmatter field is stubbed empty so authors see the
// schema without consulting docs (AUR-146 acceptance criterion). Empty
// frontmatter values are treated as absent at load time (AUR-144), so the
// scaffolded file loads cleanly.
const scaffoldTemplate = `---
title:
description:
tags: []
key:
mode:
enter:
---
`

// InvalidNewIDError reports a `tprompt new <id>` argument that fails id
// validation. Surfaced as a usage error (exit 2) by app.ExitCode.
type InvalidNewIDError struct {
	ID     string
	Reason string
}

func (e *InvalidNewIDError) Error() string {
	if e.ID == "" {
		return fmt.Sprintf("invalid prompt id: %s", e.Reason)
	}
	return fmt.Sprintf("invalid prompt id %q: %s", e.ID, e.Reason)
}

// PromptFileExistsError reports that a prompt with the requested id already
// exists in the resolved prompts source. Path names the existing file —
// either the same target the scaffolder would have written, or another file
// elsewhere in the tree whose filename stem matches the id. Surfaced as a
// prompt-store error (exit 3) by app.ExitCode.
type PromptFileExistsError struct {
	ID   string
	Path string
}

func (e *PromptFileExistsError) Error() string {
	return fmt.Sprintf("prompt id %q already exists at %s (refusing to overwrite)", e.ID, e.Path)
}

func newNewCmd(deps Deps) *cobra.Command {
	var project bool
	cmd := &cobra.Command{
		Use:   "new <id>",
		Short: "Scaffold a new prompt file",
		Long: `New scaffolds a templated markdown file in the primary global prompts
directory and prints the absolute path of the created file. With --project, it
scaffolds into the current git project's tprompt/ directory instead.

The argument is the bare id; the .md extension is implied. Ids with path
separators, leading dots, empty ids, ids ending in .md, or ids with
non-printable characters are rejected up front.

If the target file already exists, new refuses to overwrite it and exits
non-zero. The parent directory is auto-created on first use when it is
the default global path. With --project, <gitroot>/tprompt is auto-created.
Explicit prompts_dir paths must already exist.

The scaffolded file stubs every supported frontmatter field with an empty
value so authors can see the schema without consulting docs. Empty
frontmatter values are ignored at load.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runNew(deps, args[0], newFlags{project: project})
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write to <gitroot>/tprompt instead of the global prompts directory")
	return cmd
}

type newFlags struct {
	project bool
}

func runNew(deps Deps, id string, flags newFlags) error {
	if err := validateNewID(id); err != nil {
		return err
	}
	cfg, err := deps.LoadConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}
	source, collisionSources, err := scaffoldTargetSources(cfg, flags)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(filepath.Join(source.Path, id+".md"))
	if err != nil {
		return fmt.Errorf("resolve prompt file path: %w", err)
	}
	if err := validatePromptSources(collisionSources, source.Path); err != nil {
		return err
	}
	if err := ensureScaffoldDir(source.Path, source.AutoCreateOnAccess); err != nil {
		return err
	}
	// Prompt ids are filename stems and the store walks subdirectories, so
	// any existing `<id>.md` anywhere under source.Path collides — not just
	// the exact target. Scan first so the user sees a clear error instead
	// of a silently-broken store on the next list/show.
	if existing, err := findPromptByIDInSources(collisionSources, source.Scope, id); err != nil {
		return err
	} else if existing != "" {
		return &PromptFileExistsError{ID: id, Path: existing}
	}
	if err := writeScaffold(id, target); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(deps.Stdout, target)
	// The scaffold body is empty, so a freshly-created prompt delivers nothing
	// until the author fills it in. Nudge them, pointing at the absolute file
	// path (not a reconstructed `show <id>` command, which would not preserve
	// --config, could resolve to a shadowing prompt under --project, and would
	// need shell-quoting for unusual ids). tty-gated to stderr: stdout stays
	// exactly the path for scripting, and piped/non-tty runs (including the
	// golden testscripts asserting empty stderr) emit nothing.
	if stderrIsTTY(deps.Stderr) {
		_, _ = fmt.Fprintf(deps.Stderr, "now add your prompt body to %s\n", target)
	}
	return nil
}

func scaffoldTargetSources(cfg config.Resolved, flags newFlags) (promptsource.Source, []promptsource.Source, error) {
	if flags.project {
		cwd, err := os.Getwd()
		if err != nil {
			return promptsource.Source{}, nil, fmt.Errorf("resolve current directory: %w", err)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			home = ""
		}
		source, err := promptsource.ProjectSource(cwd, home)
		if err != nil {
			return promptsource.Source{}, nil, err
		}
		source.AutoCreateOnAccess = true
		return source, []promptsource.Source{source}, nil
	}

	sources, err := promptSources(cfg)
	if err != nil {
		return promptsource.Source{}, nil, err
	}
	return sources[0], sources, nil
}

func validateNewID(id string) error {
	if id == "" {
		return &InvalidNewIDError{ID: id, Reason: "must not be empty"}
	}
	if strings.ContainsAny(id, `/\`) {
		return &InvalidNewIDError{ID: id, Reason: "must not contain path separators"}
	}
	// A leading dot would produce a hidden filename; the store's WalkDir
	// skips hidden basenames (internal/store/store.go shouldSkipPath), so
	// scaffolding `.foo` would silently create an undiscoverable file.
	if strings.HasPrefix(id, ".") {
		return &InvalidNewIDError{ID: id, Reason: "must not start with a dot"}
	}
	if strings.HasSuffix(id, ".md") {
		return &InvalidNewIDError{ID: id, Reason: "must not include the .md suffix; pass the bare id"}
	}
	for _, r := range id {
		if !unicode.IsPrint(r) {
			return &InvalidNewIDError{ID: id, Reason: "must contain only printable characters"}
		}
	}
	return nil
}

// ensureScaffoldDir guarantees that the prompts directory exists before the
// scaffolder writes into it. The default global path is created on demand so
// fresh installs work with zero setup; explicit prompts_dir settings keep the
// existing "missing dir is a hard error" contract.
func ensureScaffoldDir(path string, autoCreate bool) error {
	if autoCreate {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return &store.PromptsDirCreateError{Path: path, Err: err}
		}
		return nil
	}
	info, statErr := os.Stat(path)
	if statErr != nil || !info.IsDir() {
		return &store.PromptsDirMissingError{Path: path}
	}
	return nil
}

func validatePromptSources(sources []promptsource.Source, targetPath string) error {
	for _, source := range sources {
		if source.Path == targetPath {
			continue
		}
		if _, err := promptSourceExists(source); err != nil {
			return err
		}
	}
	return nil
}

func findPromptByIDInSources(sources []promptsource.Source, scope promptsource.Scope, id string) (string, error) {
	for _, source := range sources {
		if source.Scope != scope {
			continue
		}
		if ok, err := promptSourceExists(source); err != nil {
			return "", err
		} else if !ok {
			continue
		}
		existing, err := findPromptByID(source.Path, id)
		if err != nil {
			return "", err
		}
		if existing != "" {
			return existing, nil
		}
	}
	return "", nil
}

func promptSourceExists(source promptsource.Source) (bool, error) {
	info, err := os.Stat(source.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if source.AutoCreateOnAccess {
				return false, nil
			}
			if source.Optional {
				return false, nil
			}
			return false, &store.PromptsDirMissingError{Path: source.Path}
		}
		return false, err
	}
	if !info.IsDir() {
		return false, &store.PromptsDirMissingError{Path: source.Path}
	}
	return true, nil
}

// findPromptByID walks root looking for any markdown file whose filename
// stem matches id, mirroring the discovery rules in internal/store
// (skip hidden basenames, only `.md` files). Returns the first match or
// the empty string if none. Walk errors propagate.
func findPromptByID(root, id string) (string, error) {
	var found string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		if strings.TrimSuffix(d.Name(), ".md") == id {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	return found, nil
}

// writeScaffold creates target with the scaffold template, refusing to
// overwrite. O_EXCL closes the TOCTOU window between findPromptByID and the
// write so a concurrent author cannot lose work.
func writeScaffold(id, target string) error {
	// G304: target is composed from the resolved primary prompts directory
	// and a validated id (validateNewID rejects path separators, .md
	// suffix, non-printable runes, empty input), so this is a bounded write
	// into the user's own prompt store, not arbitrary user input.
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600) //nolint:gosec
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return &PromptFileExistsError{ID: id, Path: target}
		}
		return fmt.Errorf("create prompt file %s: %w", target, err)
	}
	if _, writeErr := f.Write([]byte(scaffoldTemplate)); writeErr != nil {
		_ = f.Close()
		return fmt.Errorf("write prompt file %s: %w", target, writeErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("close prompt file %s: %w", target, closeErr)
	}
	return nil
}
