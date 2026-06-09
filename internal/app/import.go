package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/aurokin/tprompt/internal/importtui"
	"github.com/aurokin/tprompt/internal/promptsource"
	"github.com/aurokin/tprompt/internal/wispr"
)

// defaultWisprTag is the default provenance tag stamped on every imported
// prompt; overridable via --tag.
const defaultWisprTag = "wispr"

func newImportCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import prompts from external sources",
		Long: `Import brings prompt-shaped content in from external tools as markdown
prompts. Pick a source subcommand:

  wispr   Import Wispr Flow snippets as prompts.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			// Bare `tprompt import` (no source) prints help when not dispatched.
			// In tmux+tty, dispatchArgs rewrites it to `import <default> -i`
			// (DECISIONS §30/§34) before cobra runs; outside tmux/tty it lands
			// here and prints help.
			return c.Help()
		},
	}
	// Each registered import source contributes its own subcommand. The engine
	// downstream is source-agnostic (it consumes ImportRecords), so adding a source
	// is a registry entry plus its implementer — see internal/app/import_source.go.
	for _, src := range importSourceRegistry {
		cmd.AddCommand(src.NewCommand(deps))
	}
	return cmd
}

func newImportWisprCmd(deps Deps) *cobra.Command {
	flags := importFlags{}
	var dbPath string
	cmd := &cobra.Command{
		Use:   "wispr",
		Short: "Import Wispr Flow snippets as prompts",
		Long: `Wispr reads your local Wispr Flow snippets (the trigger → expansion text
you saved in Wispr) and writes each as a markdown prompt in the primary global
prompts directory. It opens Wispr's local flow.sqlite read-only — it never
writes to Wispr — and reads only snippets, never your dictation history.

Import is idempotent: a snippet whose id already exists as a prompt is skipped,
so re-running never creates duplicates. Use --overwrite to refresh existing
prompts from Wispr, or --dry-run to preview without writing. Created file paths
print to stdout (one per line, for scripting); an "imported N, skipped M"
summary prints to stderr.

The Wispr database is found at its default OS location; pass --db-path to point
at a copy or a non-default install.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runImportWispr(deps, dbPath, flags)
		},
	}
	// --db-path is wispr-specific and stays out of the shared importFlags: the
	// source resolves it before handing records to the source-agnostic engine.
	cmd.Flags().StringVar(&dbPath, "db-path", "", "path to Wispr Flow flow.sqlite (overrides the OS default location)")
	cmd.Flags().BoolVar(&flags.project, "project", false, "write to <gitroot>/tprompt instead of the global prompts directory")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "preview what would be imported; write nothing")
	cmd.Flags().BoolVar(&flags.overwrite, "overwrite", false, "replace existing prompts with the same id (refresh from Wispr)")
	cmd.Flags().StringVar(&flags.tag, "tag", defaultWisprTag, "provenance tag stamped on every imported prompt")
	cmd.Flags().BoolVarP(&flags.interactive, "interactive", "i", false, "pick which snippets to import in an interactive list (needs a terminal)")
	return cmd
}

// importFlags are the source-agnostic import flags shared by every import source's
// subcommand and threaded through the engine. Source-specific flags (e.g. wispr's
// --db-path) are bound separately by each source's command.
type importFlags struct {
	project     bool
	dryRun      bool
	overwrite   bool
	tag         string
	interactive bool
}

// InteractiveRequiresTTYError reports `import wispr -i` invoked without an
// interactive terminal on both stdin and stdout. The picker cannot run, and the
// locked decision is an obvious failure over a silent non-interactive fallback.
// Surfaced as a usage error (exit 2) by app.ExitCode.
type InteractiveRequiresTTYError struct{}

func (*InteractiveRequiresTTYError) Error() string {
	return "import wispr -i needs an interactive terminal (stdin and stdout must be a tty); re-run without -i to import non-interactively"
}

// InteractiveDryRunConflictError reports `import wispr -i --dry-run`: a picker
// that writes nothing is a contradiction. Surfaced as a usage error (exit 2).
type InteractiveDryRunConflictError struct{}

func (*InteractiveDryRunConflictError) Error() string {
	return "import wispr -i and --dry-run conflict: -i interactively selects what to write, --dry-run writes nothing"
}

