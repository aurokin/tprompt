package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/aurokin/tprompt/internal/config"
	"github.com/aurokin/tprompt/internal/promptsource"
	"github.com/aurokin/tprompt/internal/store"
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
	var project, edit, noEdit, force bool
	var editor string
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
non-zero; pass --force to overwrite intentionally. The parent directory is
auto-created on first use when it is the default global path. With --project,
<gitroot>/tprompt is auto-created. Explicit prompts_dir paths must already
exist.

When run in an interactive terminal, new opens the created file in your editor,
resolved from --editor, then $VISUAL, then $EDITOR. This is a clean no-op when
none is set or when stdin/stdout is not a tty, so pipelines and CI are
unaffected. Pass --no-edit to skip it, --edit to force it, or --editor <cmd> to
pick the editor for this run (which implies editing); set edit_on_new = false in
config to disable auto-open by default. Editing is best-effort: a failing editor
is reported but does not fail the command, since the file was already created.

The scaffolded file stubs every supported frontmatter field with an empty
value so authors can see the schema without consulting docs. Empty
frontmatter values are ignored at load.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runNew(deps, args[0], newFlags{project: project, edit: edit, noEdit: noEdit, editor: editor, force: force})
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write to <gitroot>/tprompt instead of the global prompts directory")
	cmd.Flags().StringVar(&editor, "editor", "", "editor command to open the new file with (implies --edit; overrides $VISUAL/$EDITOR)")
	cmd.Flags().BoolVar(&edit, "edit", false, "force-open the new file in your editor after creating it")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "do not open an editor, even in an interactive terminal")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing prompt file instead of refusing")
	cmd.MarkFlagsMutuallyExclusive("edit", "no-edit")
	// --editor implies edit, so pairing it with --no-edit is contradictory;
	// reject it rather than silently letting --no-edit cancel the explicit editor.
	cmd.MarkFlagsMutuallyExclusive("editor", "no-edit")
	return cmd
}

type newFlags struct {
	project bool
	edit    bool
	noEdit  bool
	editor  string
	force   bool
}

func runNew(deps Deps, id string, flags newFlags) error {
	if err := validateNewID(id); err != nil {
		return err
	}
	cfg, err := deps.LoadConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}
	source, collisionSources, err := scaffoldTargetSources(cfg, flags.project)
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
	if err := writePromptFile(target, collisionSources, source.Scope, id, []byte(scaffoldTemplate), flags.force); err != nil {
		return err
	}
	editorCmd, open := editorPlan(flags, cfg, deps)
	return announceNewFile(deps, target, editorCmd, open)
}

// writePromptFile is the TOCTOU-safe collision-check + atomic write shared by
// `tprompt new` and `tprompt import`. target is the absolute destination path;
// sources/scope define the duplicate-stem scan tier; content is the file bytes;
// overwrite selects refuse-vs-atomic-replace. The destination directory must
// already exist — callers own ensure/auto-create.
//
// Prompt ids are filename stems and the store walks subdirectories (and
// additional sources), so a same-id `.md` anywhere in this scope collides — not
// just the exact target.
//
// The exact target is checked directly (a stat, not a walk) so an unreadable
// sibling subtree can never mask an already-taken target: this preserves the
// original "first collision wins" error semantics (PromptFileExistsError, exit 3)
// instead of leaking a filesystem walk error. A same-id file at any OTHER path
// in scope (subdirectory or another source) is a duplicate that overwrite cannot
// resolve in place, so it refuses even with overwrite=true; an overwrite=false
// create refuses on any collision.
func writePromptFile(target string, sources []promptsource.Source, scope promptsource.Scope, id string, content []byte, overwrite bool) error {
	targetExisted := pathExists(target)
	blocker, err := promptCollision(target, sources, scope, id, targetExisted, overwrite)
	if err != nil {
		return err
	}
	if blocker != "" {
		return &PromptFileExistsError{ID: id, Path: blocker}
	}
	// Overwrite (atomic rename) only when the target actually existed and
	// overwrite was requested. A fresh create still goes through the O_EXCL path
	// even under overwrite=true, so a concurrent creator racing in between the
	// scan and the write is refused rather than silently clobbered.
	return writePromptContent(id, target, content, overwrite && targetExisted)
}

