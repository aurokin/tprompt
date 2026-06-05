package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/wispr"
)

type fakeWisprReader struct {
	snippets []wispr.Snippet
	err      error
}

func (f *fakeWisprReader) Snippets() ([]wispr.Snippet, error) {
	return f.snippets, f.err
}

func importCmdDeps(t *testing.T, promptsDir string, reader wispr.Reader) Deps {
	t.Helper()
	deps := newCmdDeps(t, promptsDir)
	deps.NewWisprReader = func(string) wispr.Reader { return reader }
	return deps
}

func liveSnippets() []wispr.Snippet {
	return []wispr.Snippet{
		{ID: "uuid-1", Phrase: "organize thoughts prompt", Replacement: "Help me organize my thoughts."},
		{ID: "uuid-2", Phrase: "code review", Replacement: "Review this code.", Starred: true},
	}
}

func TestImportWispr_WritesOnePromptPerSnippet(t *testing.T) {
	dir := t.TempDir()
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()})

	stdout, stderr, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "ignored")
	if err != nil {
		t.Fatalf("import wispr: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty (non-tty)", stderr)
	}

	// stdout is exactly the two created paths, one per line.
	want := []string{
		filepath.Join(dir, "organize-thoughts-prompt.md"),
		filepath.Join(dir, "code-review.md"),
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout lines = %d (%q), want 2", len(lines), stdout)
	}
	for _, w := range want {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout missing %q:\n%s", w, stdout)
		}
		if _, err := os.Stat(w); err != nil {
			t.Errorf("expected file %q: %v", w, err)
		}
	}

	// The starred snippet carries title + tags through to a loadable prompt.
	body, err := os.ReadFile(filepath.Join(dir, "code-review.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "title: code review") {
		t.Errorf("frontmatter missing verbatim title:\n%s", body)
	}
	if !strings.Contains(string(body), "tags: [wispr, starred]") {
		t.Errorf("frontmatter missing starred tag:\n%s", body)
	}
}

func TestImportWispr_SkipsExistingOnReRun(t *testing.T) {
	dir := t.TempDir()
	reader := &fakeWisprReader{snippets: liveSnippets()}

	// First run creates both.
	if _, _, err := executeRootWith(t, importCmdDeps(t, dir, reader), "import", "wispr", "--db-path", "x"); err != nil {
		t.Fatalf("first import: %v", err)
	}
	// Mutate one file so we can prove the re-run does not overwrite it.
	target := filepath.Join(dir, "code-review.md")
	sentinel := []byte("SENTINEL — must survive re-run\n")
	if err := os.WriteFile(target, sentinel, 0o600); err != nil {
		t.Fatalf("seed sentinel: %v", err)
	}

	stdout, _, err := executeRootWith(t, importCmdDeps(t, dir, reader), "import", "wispr", "--db-path", "x")
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("re-run stdout = %q, want empty (everything skipped)", stdout)
	}
	if got, _ := os.ReadFile(target); string(got) != string(sentinel) {
		t.Errorf("skip-existing overwrote the file: %q", got)
	}
}

func TestImportWispr_SkipsEmptyReplacement(t *testing.T) {
	dir := t.TempDir()
	snips := []wispr.Snippet{
		{ID: "uuid-1", Phrase: "good", Replacement: "usable body"},
		{ID: "uuid-2", Phrase: "empty", Replacement: ""},
		{ID: "uuid-3", Phrase: "whitespace", Replacement: "   \n"},
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: snips})

	stdout, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !strings.Contains(stdout, "good.md") {
		t.Errorf("stdout = %q, want good.md", stdout)
	}
	for _, skipped := range []string{"empty.md", "whitespace.md"} {
		if _, err := os.Stat(filepath.Join(dir, skipped)); !os.IsNotExist(err) {
			t.Errorf("%s should not be created (empty body), stat err = %v", skipped, err)
		}
	}
}

func TestImportWispr_SkipsUnsluggablePhraseInsteadOfWritingHiddenFile(t *testing.T) {
	// A phrase that slugifies to empty (all punctuation / non-ASCII) must not be
	// written as a hidden `<dir>/.md` file and reported as imported; it is skipped
	// until AUR-525 supplies a uuid fallback. The usable snippet still imports.
	dir := t.TempDir()
	snips := []wispr.Snippet{
		{ID: "uuid-1", Phrase: "!@#$%", Replacement: "body for unsluggable phrase"},
		{ID: "uuid-2", Phrase: "good one", Replacement: "usable body"},
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: snips})

	stdout, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if strings.Contains(stdout, "/.md") {
		t.Errorf("stdout reported a hidden .md file for an unsluggable phrase:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, ".md")); !os.IsNotExist(err) {
		t.Errorf("hidden .md file should not be created, stat err = %v", err)
	}
	if !strings.Contains(stdout, "good-one.md") {
		t.Errorf("the sluggable snippet should still import:\n%s", stdout)
	}
	// Only one file (the good one) exists.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("want exactly 1 created file, got %d: %v", len(entries), entries)
	}
}

func TestImportWispr_SummaryToStderrTTYOnly_NeverLeaksBody(t *testing.T) {
	dir := t.TempDir()
	forceStreamsTTY(t)
	secret := "TOP-SECRET replacement body"
	snips := []wispr.Snippet{{ID: "uuid-1", Phrase: "p", Replacement: secret}}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: snips})

	_, stderr, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !strings.Contains(stderr, "imported 1, skipped 0") {
		t.Errorf("stderr = %q, want summary line", stderr)
	}
	// The logging contract: replacement (body) content never appears in output.
	if strings.Contains(stderr, secret) {
		t.Errorf("stderr leaked replacement body: %q", stderr)
	}
}

func TestImportBare_PrintsHelp(t *testing.T) {
	// `tprompt import` with no source subcommand prints help, exit 0.
	dir := t.TempDir()
	deps := importCmdDeps(t, dir, &fakeWisprReader{})
	stdout, _, err := executeRootWith(t, deps, "import")
	if err != nil {
		t.Fatalf("import (bare): %v", err)
	}
	if !strings.Contains(stdout, "wispr") {
		t.Errorf("bare import help should mention the wispr subcommand:\n%s", stdout)
	}
}