// runImportWispr is the wispr source's run: it produces records (resolve the DB
// path, read snippets read-only) and hands them to the source-agnostic engine.
// The DB read is deferred into the producer so runImport's interactive preflight
// still runs before any side effect.
func runImportWispr(deps Deps, dbPath string, flags importFlags) error {
	return runImport(deps, flags, func() ([]wispr.Snippet, error) {
		resolved, err := resolveWisprDBPath(deps, dbPath)
		if err != nil {
			return nil, err
		}
		return deps.NewWisprReader(resolved).Snippets()
	})
}

// runImport is the source-agnostic import engine. produce yields the source's
// records lazily so the interactive preflight (a flag contradiction or a non-tty
// environment) fails obviously (exit 2) BEFORE any source side effect such as
// opening a database. A nil picker means a non-interactive run. The whole pipeline
// — config, destination prep, classification, write, summary — is identical for any
// source; only produce() varies, which is the seam.
func runImport[R ImportRecord](deps Deps, flags importFlags, produce func() ([]R, error)) error {
	picker, err := interactivePicker(deps, flags)
	if err != nil {
		return err
	}

	cfg, err := deps.LoadConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}

	records, err := produce()
	if err != nil {
		return err
	}

	source, collisionSources, err := scaffoldTargetSources(cfg, flags.project)
	if err != nil {
		return err
	}
	if err := validatePromptSources(collisionSources, source.Path); err != nil {
		return err
	}
	if err := prepareImportDest(source, flags, picker != nil); err != nil {
		return err
	}

	if picker != nil {
		return runInteractiveImport(deps, picker, source, collisionSources, records, flags)
	}

	imported, skipped, err := importSnippets(deps, source, collisionSources, records, flags, nil)
	if err != nil {
		return err
	}
	writeImportSummary(deps, flags.dryRun, imported, skipped)
	return nil
}

// prepareImportDest validates or creates the destination before any write. The
// auto-create default global dir / project overlay is created only for a
// non-interactive real run: --dry-run writes nothing, and an interactive run
// defers creation until the user confirms (so a cancel leaves no directory —
// importSnippets handles it via ensureImportWriteDir). An explicit,
// non-auto-create prompts_dir that is missing is surfaced eagerly in every mode,
// so a real import never claims success — and a picker never opens — for a
// destination it could not use.
//
// An interactive auto-create destination still validates an EXISTING path here
// (without creating a missing one): a path occupied by a regular file can never
// be a prompts directory, so surface that before the picker opens rather than
// only after the user confirms (and never at all if they cancel).
func prepareImportDest(source promptsource.Source, flags importFlags, interactive bool) error {
	if !source.AutoCreateOnAccess {
		return ensureScaffoldDir(source.Path, false)
	}
	if flags.dryRun {
		return nil
	}
	if interactive {
		// Missing path → fine; created lazily on the first write. An existing path
		// is validated via the create helper, which is a no-op for a real directory
		// and surfaces a non-directory as PromptsDirCreateError — without creating
		// anything new, since the path already exists. Lstat (not Stat) so a broken
		// symlink, which Stat reports as ErrNotExist, is still caught here: the
		// dir-entry exists and cannot be used, so MkdirAll surfaces it eagerly.
		if _, err := os.Lstat(source.Path); errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return ensureScaffoldDir(source.Path, true)
	}
	return ensureScaffoldDir(source.Path, true)
}

// interactivePicker validates the `-i` preflight and builds the picker, or
// returns (nil, nil) for a non-interactive run. The renderer's factory carries
// the tty gate (and the test stub bypasses it), so constructing it here rejects
// piped `-i` before any DB/store work, regardless of import contents.
func interactivePicker(deps Deps, flags importFlags) (importtui.Renderer, error) {
	if !flags.interactive {
		return nil, nil
	}
	if flags.dryRun {
		return nil, &InteractiveDryRunConflictError{}
	}
	return deps.NewImportRenderer()
}