// promptCollision returns the path of an existing prompt that blocks writing id
// at target under the overwrite policy, or "" if the write may proceed. The exact
// target blocks a non-overwrite write (targetExisted reports whether it already
// exists, so callers stat once); a same-id file at any OTHER path in scope blocks
// regardless of overwrite, since overwrite cannot resolve a duplicate in place.
//
// It is the shared collision predicate. `new` (via writePromptFile) treats any
// non-empty result as a hard error. `import` distinguishes the two cases by the
// returned path: a result equal to the exact target is an idempotent skip, while a
// different path is a cross-path duplicate it must refuse. The exact-target
// short-circuit (returning before the tree walk) means an idempotent import re-run
// skips with a single stat, without walking the store.
func promptCollision(target string, sources []promptsource.Source, scope promptsource.Scope, id string, targetExisted, overwrite bool) (string, error) {
	if targetExisted && !overwrite {
		return target, nil
	}
	return findOtherPromptMatch(sources, scope, id, target)
}

// pathExists reports whether p names an existing filesystem entry. Lstat (not
// Stat) so a symlink counts as present and is compared/replaced as the link
// itself rather than followed.
func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// announceNewFile reports the created file and, when editing is enabled, opens
// it. stdout is tty-aware: an interactive terminal gets a single `Created
// <path>` line, while a piped/redirected stdout gets the bare path and nothing
// else — the scripting contract, so `p=$(tprompt new foo)` still captures just
// the path. When open is true the editor is launched after the line is printed.
func announceNewFile(deps Deps, target, editorCmd string, open bool) error {
	if streamIsTTY(deps.Stdout) {
		_, _ = fmt.Fprintf(deps.Stdout, "Created %s\n", target)
	} else {
		_, _ = fmt.Fprintln(deps.Stdout, target)
	}
	if open {
		launchEditor(deps, editorCmd, target)
	}
	return nil
}

// editorPlan resolves the editor command and decides whether to open it after
// scaffolding. The command is resolved from --editor, then $VISUAL, then
// $EDITOR (first non-empty). The editor opens only when a command resolves AND
// stdin and stdout are both ttys (so redirected/piped/CI runs never launch an
// editor and never leak it onto a stdout the scripting contract reserves for the
// path), and:
//   - --no-edit             -> never
//   - --edit or --editor set -> force open
//   - otherwise             -> the edit_on_new config default (true)
func editorPlan(flags newFlags, cfg config.Resolved, deps Deps) (string, bool) {
	editorCmd := resolveEditor(flags.editor, deps.Env)
	if flags.noEdit {
		return editorCmd, false
	}
	if editorCmd == "" || !streamIsTTY(deps.Stdin) || !streamIsTTY(deps.Stdout) {
		return editorCmd, false
	}
	if flags.edit || flags.editor != "" {
		return editorCmd, true
	}
	return editorCmd, cfg.EditOnNew
}

// resolveEditor returns the editor command line to use: the --editor flag, then
// $VISUAL, then $EDITOR — the standard Unix precedence (VISUAL, the full-screen
// editor, wins over EDITOR). Returns "" when none is set; there is deliberately
// no hardcoded vi/nano fallback, so `new` never force-opens an editor the user
// did not configure. The value is a shell command line (see launchEditor).
func resolveEditor(flagEditor string, env func(string) string) string {
	for _, candidate := range []string{flagEditor, env("VISUAL"), env("EDITOR")} {
		if s := strings.TrimSpace(candidate); s != "" {
			return s
		}
	}
	return ""
}

// launchEditor opens target with editorCmd, best-effort. The caller (editorPlan)
// has already resolved a non-empty command and confirmed stdin/stdout are ttys.
//
// Editing never fails the command: a non-zero editor exit or an unstartable
// editor is reported to stderr but not returned, since `new`'s deliverable (the
// created file) already succeeded.
func launchEditor(deps Deps, editorCmd, target string) {
	// Run through `sh -c` so the command is parsed with full shell quoting —
	// values like `code --wait`, `emacsclient -c -a ""`, or a quoted path
	// containing spaces work, with no naive strings.Fields split to mishandle
	// them. `"$@"` passes the validated scaffold path as the sole positional
	// argument; the editor inherits the command's stdio so it can drive the
	// terminal.
	//
	// $EDITOR/$VISUAL are treated as shell command lines, matching the universal
	// convention (git, less, the freedesktop spec): an executable path that
	// contains spaces must be quoted in the value itself. This is deliberate —
	// field-splitting the raw value instead would break editors that carry
	// quoted arguments. Not a bug; do not "fix" by switching to strings.Fields.
	//
	// G204: the command is the user's own editor and the argument is a validated
	// path in the user's own prompt store — not remote input.
	ed := exec.Command("sh", "-c", editorCmd+` "$@"`, "sh", target) //nolint:gosec
	ed.Stdin = deps.Stdin
	ed.Stdout = deps.Stdout
	ed.Stderr = deps.Stderr
	if err := ed.Run(); err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "tprompt: editor exited with error: %v\n", err)
	}
}

