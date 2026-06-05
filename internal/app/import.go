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

// wisprTag is the default provenance tag stamped on every imported prompt. It is
// configurable via `--tag` (AUR-525); for now it is the fixed default.
const wisprTag = "wispr"

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
	var dbPath string
	cmd := &cobra.Command{
		Use:   "wispr",
		Short: "Import Wispr Flow snippets as prompts",
		Long: `Wispr reads your local Wispr Flow snippets (the trigger → expansion text
you saved in Wispr) and writes each as a markdown prompt in the primary global
prompts directory. It opens Wispr's local flow.sqlite read-only — it never
writes to Wispr — and reads only snippets, never your dictation history.

Import is idempotent: a snippet whose id already exists as a prompt is skipped,
so re-running never creates duplicates. Created file paths print to stdout (one
per line, for scripting); an "imported N, skipped M" summary prints to stderr.

The Wispr database is found at its default OS location; pass --db-path to point
at a copy or a non-default install.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runImportWispr(deps, importWisprFlags{dbPath: dbPath})
		},
	}
	cmd.Flags().StringVar(&dbPath, "db-path", "", "path to Wispr Flow flow.sqlite (overrides the OS default location)")
	return cmd
}

type importWisprFlags struct {
	dbPath string
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

	source, collisionSources, err := scaffoldTargetSources(cfg, false)
	if err != nil {
		return err
	}
	if err := validatePromptSources(collisionSources, source.Path); err != nil {
		return err
	}
	if err := ensureScaffoldDir(source.Path, source.AutoCreateOnAccess); err != nil {
		return err
	}

	imported, skipped, err := importSnippets(deps, source, collisionSources, snippets)
	if err != nil {
		return err
	}

	// Summary mirrors `new`'s add-a-body hint: tty-gated to stderr so stdout
	// stays exactly the created paths for scripting and piped runs emit nothing.
	if streamIsTTY(deps.Stderr) {
		_, _ = fmt.Fprintf(deps.Stderr, "imported %d, skipped %d\n", imported, skipped)
	}
	return nil
}

// importSnippets writes each snippet to the resolved source, printing created
// paths to stdout, and returns the imported/skipped tallies. Snippets with no
// usable body and collisions with existing prompts are skipped, not errors.
func importSnippets(deps Deps, source promptsource.Source, collisionSources []promptsource.Source, snippets []wispr.Snippet) (imported, skipped int, err error) {
	for _, snip := range snippets {
		created, wasSkipped, writeErr := importOneSnippet(source, collisionSources, snip)
		if writeErr != nil {
			return imported, skipped, writeErr
		}
		if wasSkipped {
			skipped++
			continue
		}
		imported++
		_, _ = fmt.Fprintln(deps.Stdout, created)
	}
	return imported, skipped, nil
}

// importOneSnippet maps and writes a single snippet. It returns the created path
// (when written), or skipped=true when the snippet has no usable body or its id
// already exists (skip-existing: a PromptFileExistsError is a write refusal, not
// a failure, so re-runs are idempotent). Any other write error is returned.
func importOneSnippet(source promptsource.Source, collisionSources []promptsource.Source, snip wispr.Snippet) (created string, skipped bool, err error) {
	id, content, ok := snip.ToPrompt(wisprTag)
	if !ok {
		return "", true, nil
	}
	// A phrase of only punctuation/whitespace/non-ASCII can slugify to an empty
	// (or otherwise store-invalid) id. Writing `<dir>/.md` would create a hidden,
	// undiscoverable file the store skips, so refuse it here and skip instead.
	// (AUR-525 replaces this skip with a `wispr-<uuid>` fallback so such snippets
	// import under a synthetic id rather than being dropped.)
	if validateNewID(id) != nil {
		return "", true, nil
	}
	target, err := filepath.Abs(filepath.Join(source.Path, id+".md"))
	if err != nil {
		return "", false, fmt.Errorf("resolve prompt file path: %w", err)
	}
	// Skip-existing default: a PromptFileExistsError is a write refusal, not a
	// failure, so re-runs stay idempotent. Two distinct snippets whose phrases
	// normalize to the same slug also collide here, so the second is skipped;
	// AUR-525 disambiguates intra-batch slug collisions with a `-<uuid>` suffix
	// so no snippet is dropped.
	if err := writePromptFile(target, collisionSources, source.Scope, id, content, false); err != nil {
		var exists *PromptFileExistsError
		if errors.As(err, &exists) {
			return "", true, nil
		}
		return "", false, err
	}
	return target, false, nil
}

// resolveWisprDBPath returns the flow.sqlite path: the --db-path override if set,
// otherwise the conventional OS location. (The full DB-error taxonomy — missing
// path, unsupported OS — is refined in AUR-525.)
func resolveWisprDBPath(deps Deps, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	home, _ := os.UserHomeDir()
	path, ok := wispr.DefaultDBPath(runtime.GOOS, deps.Env, home)
	if !ok {
		return "", fmt.Errorf("no default Wispr Flow database location for %s; pass --db-path", runtime.GOOS)
	}
	return path, nil
}