// runInteractiveImport renders the fresh (planImportable) snippets in the picker
// and imports exactly the confirmed ids. It builds the plan and re-runs the
// writer over the SAME snippets slice in the same order, so the disambiguated ids
// the picker showed are exactly the ids importSnippets re-derives (the same-slice
// invariant — DECISIONS AUR-528 D4).
//
// selected starts empty (nothing chosen). With no fresh items the picker is
// skipped entirely (no blank list) and the empty selection imports nothing. A
// cancel returns before anything is written (exit 0, no summary).
//
// The auto-create destination is created lazily by importSnippets, right before
// the first actual write (see ensureImportWriteDir). So every no-op interactive
// outcome — cancel, deselect-all, zero fresh rows, or a selection that all
// re-classifies to a skip while the picker was open — leaves no empty directory
// behind, honoring the cancel/no-op = "writes nothing" contract even under a race.
func runInteractiveImport[R ImportRecord](deps Deps, picker importtui.Renderer, source promptsource.Source, collisionSources []promptsource.Source, records []R, flags importFlags) error {
	sel := &importSelection{write: map[string]bool{}, overwrite: map[string]bool{}}
	if items := pickerItems(dryRunPlan(source, collisionSources, records, flags), flags.tag); len(items) > 0 {
		result, err := picker.Run(importtui.State{Items: items})
		if err != nil {
			return err
		}
		if result.Action == importtui.ActionCancel {
			return nil
		}
		for _, id := range result.SelectedIDs {
			sel.write[id] = true
		}
		for _, id := range result.OverwriteIDs {
			sel.overwrite[id] = true
		}
	}

	imported, skipped, err := importSnippets(deps, source, collisionSources, records, flags, sel)
	if err != nil {
		return err
	}
	writeImportSummary(deps, flags.dryRun, imported, skipped)
	return nil
}

// ensureImportWriteDir creates the auto-create destination just before the first
// interactive write. A non-auto-create explicit prompts_dir was already validated
// to exist in runImportWispr, so it needs nothing here.
func ensureImportWriteDir(source promptsource.Source) error {
	if !source.AutoCreateOnAccess {
		return nil
	}
	return ensureScaffoldDir(source.Path, true)
}

// pickerItems projects the user-actionable plan rows into picker items, carrying
// each row's conflict kind so the picker renders the right glyph and selectability:
//   - planImportable, fresh create (no existing target) → ConflictNone;
//   - planImportable, existing target → a refresh authorized by the CLI --overwrite
//     flag (the only way an existing target classifies importable): shown as a
//     pre-armed ConflictExactTarget so the glyph/counter report the overwrite
//     truthfully rather than as a fresh create (completes AUR-528 D10's deferral);
//   - planExists     → ConflictExactTarget (skip-by-default; check to arm overwrite);
//   - planCrossPath  → ConflictCrossPath (non-selectable; shows the conflicting path).
//
// planEmptyBody / planInvalidID / planClassifyError are NOT shown: they are
// degenerate or internal skips outside the conflict-review vocabulary (a classify
// error still aborts the run via the writer path, not the picker).
//
// tag is the active provenance tag (flags.tag); each item carries the snippet's
// tags (the tag plus "starred" when starred) so the picker's `/`-search can match
// on them. These are the SAME tags the writer stamps into frontmatter (both go
// through wispr.Snippet.Tags), so a search hit reflects what gets written.
func pickerItems(plan []planItem, tag string) []importtui.Item {
	items := make([]importtui.Item, 0, len(plan))
	for _, p := range plan {
		tags := p.record.Tags(tag)
		title := p.record.Title()
		switch p.status {
		case planImportable:
			if p.targetExisted {
				items = append(items, importtui.Item{ID: p.id, Title: title, Conflict: importtui.ConflictExactTarget, Armed: true, Tags: tags})
			} else {
				items = append(items, importtui.Item{ID: p.id, Title: title, Conflict: importtui.ConflictNone, Tags: tags})
			}
		case planExists:
			items = append(items, importtui.Item{ID: p.id, Title: title, Conflict: importtui.ConflictExactTarget, Tags: tags})
		case planCrossPath:
			items = append(items, importtui.Item{ID: p.id, Title: title, Conflict: importtui.ConflictCrossPath, Blocker: p.blocker, Tags: tags})
		}
	}
	return items
}

