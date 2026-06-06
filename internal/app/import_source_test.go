package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// fakeRecord is a non-wispr ImportRecord, proving the import engine is generic
// over the record type (it never names wispr.Snippet).
type fakeRecord struct {
	id     string
	disamb string
	title  string
	body   string
	tags   []string
}

func (r fakeRecord) ToPrompt(string) (string, []byte, bool) {
	if strings.TrimSpace(r.body) == "" {
		return r.id, nil, false
	}
	return r.id, []byte("---\ntitle: " + r.title + "\n---\n\n" + r.body + "\n"), true
}

func (r fakeRecord) Tags(tag string) []string { return append([]string{tag}, r.tags...) }
func (r fakeRecord) Title() string            { return r.title }
func (r fakeRecord) Disambiguator() string    { return r.disamb }

// fakeImportSource is a second registry entry used only in tests. Its NewCommand
// builds a subcommand that produces fakeRecords and hands them to the shared
// engine (runImport) — exactly how wispr does it.
type fakeImportSource struct {
	records []fakeRecord
}

func (fakeImportSource) Name() string { return "fake" }

func (s fakeImportSource) NewCommand(deps Deps) *cobra.Command {
	flags := importFlags{}
	cmd := &cobra.Command{
		Use:  "fake",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runImport(deps, flags, func() ([]fakeRecord, error) { return s.records, nil })
		},
	}
	cmd.Flags().BoolVar(&flags.project, "project", false, "")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "")
	cmd.Flags().BoolVar(&flags.overwrite, "overwrite", false, "")
	cmd.Flags().StringVar(&flags.tag, "tag", "fake", "")
	cmd.Flags().BoolVarP(&flags.interactive, "interactive", "i", false, "")
	return cmd
}

// executeImportSource builds an `import` parent carrying just the given source's
// subcommand (the same wiring newImportCmd does over the registry) and runs it,
// without mutating the package registry.
func executeImportSource(t *testing.T, deps Deps, src ImportSource, args ...string) error {
	t.Helper()
	var out, errb bytes.Buffer
	deps.Stdout = &out
	deps.Stderr = &errb
	root := &cobra.Command{Use: "tprompt", SilenceUsage: true, SilenceErrors: true}
	parent := &cobra.Command{Use: "import", Args: cobra.NoArgs}
	parent.AddCommand(src.NewCommand(deps))
	root.AddCommand(parent)
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(append([]string{"import"}, args...))
	return root.Execute()
}

func TestImportSource_RegistryEntryYieldsWorkingSubcommand(t *testing.T) {
	dir := t.TempDir()
	deps := newCmdDeps(t, dir)
	src := fakeImportSource{records: []fakeRecord{
		{id: "alpha", disamb: "a1", title: "alpha note", body: "first"},
	}}

	if err := executeImportSource(t, deps, src, "fake"); err != nil {
		t.Fatalf("import fake: %v", err)
	}
	// The fake record was imported through the shared engine — a registry entry
	// yields a working subcommand with no engine changes.
	if _, err := os.Stat(filepath.Join(dir, "alpha.md")); err != nil {
		t.Fatalf("fake source did not import its record: %v", err)
	}
}

func TestImportSource_EngineUsesRecordDisambiguator(t *testing.T) {
	// Two fake records minting the same base id must be disambiguated with the
	// record's OWN Disambiguator() (not anything wispr-specific): the second is
	// suffixed with its disambiguator, proving the collision policy is generic.
	dir := t.TempDir()
	deps := newCmdDeps(t, dir)
	src := fakeImportSource{records: []fakeRecord{
		{id: "note", disamb: "aaa", title: "note", body: "first"},
		{id: "note", disamb: "bbb", title: "note", body: "second"},
	}}

	if err := executeImportSource(t, deps, src, "fake"); err != nil {
		t.Fatalf("import fake: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "note.md")); err != nil {
		t.Fatalf("first record should keep the bare id: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "note-bbb.md")); err != nil {
		t.Fatalf("second record should be suffixed with its own disambiguator: %v", err)
	}
}

func TestImportSourceRegistry_DefaultIsWispr(t *testing.T) {
	if got := defaultImportSourceName(); got != "wispr" {
		t.Fatalf("defaultImportSourceName() = %q, want wispr (the bare-import dispatch default)", got)
	}
	// The real registry wires the wispr subcommand under `import`.
	importCmd := newImportCmd(fakeDeps(t))
	var names []string
	for _, c := range importCmd.Commands() {
		names = append(names, c.Name())
	}
	if !containsStr(names, "wispr") {
		t.Fatalf("import subcommands = %v, want to include wispr", names)
	}
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
