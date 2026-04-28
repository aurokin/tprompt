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

// PromptFileExistsError reports that `tprompt new` refused to overwrite an
// existing file. Surfaced as a prompt-store error (exit 3) by app.ExitCode.
type PromptFileExistsError struct {
	Path string
}

func (e *PromptFileExistsError) Error() string {
	return fmt.Sprintf("prompt file already exists: %s (refusing to overwrite)", e.Path)
}

func newNewCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "new <id>",
		Short: "Scaffold a new prompt file in the global prompts directory",
		Long: `New scaffolds a templated markdown file in the primary global prompts
directory and prints the absolute path of the created file.

The argument is the bare id; the .md extension is implied. Ids with path
separators, leading dots, empty ids, ids ending in .md, or ids with
non-printable characters are rejected up front.

If the target file already exists, new refuses to overwrite it and exits
non-zero. The parent directory is auto-created on first use when it is
the default global path; explicit prompts_dir paths must already exist.

The scaffolded file stubs every supported frontmatter field with an empty
value so authors can see the schema without consulting docs. Empty
frontmatter values are ignored at load.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runNew(deps, args[0])
		},
	}
}

func runNew(deps Deps, id string) error {
	if err := validateNewID(id); err != nil {
		return err
	}
	cfg, err := deps.LoadConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}
	source, err := primaryPromptSource(cfg)
	if err != nil {
		return err
	}
	if err := ensureScaffoldDir(source.Path, source.AutoCreateOnAccess); err != nil {
		return err
	}

	target, err := filepath.Abs(filepath.Join(source.Path, id+".md"))
	if err != nil {
		return fmt.Errorf("resolve prompt file path: %w", err)
	}
	if err := writeScaffold(target); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(deps.Stdout, target)
	return nil
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

// writeScaffold creates target with the scaffold template, refusing to
// overwrite. O_EXCL closes the TOCTOU window between an existence probe and
// the write so a concurrent author cannot lose work.
func writeScaffold(target string) error {
	// G304: target is composed from the resolved primary prompts directory
	// and a validated id (validateNewID rejects path separators, .md
	// suffix, non-printable runes, empty input), so this is a bounded write
	// into the user's own prompt store, not arbitrary user input.
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600) //nolint:gosec
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return &PromptFileExistsError{Path: target}
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