// importSnippets executes (or, under --dry-run, previews) the batch import,
// returning the imported/skipped tallies. It classifies and writes each snippet
// adjacently — the original single-pass structure — so every collision decision is
// made against the store as it is at that snippet's write moment, never a stale
// batch snapshot (which could skip a freed target, abort on a removed duplicate, or
// clobber a target that appeared after planning). dryRunPlan is the batch
// counterpart the interactive import TUI renders up front (AUR-528); it shares
// classifySnippet, so the displayed classification matches what the writer does.
//
// Created paths go to stdout (real runs) or `would create:`/`would skip:` lines to
// stderr (dry-run). The imported-vs-skipped branch keys off the executed outcome,
// never the planned status — the O_EXCL create-race turns a planned create into an
// idempotent skip at execution time.
//
// sel is the interactive selection filter. nil imports every item (byte-identical
// to the non-interactive path). A non-nil sel means interactive mode: re-classify
// each snippet (folding in per-item overwrite arming) and let only what
// interactiveReachesExecute allows reach executePlanItem — a confirmed importable
// row (→ written, possibly an overwrite), an explicitly-armed row (→ written, or
// surfaced as the §4 hard error if it resolved to a cross-path), and a genuine
// classify error. Everything else is skipped: a deselected/unarmed row, or a
// non-armed row that re-classified to a §4 cross-path / already-exists / empty /
// invalid while the picker was open. So a non-armed confirmed import never becomes
// a §4 hard error the user could not act on, while an explicit overwrite intent
// keeps the writer authoritative. confirm-all therefore writes the same fresh-item
// bytes as a non-interactive run when no conflicts exist.
func importSnippets[R ImportRecord](deps Deps, source promptsource.Source, collisionSources []promptsource.Source, records []R, flags importFlags, sel *importSelection) (imported, skipped int, err error) {
	claimed := map[string]bool{}
	interactive := sel != nil
	destReady := false
	for _, rec := range records {
		item := classifySnippet(source, collisionSources, rec, flags, claimed, sel)
		if interactive && !interactiveReachesExecute(item, sel) {
			skipped++
			continue
		}
		// Interactive runs defer creating the auto-create destination to here, the
		// first real write, so a no-op selection (everything deselected, or a row
		// that re-classified to a skip while the picker was open) leaves no empty
		// directory. Non-interactive runs created it eagerly in runImport.
		if interactive && !destReady && item.status == planImportable && !flags.dryRun {
			if dirErr := ensureImportWriteDir(source); dirErr != nil {
				return imported, skipped, dirErr
			}
			destReady = true
		}
		outcome, writeErr := executePlanItem(item, flags)
		if writeErr != nil {
			return imported, skipped, writeErr
		}
		di, ds := reportOutcome(deps, outcome, flags.dryRun)
		imported += di
		skipped += ds
	}
	return imported, skipped, nil
}

// interactiveReachesExecute reports whether a re-classified item should reach
// executePlanItem in interactive mode. Three reach it:
//   - a genuine classify error (→ surfaced/aborted, never swallowed);
//   - an explicitly ARMED overwrite (sel.overwrite[id]) regardless of status — so an
//     armed id that resolved to a cross-path duplicate surfaces the §34 hard error
//     (exit 3) via executePlanItem's planCrossPath case rather than silently
//     skipping. This keeps the writer authoritative: per-item overwrite cannot
//     create a §4/§18 duplicate. A cross-path is never armed through the picker (it
//     renders non-selectable), so this is reachable only when arming an exact-target
//     turns up an other-path duplicate at write time: either a race, or a PRE-EXISTING
//     same-id duplicate the cheap exact-target skip-path did not surface at
//     picker-build. Both are genuine §4 violations, and arming an overwrite is the
//     same intent as the CLI --overwrite, which also hard-errors (exit 3) here — so
//     surfacing it (with the conflicting path named) is correct and consistent;
//   - a confirmed still-importable row (→ written).
//
// Everything else is skipped: a deselected/unarmed row, or a NON-armed row that
// merely raced to a conflict / exists / empty / invalid while the picker was open.
// Only explicit overwrite intent escalates a cross-path to the hard error; a plain
// fresh row that raced is still skipped (AUR-528 race-safety), so the user only ever
// writes what they saw and confirmed.
func interactiveReachesExecute(item planItem, sel *importSelection) bool {
	if item.status == planClassifyError {
		return true
	}
	if sel.overwrite[item.id] {
		return true
	}
	return item.status == planImportable && sel.write[item.id]
}

