package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

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
	return cmd
}

type importWisprFlags struct {
	dbPath    string
	project   bool
	dryRun    bool
	overwrite bool
	tag       string
}

func runImportWispr(deps Deps, flags importWisprFlags) error {
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
	// A real run ensures the destination exists, auto-creating the default global
	// dir or the project overlay. --dry-run writes nothing, so it must not create
	// the auto-create dir — but an explicit, non-auto-create prompts_dir that is
	// missing is still surfaced (a real import would reject it), so the preview
	// never claims success for a destination the import could not actually use.
	if source.AutoCreateOnAccess {
		if !flags.dryRun {
			if err := ensureScaffoldDir(source.Path, true); err != nil {
				return err
			}
		}
	} else if err := ensureScaffoldDir(source.Path, false); err != nil {
		return err
	}

	imported, skipped, err := importSnippets(deps, source, collisionSources, snippets, flags)
	if err != nil {
		return err
	}
	writeImportSummary(deps, flags.dryRun, imported, skipped)
	return nil
}

// importSnippets writes (or, under --dry-run, previews) each snippet to the
// resolved source and returns the imported/skipped tallies. Created paths go to
// stdout (real runs) or `would create:`/`would skip:` lines to stderr (dry-run).
func importSnippets(deps Deps, source promptsource.Source, collisionSources []promptsource.Source, snippets []wispr.Snippet, flags importWisprFlags) (imported, skipped int, err error) {
	claimed := map[string]bool{}
	for _, snip := range snippets {
		outcome, writeErr := importOneSnippet(source, collisionSources, snip, flags, claimed)
		if writeErr != nil {
			return imported, skipped, writeErr
		}
		if outcome.imported {
			imported++
			if flags.dryRun {
				_, _ = fmt.Fprintf(deps.Stderr, "would create: %s\n", outcome.path)
			} else {
				_, _ = fmt.Fprintln(deps.Stdout, outcome.path)
			}
			continue
		}
		skipped++
		if flags.dryRun {
			_, _ = fmt.Fprintf(deps.Stderr, "would skip: %s\n", outcome.skipNote)
		}
	}
	return imported, skipped, nil
}

// snippetOutcome reports what happened (or, in dry-run, would happen) to one
// snippet: imported (path set) or skipped (skipNote describes why).
type snippetOutcome struct {
	imported bool
	path     string
	skipNote string
}

// importOneSnippet maps and writes a single snippet, applying intra-batch id
// disambiguation and the skip-existing policy. It returns the created/would-create
// path, or a skip with a human-readable note. Only a genuine write failure (not a
// collision) is returned as an error.
func importOneSnippet(source promptsource.Source, collisionSources []promptsource.Source, snip wispr.Snippet, flags importWisprFlags, claimed map[string]bool) (snippetOutcome, error) {
	id, content, ok := snip.ToPrompt(flags.tag)
	if !ok {
		// A skipped (empty-body) snippet is never written, so it must not claim a
		// slug — otherwise a later valid sibling with the same phrase would be
		// suffixed even though the clean slug is free.
		return snippetOutcome{skipNote: id + " (empty body)"}, nil
	}
	id = disambiguateID(id, snip, claimed)
	// slugID + fallback always yield a valid id; this is a defensive backstop so
	// an invalid id is never written as a hidden/undiscoverable file.
	if validateNewID(id) != nil {
		return snippetOutcome{skipNote: id + " (invalid id)"}, nil
	}
	target, err := filepath.Abs(filepath.Join(source.Path, id+".md"))
	if err != nil {
		return snippetOutcome{}, fmt.Errorf("resolve prompt file path: %w", err)
	}

	targetExisted := pathExists(target)
	blocker, err := promptCollision(target, collisionSources, source.Scope, id, targetExisted, flags.overwrite)
	if err != nil {
		return snippetOutcome{}, err
	}
	if blocker != "" {
		if blocker == target {
			// The exact target already exists: skip-existing is a write refusal, not a
			// failure, so re-runs stay idempotent — and cheap. promptCollision
			// short-circuits here on a single stat, so an idempotent re-run never walks
			// the store (no quadratic scan, no spurious walk-error aborts).
			return snippetOutcome{skipNote: blocker + " (exists)"}, nil
		}
		// A same-id prompt at ANOTHER path in scope means this write would CREATE a
		// §4/§18 duplicate that --overwrite cannot resolve in place, so refuse it.
		// This walk runs only when the exact target is absent (or --overwrite), so
		// idempotent re-runs skip above without it. A duplicate that already coexists
		// with the exact target is a pre-existing store-level §4 violation surfaced by
		// list/send/doctor — import guards against CREATING duplicates, it does not
		// re-audit the whole store on a no-op skip.
		return snippetOutcome{}, &PromptFileExistsError{ID: id, Path: blocker}
	}
	// Dry-run previews the import PLAN — which ids would be created vs skipped, plus
	// any id/collision/duplicate errors resolved above, all determined without
	// writing. It deliberately does NOT probe write permissions or disk space:
	// surfacing those would need a mutating probe (violating "write nothing") or a
	// racy, platform-specific access check that still would not guarantee the real
	// write. Environmental write failures are surfaced by the real run instead.
	if !flags.dryRun {
		if err := writePromptContent(id, target, content, flags.overwrite && targetExisted); err != nil {
			// A concurrent writer can win the O_EXCL create race in the window
			// between the check above and this write. Skip-existing is a write
			// refusal, not a failure, so treat that as an idempotent skip rather
			// than aborting the whole import.
			var exists *PromptFileExistsError
			if errors.As(err, &exists) {
				return snippetOutcome{skipNote: exists.Path + " (exists)"}, nil
			}
			return snippetOutcome{}, err
		}
	}
	return snippetOutcome{imported: true, path: target}, nil
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
