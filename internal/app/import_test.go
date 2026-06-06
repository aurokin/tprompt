package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/store"
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

func TestImportWispr_UnsluggablePhraseImportsUnderUUIDFallback(t *testing.T) {
	// A phrase that slugifies to empty (all punctuation / non-ASCII) must not be
	// written as a hidden `<dir>/.md` file; instead it imports under a
	// deterministic `wispr-<uuid>` fallback id (AUR-525), and the title still
	// preserves the original phrase.
	dir := t.TempDir()
	snips := []wispr.Snippet{
		{ID: "9f8e7d6c-0000-0000-0000-000000000000", Phrase: "！？", Replacement: "body for unsluggable phrase"},
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
	fallback := filepath.Join(dir, "wispr-9f8e7d6c.md")
	if _, err := os.Stat(fallback); err != nil {
		t.Errorf("unsluggable phrase should import under wispr-<uuid> fallback: %v", err)
	}
	if !strings.Contains(stdout, "good-one.md") {
		t.Errorf("the sluggable snippet should still import:\n%s", stdout)
	}
	// The fallback prompt preserves the original phrase as its title.
	body, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatalf("read fallback: %v", err)
	}
	if !strings.Contains(string(body), "title: ！？") {
		t.Errorf("fallback prompt should preserve the phrase as title:\n%s", body)
	}
}

func TestImportWispr_EmptyBodySiblingDoesNotStealSlug(t *testing.T) {
	// A skipped empty-body snippet must not reserve its slug: a later snippet whose
	// phrase normalizes to the same id should still get the clean bare slug, not a
	// `-<uuid>` suffix.
	dir := t.TempDir()
	snips := []wispr.Snippet{
		{ID: "aaaaaaaa-0000-0000-0000-000000000000", Phrase: "code review", Replacement: ""},
		{ID: "bbbbbbbb-0000-0000-0000-000000000000", Phrase: "Code Review", Replacement: "real body"},
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: snips})

	stdout, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	bare := filepath.Join(dir, "code-review.md")
	if _, err := os.Stat(bare); err != nil {
		t.Errorf("valid sibling should claim the bare slug: %v", err)
	}
	if !strings.Contains(stdout, bare) {
		t.Errorf("stdout missing %s:\n%s", bare, stdout)
	}
	// No suffixed variant should exist (the empty-body snippet wrote nothing).
	if suffixed := filepath.Join(dir, "code-review-bbbbbb.md"); pathExists(suffixed) {
		t.Errorf("empty-body sibling forced an unnecessary suffix: %s", suffixed)
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

func TestImportWispr_DryRunWritesNothingAndPreviews(t *testing.T) {
	dir := t.TempDir()
	// Seed an existing prompt so one snippet would be skipped, one created.
	if err := os.WriteFile(filepath.Join(dir, "code-review.md"), []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()})
	forceStreamsTTY(t)

	stdout, stderr, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	// stdout stays empty: nothing is created.
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("dry-run stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "would create:") || !strings.Contains(stderr, "organize-thoughts-prompt.md") {
		t.Errorf("stderr missing would-create line:\n%s", stderr)
	}
	if !strings.Contains(stderr, "would skip:") || !strings.Contains(stderr, "code-review.md") {
		t.Errorf("stderr missing would-skip line:\n%s", stderr)
	}
	if !strings.Contains(stderr, "would import 1, would skip 1") {
		t.Errorf("stderr missing dry-run summary:\n%s", stderr)
	}
	// Nothing was actually written.
	if _, err := os.Stat(filepath.Join(dir, "organize-thoughts-prompt.md")); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create files, stat err = %v", err)
	}
}

func TestImportWispr_OverwriteRefreshesExisting(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "code-review.md")
	if err := os.WriteFile(stale, []byte("STALE content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()})

	stdout, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--overwrite")
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if !strings.Contains(stdout, "code-review.md") {
		t.Errorf("overwrite should report the refreshed path:\n%s", stdout)
	}
	body, err := os.ReadFile(stale)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(body), "STALE") {
		t.Errorf("overwrite did not refresh the file: %q", body)
	}
	if !strings.Contains(string(body), "Review this code.") {
		t.Errorf("overwrite should write the snippet body: %q", body)
	}
}

func TestImportWispr_TagOverridesProvenanceTag(t *testing.T) {
	dir := t.TempDir()
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: []wispr.Snippet{
		{ID: "u1", Phrase: "thing", Replacement: "body", Starred: true},
	}})

	if _, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--tag", "from-wispr"); err != nil {
		t.Fatalf("import --tag: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "thing.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Custom provenance tag replaces the default; starred still appends.
	if !strings.Contains(string(body), "tags: [from-wispr, starred]") {
		t.Errorf("frontmatter = %s, want custom tag + starred", body)
	}
}