// reportOutcome emits the per-snippet line for one executed outcome and returns
// its (imported, skipped) tally contribution. A real run prints created paths to
// stdout (for scripting); a dry-run previews `would create:` / `would overwrite:`
// / `would skip:` to stderr.
func reportOutcome(deps Deps, outcome snippetOutcome, dryRun bool) (imported, skipped int) {
	if outcome.imported {
		if dryRun {
			label := "create"
			if outcome.overwrite {
				label = "overwrite"
			}
			_, _ = fmt.Fprintf(deps.Stderr, "would %s: %s\n", label, outcome.path)
		} else {
			_, _ = fmt.Fprintln(deps.Stdout, outcome.path)
		}
		return 1, 0
	}
	if dryRun {
		_, _ = fmt.Fprintf(deps.Stderr, "would skip: %s\n", outcome.skipNote)
	}
	return 0, 1
}

// snippetOutcome reports what happened (or, in dry-run, would happen) to one
// snippet: imported (path set) or skipped (skipNote describes why).
type snippetOutcome struct {
	imported  bool
	path      string
	overwrite bool // dry-run preview wording for an existing target refresh
	skipNote  string
}

// planStatus classifies what importing one snippet would do, computed write-free
// (read-only stat/walk). executePlanItem turns each status into the same
// observable outcome the original single-pass importer produced.
type planStatus int

const (
	planImportable    planStatus = iota // would be created (or refreshed under --overwrite)
	planEmptyBody                       // no usable body → skip (claims no slug)
	planInvalidID                       // minted id failed validation → skip (defensive)
	planExists                          // exact target already exists → idempotent skip
	planCrossPath                       // same-id prompt at another path → §4/§18 hard error
	planClassifyError                   // the read-only collision check errored (walk/abs)
)

// planItem is the write-free classification of one snippet, produced by
// classifySnippet. The non-interactive importer classifies and writes each item
// adjacently (importSnippets); the interactive import TUI (AUR-528) renders a whole
// batch of these — via dryRunPlan — before the user selects what to write. Because
// the TUI's plan can be stale by the time the user confirms, the TUI re-classifies
// at write time; the non-interactive path does not need to, since it never holds a
// plan across a gap.
type planItem struct {
	record        ImportRecord // source record (title via .Title(); tag source for the TUI)
	id            string       // minted/disambiguated id (natural id for planEmptyBody)
	content       []byte       // markdown to write (planImportable only)
	target        string       // absolute destination path (planImportable/planExists)
	targetExisted bool         // exact target existed at classify time (overwrite vs create)
	overwrite     bool         // effective overwrite for this id (flags.overwrite OR per-item armed)
	status        planStatus
	blocker       string // blocking path for planExists/planCrossPath
	err           error  // captured error for planClassifyError
}

// dryRunPlan classifies every snippet in the batch write-free, in order, threading
// the intra-batch id-disambiguation state (claimed) exactly as the single-pass
// importer does. It is the batch entry point the interactive import TUI (AUR-528)
// renders; the non-interactive importer instead classifies each snippet immediately
// before writing it (importSnippets), so it never relies on a batch snapshot. A
// classification error (collision walk failure, path resolution) is captured on its
// item as planClassifyError, so a full plan always exists for the TUI to show rather
// than aborting plan construction.
func dryRunPlan[R ImportRecord](source promptsource.Source, collisionSources []promptsource.Source, records []R, flags importFlags) []planItem {
	claimed := map[string]bool{}
	plan := make([]planItem, 0, len(records))
	for _, rec := range records {
		// nil selection: the picker has no per-item arming yet at plan-build time, so
		// exact-targets classify under flags.overwrite and render as [=] (skip-by-default)
		// unless the CLI --overwrite already made them importable refreshes.
		plan = append(plan, classifySnippet(source, collisionSources, rec, flags, claimed, nil))
	}
	return plan
}

// importSelection is the interactive picker's confirmed choices, threaded into the
// write replay. write is every id to write (fresh creates plus armed exact-target
// overwrites); overwrite is the subset armed for a per-item overwrite. A nil
// *importSelection means a non-interactive run (import everything, byte-identical).
type importSelection struct {
	write     map[string]bool
	overwrite map[string]bool
}

