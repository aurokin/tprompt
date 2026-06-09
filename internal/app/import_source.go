package app

import "github.com/spf13/cobra"

// ImportRecord is one prompt-shaped item an import source yields, reduced to
// exactly what the source-agnostic import engine (classifySnippet / dryRunPlan /
// importSnippets) needs. wispr.Snippet satisfies it structurally, and any future
// source's record type can too — source commands box their concrete records into
// this interface at the source boundary before handing them to the shared engine.
type ImportRecord interface {
	// ToPrompt maps the record to a prompt id (filename stem) and markdown file
	// content; ok is false when the record has no usable body (skip it).
	ToPrompt(tag string) (id string, markdown []byte, ok bool)
	// Tags is the provenance tag set stamped on the prompt and fed to the picker's
	// search corpus (the same set, so a search hit matches what is written).
	Tags(tag string) []string
	// Title is the picker row label (a human phrase; never an id).
	Title() string
	// Disambiguator is a stable, NON-EMPTY seed the engine appends to resolve
	// intra-batch id collisions. It must be non-empty (a real source's stable id),
	// so disambiguated ids are deterministic and never collapse to "<base>-".
	Disambiguator() string
}

// ImportSource is one registered import origin. Each yields a subcommand under
// `tprompt import` and, when that subcommand runs, produces ImportRecords and
// hands them to the shared import engine (runImport). The interface is
// deliberately tiny: a source owns its own flags and record production, the
// engine owns everything downstream (collision policy, picker, write, summary).
type ImportSource interface {
	// Name is the subcommand name (e.g. "wispr") and the registry key.
	Name() string
	// NewCommand builds the cobra subcommand registered under `import`.
	NewCommand(deps Deps) *cobra.Command
	// RunDefaultInteractive runs this source through its default interactive
	// import path for bare `tprompt import` dispatch. The import parent calls this
	// after cobra has already parsed root persistent flags and accepted the parent
	// invocation as truly bare.
	RunDefaultInteractive(deps Deps) error
}

// importSourceRegistry is the ordered, non-empty list of import origins. The first
// entry is the default for the bare-`import` dispatch (DECISIONS §30/§34). It is
// read-only: newImportCmd ranges over it and dispatch reads its first name (via
// defaultImportSourceName). A second source is added here, plus its implementer.
var importSourceRegistry = []ImportSource{wisprImportSource{}}

// defaultImportSourceName is the source bare `tprompt import` dispatches to in
// tmux+tty. The registry is a non-empty built-in literal, so the first entry is
// the locked default.
func defaultImportSourceName() string {
	return defaultImportSource().Name()
}

func defaultImportSource() ImportSource {
	return importSourceRegistry[0]
}

// wisprImportSource is the sole built-in import source today: Wispr Flow snippets.
type wisprImportSource struct{}

func (wisprImportSource) Name() string { return "wispr" }

func (wisprImportSource) NewCommand(deps Deps) *cobra.Command { return newImportWisprCmd(deps) }

func (wisprImportSource) RunDefaultInteractive(deps Deps) error {
	return runImportWispr(deps, "", importFlags{
		tag:         defaultWisprTag,
		interactive: true,
	})
}