func TestImportWispr_IntraBatchSlugCollisionDisambiguates(t *testing.T) {
	// Two distinct snippets whose phrases normalize to the same slug must both
	// import: the first keeps the bare slug, the second gets a `-<uuid>` suffix.
	dir := t.TempDir()
	snips := []wispr.Snippet{
		{ID: "aaaaaaaa-1111-0000-0000-000000000000", Phrase: "Code Review", Replacement: "first body"},
		{ID: "bbbbbbbb-2222-0000-0000-000000000000", Phrase: "code-review", Replacement: "second body"},
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: snips})

	stdout, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	// First (lower id, ORDER BY id is applied by the real reader; the fake keeps
	// slice order) keeps the bare slug; the second is suffixed by its uuid.
	bare := filepath.Join(dir, "code-review.md")
	suffixed := filepath.Join(dir, "code-review-bbbbbb.md")
	for _, p := range []string{bare, suffixed} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s: %v", p, err)
		}
		if !strings.Contains(stdout, p) {
			t.Errorf("stdout missing %s:\n%s", p, stdout)
		}
	}
	// Bodies are distinct (no snippet dropped).
	first, _ := os.ReadFile(bare)
	second, _ := os.ReadFile(suffixed)
	if !strings.Contains(string(first), "first body") || !strings.Contains(string(second), "second body") {
		t.Errorf("collision disambiguation dropped a snippet body:\n%q\n%q", first, second)
	}
}

func TestImportWispr_TripleSlugCollisionNeverDropsASnippet(t *testing.T) {
	// Three snippets normalize to the same slug, and the two later ones share the
	// same first-6 slug-safe UUID prefix ("bbbbbb"). The bare suffix alone would
	// collide for the third, so disambiguation must extend it with a counter. All
	// three must import under distinct ids with their own bodies — no drop, no
	// clobber (the locked no-drop contract).
	dir := t.TempDir()
	snips := []wispr.Snippet{
		{ID: "aaaaaa00-0000-0000-0000-000000000000", Phrase: "Code Review", Replacement: "body A"},
		{ID: "bbbbbb11-1111-0000-0000-000000000000", Phrase: "code review", Replacement: "body B"},
		{ID: "bbbbbb22-2222-0000-0000-000000000000", Phrase: "code-review", Replacement: "body C"},
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: snips})

	stdout, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	want := map[string]string{
		"code-review.md":          "body A",
		"code-review-bbbbbb.md":   "body B",
		"code-review-bbbbbb-2.md": "body C",
	}
	for name, body := range want {
		p := filepath.Join(dir, name)
		if !strings.Contains(stdout, p) {
			t.Errorf("stdout missing %s:\n%s", name, stdout)
		}
		got, readErr := os.ReadFile(p)
		if readErr != nil {
			t.Errorf("expected %s: %v", name, readErr)
			continue
		}
		if !strings.Contains(string(got), body) {
			t.Errorf("%s should carry %q (no drop/clobber):\n%s", name, body, got)
		}
	}
}

func TestImportWispr_CrossPathDuplicateIsHardError(t *testing.T) {
	// A snippet whose id already exists as a prompt at ANOTHER path in the same
	// scope (here a subdirectory) is the §4/§18 duplicate-ID hard error, not a
	// silent skip — overwrite cannot resolve a duplicate in place. The import must
	// fail (exit 3) and not write a second same-id file at the top level.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "agents", "code-review.md")
	if err := os.WriteFile(existing, []byte("---\ntitle: x\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: []wispr.Snippet{
		{ID: "u1", Phrase: "code review", Replacement: "body"},
	}})

	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x")
	var exists *PromptFileExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError (duplicate-ID hard error)", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitPrompt)
	}
	if pathExists(filepath.Join(dir, "code-review.md")) {
		t.Errorf("must not write a duplicate-id prompt at the top level")
	}
}

func TestImportWispr_DryRunStillAbortsOnCrossPath(t *testing.T) {
	// A cross-path duplicate is a §4/§18 hard error even under --dry-run: the
	// dry-run gate only ever suppressed the write, not the policy. The preview must
	// surface the same exit-3 error a real run would, not a silent "would skip".
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "code-review.md"), []byte("---\ntitle: x\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: []wispr.Snippet{
		{ID: "u1", Phrase: "code review", Replacement: "body"},
	}})

	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--dry-run")
	var exists *PromptFileExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError under --dry-run", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitPrompt)
	}
}