// classifySnippet maps and classifies a single snippet without writing, applying
// intra-batch id disambiguation and the skip-existing / cross-path policy. The
// claimed-map mutation order matches the original importer exactly: an empty-body
// snippet returns before disambiguateID so it never claims a slug, while an
// invalid-id snippet returns after, having already consumed its disambiguated slot.
//
// sel carries the interactive per-item overwrite arming: an id the user armed gets
// an effective overwrite of true even when --overwrite was not passed. This is the
// only way per-item overwrite enters the pipeline, and it still routes through the
// same promptCollision check, so an armed id with a same-id prompt at another path
// stays planCrossPath — arming cannot create a §4/§18 duplicate. sel == nil
// (non-interactive) leaves the effective overwrite at flags.overwrite, byte-identical.
func classifySnippet[R ImportRecord](source promptsource.Source, collisionSources []promptsource.Source, rec R, flags importFlags, claimed map[string]bool, sel *importSelection) planItem {
	id, content, ok := rec.ToPrompt(flags.tag)
	if !ok {
		// A skipped (empty-body) record is never written, so it must not claim a
		// slug — otherwise a later valid sibling with the same phrase would be
		// suffixed even though the clean slug is free.
		return planItem{record: rec, id: id, status: planEmptyBody}
	}
	id = disambiguateID(id, rec, claimed)
	// slugID + fallback always yield a valid id; this is a defensive backstop so
	// an invalid id is never written as a hidden/undiscoverable file.
	if validateNewID(id) != nil {
		return planItem{record: rec, id: id, status: planInvalidID}
	}
	target, err := filepath.Abs(filepath.Join(source.Path, id+".md"))
	if err != nil {
		return planItem{record: rec, id: id, status: planClassifyError, err: fmt.Errorf("resolve prompt file path: %w", err)}
	}

	overwrite := flags.overwrite || (sel != nil && sel.overwrite[id])
	targetExisted := pathExists(target)
	blocker, err := promptCollision(target, collisionSources, source.Scope, id, targetExisted, overwrite)
	if err != nil {
		return planItem{record: rec, id: id, status: planClassifyError, err: err}
	}
	if blocker != "" {
		if blocker == target {
			// The exact target already exists: skip-existing is a write refusal, not a
			// failure, so re-runs stay idempotent — and cheap. promptCollision
			// short-circuits here on a single stat, so an idempotent re-run never walks
			// the store (no quadratic scan, no spurious walk-error aborts).
			return planItem{record: rec, id: id, target: target, targetExisted: targetExisted, overwrite: overwrite, status: planExists, blocker: blocker}
		}
		// A same-id prompt at ANOTHER path in scope means this write would CREATE a
		// §4/§18 duplicate that overwrite cannot resolve in place, so refuse it. This
		// walk runs only when the exact target is absent (or overwrite), so idempotent
		// re-runs skip above without it. A duplicate that already coexists with the
		// exact target is a pre-existing store-level §4 violation surfaced by
		// list/send/doctor — import guards against CREATING duplicates, it does not
		// re-audit the whole store on a no-op skip.
		return planItem{record: rec, id: id, overwrite: overwrite, status: planCrossPath, blocker: blocker}
	}
	return planItem{record: rec, id: id, content: content, target: target, targetExisted: targetExisted, overwrite: overwrite, status: planImportable}
}

