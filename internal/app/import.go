package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/hsadler/tprompt/internal/importtui"
	"github.com/hsadler/tprompt/internal/promptsource"
	"github.com/hsadler/tprompt/internal/wispr"
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
			// Bare `tprompt import` (no source) prints help, mirroring root.
			return c.Help()
		},
	}
	cmd.AddCommand(newImportWisprCmd(deps))
	return cmd
}

func newImportWisprCmd(deps Deps) *cobra.Command {
	flags := importWisprFlags{}
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
			return runImportWispr(deps, flags)
		},
	}
	cmd.Flags().StringVar(&flags.dbPath, "db-path", "", "path to Wispr Flow flow.sqlite (overrides the OS default location)")
	cmd.Flags().BoolVar(&flags.project, "project", false, "write to <gitroot>/tprompt instead of the global prompts directory")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "preview what would be imported; write nothing")
	cmd.Flags().BoolVar(&flags.overwrite, "overwrite", false, "replace existing prompts with the same id (refresh from Wispr)")
	cmd.Flags().StringVar(&flags.tag, "tag", defaultWisprTag, "provenance tag stamped on every imported prompt")
	cmd.Flags().BoolVarP(&flags.interactive, "interactive", "i", false, "pick which snippets to import in an interactive list (needs a terminal)")
	return cmd
}