func TestImportWispr_PartialProgressThenCrossPathAbort(t *testing.T) {
	// Byte-identical partial-progress semantics: the importer writes/prints each
	// snippet as it goes and aborts at the FIRST cross-path duplicate. Snippet 1
	// (fresh) must be created and printed to stdout BEFORE the abort at snippet 2
	// (cross-path) — the plan/execute split must not buffer output or roll back.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-seed a same-id prompt at another path so the SECOND snippet is cross-path.
	if err := os.WriteFile(filepath.Join(dir, "agents", "code-review.md"), []byte("---\ntitle: x\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: []wispr.Snippet{
		{ID: "u1", Phrase: "organize thoughts prompt", Replacement: "first body"}, // fresh → imported
		{ID: "u2", Phrase: "code review", Replacement: "second body"},             // cross-path → abort
	}})

	stdout, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x")
	var exists *PromptFileExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError (abort at snippet 2)", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitPrompt)
	}
	// Snippet 1's side effects landed before the abort: file on disk AND path on stdout.
	first := filepath.Join(dir, "organize-thoughts-prompt.md")
	if !pathExists(first) {
		t.Error("snippet 1 must be written before the abort at snippet 2")
	}
	if !strings.Contains(stdout, first) {
		t.Errorf("snippet 1's created path must be printed before the abort:\n%s", stdout)
	}
}

func TestImportWispr_PreExistingDuplicateSkippedNotReAudited(t *testing.T) {
	// When the exact target ALSO exists, import skips (idempotent, no-op) and does
	// not walk the store to re-detect a pre-existing cross-path duplicate. The
	// collision guard prevents *creating* a duplicate; a duplicate that already
	// coexists with the target is a store-level §4 violation surfaced by
	// list/send/doctor, not re-flagged on import's no-op skip. This keeps idempotent
	// re-runs a stat per snippet (no quadratic store walk, no walk-error aborts).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "code-review.md"), []byte("---\ntitle: x\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "code-review.md"), []byte("---\ntitle: y\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: []wispr.Snippet{
		{ID: "u1", Phrase: "code review", Replacement: "body"},
	}})

	stdout, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x")
	if err != nil {
		t.Fatalf("re-run should skip the existing target, got error: %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("nothing should be created (target exists), stdout = %q", stdout)
	}
}

func TestImportWispr_DryRunStillRejectsMissingExplicitPromptsDir(t *testing.T) {
	// --dry-run must not paper over a missing explicit prompts_dir: a real import
	// would reject it (it is not auto-created), so the preview surfaces the same
	// usage error rather than claiming a runnable import.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	deps := importCmdDeps(t, missing, &fakeWisprReader{snippets: liveSnippets()})

	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--dry-run")
	var dirMissing *store.PromptsDirMissingError
	if !errors.As(err, &dirMissing) {
		t.Fatalf("err = %T %v, want *store.PromptsDirMissingError", err, err)
	}
	if ExitCode(err) != ExitUsage {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitUsage)
	}
}

func TestImportWispr_ProjectWritesToGitOverlay(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	globalPrompts := filepath.Join(t.TempDir(), "global")
	if err := os.Mkdir(globalPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	deps := importCmdDeps(t, globalPrompts, &fakeWisprReader{snippets: []wispr.Snippet{
		{ID: "u1", Phrase: "proj prompt", Replacement: "body"},
	}})

	if _, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--project"); err != nil {
		t.Fatalf("import --project: %v", err)
	}
	// Written to the project overlay, not the global dir.
	if _, err := os.Stat(filepath.Join(root, "tprompt", "proj-prompt.md")); err != nil {
		t.Errorf("expected project overlay prompt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalPrompts, "proj-prompt.md")); !os.IsNotExist(err) {
		t.Errorf("must not write to the global dir under --project, stat err = %v", err)
	}
}

func TestImportWispr_MissingDBPathRequiredOnLinux(t *testing.T) {
	// With no --db-path and no default for the OS, the command is a usage error.
	// Force the no-default branch by clearing the override and (on Linux hosts)
	// relying on DefaultDBPath returning ok=false; on macOS the default resolves,
	// so this asserts the typed path-required error only where there is no default.
	if runtime.GOOS != "linux" {
		t.Skipf("no-default-path case only deterministic on linux, got %s", runtime.GOOS)
	}
	dir := t.TempDir()
	deps := importCmdDeps(t, dir, &fakeWisprReader{})
	_, _, err := executeRootWith(t, deps, "import", "wispr")
	var pathRequired *wispr.DBPathRequiredError
	if !errors.As(err, &pathRequired) {
		t.Fatalf("err = %T %v, want *wispr.DBPathRequiredError", err, err)
	}
	if ExitCode(err) != ExitUsage {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitUsage)
	}
}

func TestImportWispr_MissingDBIsUsageError(t *testing.T) {
	dir := t.TempDir()
	// Real reader over a nonexistent db-path → DBNotFoundError → exit 2.
	deps := newCmdDeps(t, dir)
	deps.NewWisprReader = func(p string) wispr.Reader { return wispr.NewReader(p) }
	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", filepath.Join(dir, "nope.sqlite"))
	var notFound *wispr.DBNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %T %v, want *wispr.DBNotFoundError", err, err)
	}
	if ExitCode(err) != ExitUsage {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitUsage)
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