// scaffoldTargetSources resolves the write target and the collision-scan tier
// for `new` and `import`. With project=true it targets the git project's
// tprompt/ overlay; otherwise it targets the primary global source and scans all
// configured global sources for duplicate ids.
func scaffoldTargetSources(cfg config.Resolved, project bool) (promptsource.Source, []promptsource.Source, error) {
	if project {
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

// findOtherPromptMatch returns the path of the first markdown file in the given
// scope whose filename stem matches id but whose path is not the exact target,
// or "" if none. It is the duplicate-ID guard: such a file is a collision that
// --force cannot overwrite in place. The first match short-circuits the walk,
// both bounding the work and keeping a later unreadable subtree from masking an
// already-found collision.
func findOtherPromptMatch(sources []promptsource.Source, scope promptsource.Scope, id, target string) (string, error) {
	for _, source := range sources {
		if source.Scope != scope {
			continue
		}
		if ok, err := promptSourceExists(source); err != nil {
			return "", err
		} else if !ok {
			continue
		}
		// Absolutize the walk root once so matches compare to the absolute
		// target by string, without a per-entry filepath.Abs that could fail
		// mid-walk and misclassify the target itself as a collision.
		root, err := filepath.Abs(source.Path)
		if err != nil {
			return "", fmt.Errorf("resolve prompts source path %s: %w", source.Path, err)
		}
		other, err := findOtherMatchInTree(root, id, target)
		if err != nil {
			return "", err
		}
		if other != "" {
			return other, nil
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

// findOtherMatchInTree walks root (which must be absolute) for the first
// markdown file whose filename stem matches id at a path other than target,
// mirroring the store's discovery rules (skip hidden basenames, only `.md`
// files). It stops at that first match. Comparison is by path, not inode: the
// duplicate-ID contract is about distinct files at distinct paths, so a
// hard-linked same-id file is still a collision. Walk errors propagate.
func findOtherMatchInTree(root, id, target string) (string, error) {
	var other string
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
		// path (from an absolute root) and target are both absolute — target is
		// pre-absolutized by the caller at new.go:119 — so this is an
		// absolute-vs-absolute comparison. A relative prompts_dir does NOT leave
		// target relative here, so the target file is never misclassified as an
		// "other" collision; new_force_overwrite.txtar covers that exact case
		// (relative prompts_dir + --force overwrite).
		if strings.TrimSuffix(d.Name(), ".md") == id && path != target {
			other = path
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	return other, nil
}

// writePromptContent creates target with the given content. When overwrite is
// false it refuses to clobber an existing file: O_EXCL closes the TOCTOU window
// between the collision scan and the write so a concurrent author cannot lose
// work. When overwrite is true (the caller confirmed the target already existed
// and an overwrite was requested) it replaces the target atomically (see
// overwritePromptContent).
func writePromptContent(id, target string, content []byte, overwrite bool) error {
	if overwrite {
		return overwritePromptContent(target, content)
	}
	// G304: target is composed from a resolved prompts directory and a validated
	// id (validateNewID rejects path separators, .md suffix, non-printable runes,
	// empty input), so this is a bounded write into the user's own prompt store,
	// not arbitrary user input.
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600) //nolint:gosec
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return &PromptFileExistsError{ID: id, Path: target}
		}
		return fmt.Errorf("create prompt file %s: %w", target, err)
	}
	if _, writeErr := f.Write(content); writeErr != nil {
		_ = f.Close()
		return fmt.Errorf("write prompt file %s: %w", target, writeErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("close prompt file %s: %w", target, closeErr)
	}
	return nil
}

// overwritePromptContent replaces target with content atomically: write to a
// temp file in the same directory, then rename it over target. The original
// prompt is never unlinked before the new content is durable, so a mid-write
// failure cannot lose it; and rename replaces a symlinked target entry without
// following it, so an overwrite cannot clobber a file outside the store the way
// a truncating open would. The temp name is hidden (leading dot) so the store
// walk ignores any leftover from an interrupted run.
func overwritePromptContent(target string, content []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".tprompt-*")
	if err != nil {
		return fmt.Errorf("create temp prompt file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp prompt file %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp prompt file %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("replace prompt file %s: %w", target, err)
	}
	committed = true
	return nil
}