// executePlanItem performs the write half for one classified item, returning the
// snippetOutcome / typed error the original single-pass importer produced. Terminal
// statuses (skips, cross-path duplicate, classification error) are returned BEFORE
// the dry-run gate, so a dry-run still aborts on a §4/§18 cross-path duplicate
// exactly as a real run does — the dry-run gate only ever suppressed the write, not
// the policy. Only planImportable consults flags.dryRun.
//
// It trusts item as classified: the non-interactive caller (importSnippets)
// classifies each snippet immediately before calling this, so item reflects the
// store at the write moment (including item.overwrite && item.targetExisted, which
// together decide atomic-replace vs O_EXCL create). The interactive TUI, which
// holds a batch plan across user interaction, must re-classify before writing (AUR-528).
func executePlanItem(item planItem, flags importFlags) (snippetOutcome, error) {
	switch item.status {
	case planEmptyBody:
		return snippetOutcome{skipNote: item.id + " (empty body)"}, nil
	case planInvalidID:
		return snippetOutcome{skipNote: item.id + " (invalid id)"}, nil
	case planExists:
		return snippetOutcome{skipNote: item.blocker + " (exists)"}, nil
	case planCrossPath:
		return snippetOutcome{}, &PromptFileExistsError{ID: item.id, Path: item.blocker}
	case planClassifyError:
		return snippetOutcome{}, item.err
	case planImportable:
		// Dry-run previews the plan as classified — which ids would be created vs
		// skipped — without writing. It deliberately does NOT probe write permissions
		// or disk space: that would need a mutating probe (violating "write nothing")
		// or a racy access check that still would not guarantee the real write.
		// Environmental write failures are surfaced by the real run.
		if !flags.dryRun {
			// Overwrite (atomic replace) only when the exact target existed at classify
			// time and an overwrite was effective for this id (the CLI --overwrite flag
			// or a per-item arm); otherwise the O_EXCL create path turns a target that
			// appeared since classification into an idempotent skip rather than
			// clobbering it.
			if err := writePromptContent(item.id, item.target, item.content, item.overwrite && item.targetExisted); err != nil {
				// A concurrent writer can win the O_EXCL create race in the window
				// between classification and this write. Skip-existing is a write
				// refusal, not a failure, so treat that as an idempotent skip rather
				// than aborting the whole import.
				var exists *PromptFileExistsError
				if errors.As(err, &exists) {
					return snippetOutcome{skipNote: exists.Path + " (exists)"}, nil
				}
				return snippetOutcome{}, err
			}
		}
		return snippetOutcome{imported: true, path: item.target, overwrite: item.overwrite && item.targetExisted}, nil
	default:
		// Unreachable: every planStatus is handled above. A new status that reaches
		// here is a programming error, surfaced rather than silently skipped.
		return snippetOutcome{}, fmt.Errorf("import: unhandled plan status %d for id %q", item.status, item.id)
	}
}

// disambiguateID resolves intra-batch id collisions deterministically: the first
// snippet to claim an id keeps it; a later snippet whose phrase normalizes to the
// same id gets a `-<uuid>` suffix so no snippet is dropped. claimed tracks every
// id assigned this run (so the suffixed forms are tracked too), keeping the
// dry-run preview consistent with a real run.
//
// The suffix alone is not guaranteed unique — two snippets can share both a slug
// and a 6-char UUID prefix, or a suffixed form can equal another snippet's natural
// slug — so it loops, appending an incrementing counter until the id is free. This
// upholds the no-drop contract for intra-batch collisions even with 3+ colliding
// snippets. It is deterministic across runs because snippets are read ORDER BY id
// and the UUID is stable.
//
// It consults only the in-batch `claimed` set, never the on-disk store: a minted
// id that happens to match a prompt already on disk is left to the skip-existing
// collision check (skipped, not re-suffixed). Probing the filesystem here would
// make ids depend on pre-existing files and break idempotent re-runs — so a
// snippet whose deterministic id collides with an unrelated pre-existing prompt is
// skipped, the unavoidable cost of idempotent, filesystem-independent minting.
func disambiguateID(base string, rec ImportRecord, claimed map[string]bool) string {
	id := base
	suffix := rec.Disambiguator()
	for n := 1; claimed[id]; n++ {
		if n == 1 {
			id = base + "-" + suffix
		} else {
			id = fmt.Sprintf("%s-%s-%d", base, suffix, n)
		}
	}
	claimed[id] = true
	return id
}

func writeImportSummary(deps Deps, dryRun bool, imported, skipped int) {
	// tty-gated to stderr so stdout stays exactly the created paths for scripting
	// and piped runs emit nothing (mirrors `new`'s add-a-body hint).
	if !streamIsTTY(deps.Stderr) {
		return
	}
	if dryRun {
		_, _ = fmt.Fprintf(deps.Stderr, "would import %d, would skip %d\n", imported, skipped)
		return
	}
	_, _ = fmt.Fprintf(deps.Stderr, "imported %d, skipped %d\n", imported, skipped)
}

// resolveWisprDBPath returns the flow.sqlite path: the --db-path override if set,
// otherwise the conventional OS location. An OS with no known default requires
// --db-path (DBPathRequiredError, exit 2).
func resolveWisprDBPath(deps Deps, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	home, _ := os.UserHomeDir()
	path, ok := wispr.DefaultDBPath(runtime.GOOS, home)
	if !ok {
		return "", &wispr.DBPathRequiredError{OS: runtime.GOOS}
	}
	return path, nil
}