type importWisprFlags struct {
	dbPath      string
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

func runImportWispr(deps Deps, flags importWisprFlags) error {
	// Interactive preflight runs before any DB/store work so a flag contradiction
	// or a non-tty environment fails obviously (exit 2) before side effects. A nil
	// picker means a non-interactive run.
	picker, err := interactivePicker(deps, flags)
	if err != nil {
		return err
	}

	cfg, err := deps.LoadConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}

	dbPath, err := resolveWisprDBPath(deps, flags.dbPath)
	if err != nil {
		return err
	}

	snippets, err := deps.NewWisprReader(dbPath).Snippets()
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
		return runInteractiveImport(deps, picker, source, collisionSources, snippets, flags)
	}

	imported, skipped, err := importSnippets(deps, source, collisionSources, snippets, flags, nil)
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
func prepareImportDest(source promptsource.Source, flags importWisprFlags, interactive bool) error {
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
		// anything new, since the path already exists.
		if _, err := os.Stat(source.Path); errors.Is(err, fs.ErrNotExist) {
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
func interactivePicker(deps Deps, flags importWisprFlags) (importtui.Renderer, error) {
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
func runInteractiveImport(deps Deps, picker importtui.Renderer, source promptsource.Source, collisionSources []promptsource.Source, snippets []wispr.Snippet, flags importWisprFlags) error {
	selected := map[string]bool{}
	if items := freshItems(dryRunPlan(source, collisionSources, snippets, flags)); len(items) > 0 {
		result, err := picker.Run(importtui.State{Items: items})
		if err != nil {
			return err
		}
		if result.Action == importtui.ActionCancel {
			return nil
		}
		for _, id := range result.SelectedIDs {
			selected[id] = true
		}
	}

	imported, skipped, err := importSnippets(deps, source, collisionSources, snippets, flags, selected)
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

// freshItems projects the importable (creatable / refreshable-under-overwrite)
// plan rows into picker items. Non-importable statuses (exists/crossPath/empty/
// invalid) are not shown — PR3 selects only fresh items; conflict rows are
// AUR-529.
func freshItems(plan []planItem) []importtui.Item {
	items := make([]importtui.Item, 0, len(plan))
	for _, p := range plan {
		if p.status == planImportable {
			items = append(items, importtui.Item{ID: p.id, Title: p.snippet.Phrase})
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
// selected is the interactive selection filter. nil imports every item
// (byte-identical to the non-interactive path). A non-nil set means interactive
// mode: import exactly the ids the user confirmed and skip everything else. Only
// planImportable snippets are ever shown in the picker, so a non-importable
// snippet the user could not see or deselect — a §4 cross-path duplicate, an
// already-existing target, an empty/invalid id — is skipped rather than aborting
// the import (conflict review is AUR-529). The one exception is planClassifyError:
// that is a genuine IO failure (a collision scan or path resolution that errored),
// not a policy classification, so it still surfaces in interactive mode exactly as
// it does non-interactively — never silently swallowed into a successful exit.
// confirm-all therefore writes the same fresh-item bytes as a non-interactive run
// when no hidden conflicts exist; where they do, interactive skips them instead of
// hard-erroring.
func importSnippets(deps Deps, source promptsource.Source, collisionSources []promptsource.Source, snippets []wispr.Snippet, flags importWisprFlags, selected map[string]bool) (imported, skipped int, err error) {
	claimed := map[string]bool{}
	interactive := selected != nil
	destReady := false
	for _, snip := range snippets {
		item := classifySnippet(source, collisionSources, snip, flags, claimed)
		if interactive && !selected[item.id] && item.status != planClassifyError {
			skipped++
			continue
		}
		// Interactive runs defer creating the auto-create destination to here, the
		// first real write, so a no-op selection (everything deselected, or a row
		// that re-classified to a skip while the picker was open) leaves no empty
		// directory. Non-interactive runs created it eagerly in runImportWispr.
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

// reportOutcome emits the per-snippet line for one executed outcome and returns
// its (imported, skipped) tally contribution. A real run prints created paths to
// stdout (for scripting); a dry-run previews `would create:` / `would skip:` to
// stderr.
func reportOutcome(deps Deps, outcome snippetOutcome, dryRun bool) (imported, skipped int) {
	if outcome.imported {
		if dryRun {
			_, _ = fmt.Fprintf(deps.Stderr, "would create: %s\n", outcome.path)
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
	imported bool
	path     string
	skipNote string
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
	snippet       wispr.Snippet // source snippet (title via .Phrase; tag source for the TUI)
	id            string        // minted/disambiguated id (natural id for planEmptyBody)
	content       []byte        // markdown to write (planImportable only)
	target        string        // absolute destination path (planImportable/planExists)
	targetExisted bool          // exact target existed at classify time (overwrite vs create)
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
func dryRunPlan(source promptsource.Source, collisionSources []promptsource.Source, snippets []wispr.Snippet, flags importWisprFlags) []planItem {
	claimed := map[string]bool{}
	plan := make([]planItem, 0, len(snippets))
	for _, snip := range snippets {
		plan = append(plan, classifySnippet(source, collisionSources, snip, flags, claimed))
	}
	return plan
}

// classifySnippet maps and classifies a single snippet without writing, applying
// intra-batch id disambiguation and the skip-existing / cross-path policy. The
// claimed-map mutation order matches the original importer exactly: an empty-body
// snippet returns before disambiguateID so it never claims a slug, while an
// invalid-id snippet returns after, having already consumed its disambiguated slot.
func classifySnippet(source promptsource.Source, collisionSources []promptsource.Source, snip wispr.Snippet, flags importWisprFlags, claimed map[string]bool) planItem {
	id, content, ok := snip.ToPrompt(flags.tag)
	if !ok {
		// A skipped (empty-body) snippet is never written, so it must not claim a
		// slug — otherwise a later valid sibling with the same phrase would be
		// suffixed even though the clean slug is free.
		return planItem{snippet: snip, id: id, status: planEmptyBody}
	}
	id = disambiguateID(id, snip, claimed)
	// slugID + fallback always yield a valid id; this is a defensive backstop so
	// an invalid id is never written as a hidden/undiscoverable file.
	if validateNewID(id) != nil {
		return planItem{snippet: snip, id: id, status: planInvalidID}
	}
	target, err := filepath.Abs(filepath.Join(source.Path, id+".md"))
	if err != nil {
		return planItem{snippet: snip, id: id, status: planClassifyError, err: fmt.Errorf("resolve prompt file path: %w", err)}
	}

	targetExisted := pathExists(target)
	blocker, err := promptCollision(target, collisionSources, source.Scope, id, targetExisted, flags.overwrite)
	if err != nil {
		return planItem{snippet: snip, id: id, status: planClassifyError, err: err}
	}
	if blocker != "" {
		if blocker == target {
			// The exact target already exists: skip-existing is a write refusal, not a
			// failure, so re-runs stay idempotent — and cheap. promptCollision
			// short-circuits here on a single stat, so an idempotent re-run never walks
			// the store (no quadratic scan, no spurious walk-error aborts).
			return planItem{snippet: snip, id: id, target: target, targetExisted: targetExisted, status: planExists, blocker: blocker}
		}
		// A same-id prompt at ANOTHER path in scope means this write would CREATE a
		// §4/§18 duplicate that --overwrite cannot resolve in place, so refuse it.
		// This walk runs only when the exact target is absent (or --overwrite), so
		// idempotent re-runs skip above without it. A duplicate that already coexists
		// with the exact target is a pre-existing store-level §4 violation surfaced by
		// list/send/doctor — import guards against CREATING duplicates, it does not
		// re-audit the whole store on a no-op skip.
		return planItem{snippet: snip, id: id, status: planCrossPath, blocker: blocker}
	}
	return planItem{snippet: snip, id: id, content: content, target: target, targetExisted: targetExisted, status: planImportable}
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
// store at the write moment (including item.targetExisted, which decides overwrite
// vs create). The interactive TUI, which holds a batch plan across user
// interaction, must re-classify before writing (AUR-528).
func executePlanItem(item planItem, flags importWisprFlags) (snippetOutcome, error) {
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
			// time and --overwrite was given; otherwise the O_EXCL create path turns a
			// target that appeared since classification into an idempotent skip rather
			// than clobbering it.
			if err := writePromptContent(item.id, item.target, item.content, flags.overwrite && item.targetExisted); err != nil {
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
		return snippetOutcome{imported: true, path: item.target}, nil
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
func disambiguateID(base string, snip wispr.Snippet, claimed map[string]bool) string {
	id := base
	suffix := wispr.IDPrefix(snip.ID, 6)
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
	path, ok := wispr.DefaultDBPath(runtime.GOOS, deps.Env, home)
	if !ok {
		return "", &wispr.DBPathRequiredError{OS: runtime.GOOS}
	}
	return path, nil
}
