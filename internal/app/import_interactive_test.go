package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/importtui"
	"github.com/hsadler/tprompt/internal/wispr"
)

// recordingImportRenderer captures the State the picker was shown and returns a
// scripted Result, so a test can both assert what fresh items were offered and
// drive confirm/cancel/deselect without a real terminal.
type recordingImportRenderer struct {
	gotItems []importtui.Item
	decide   func([]importtui.Item) importtui.Result
	runErr   error
}

func (r *recordingImportRenderer) Run(state importtui.State) (importtui.Result, error) {
	r.gotItems = state.Items
	if r.runErr != nil {
		return importtui.Result{}, r.runErr
	}
	return r.decide(r.gotItems), nil
}

func withImportRenderer(deps Deps, r importtui.Renderer) Deps {
	deps.NewImportRenderer = func() (importtui.Renderer, error) { return r, nil }
	return deps
}

// confirmAll selects every fresh id the picker was shown — the stub equivalent
// of opening the picker and pressing Enter with the pre-checked defaults.
func confirmAll(items []importtui.Item) importtui.Result {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return importtui.Result{Action: importtui.ActionConfirm, SelectedIDs: ids}
}

// TestImportWispr_Interactive_ConfirmAllMatchesNonInteractive pins the locked
// invariant: a confirm-all interactive run writes exactly what the
// non-interactive run writes. The byte-for-byte scriptable equivalence is
// further pinned by the import_wispr_interactive testscript; here we assert the
// created-path stdout matches the non-interactive contract.
func TestImportWispr_Interactive_ConfirmAllMatchesNonInteractive(t *testing.T) {
	dir := t.TempDir()
	rec := &recordingImportRenderer{decide: confirmAll}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec)

	stdout, stderr, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i")
	if err != nil {
		t.Fatalf("interactive import: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty (non-tty)", stderr)
	}
	// The picker was shown both fresh snippets, in snippet order.
	if len(rec.gotItems) != 2 || rec.gotItems[0].ID != "organize-thoughts-prompt" || rec.gotItems[1].ID != "code-review" {
		t.Fatalf("picker items = %+v, want the two fresh snippets in order", rec.gotItems)
	}
	for _, want := range []string{
		filepath.Join(dir, "organize-thoughts-prompt.md"),
		filepath.Join(dir, "code-review.md"),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected file %q: %v", want, err)
		}
	}
}

func TestImportWispr_Interactive_CancelWritesNothing(t *testing.T) {
	dir := t.TempDir()
	rec := &recordingImportRenderer{decide: func([]importtui.Item) importtui.Result {
		return importtui.Result{Action: importtui.ActionCancel}
	}}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec)
	// Even with a tty (where the summary would normally print), cancel emits
	// nothing on either stream and exits 0.
	forceStreamsTTY(t)

	stdout, stderr, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i")
	if err != nil {
		t.Fatalf("cancel should exit 0, got: %v", err)
	}
	if stdout != "" {
		t.Errorf("cancel stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Errorf("cancel stderr = %q, want empty (no summary on cancel)", stderr)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("cancel wrote %d files, want none", len(entries))
	}
}

func TestImportWispr_Interactive_DeselectImportsOnlySelected(t *testing.T) {
	dir := t.TempDir()
	// Select only the second fresh item; the first is deselected.
	rec := &recordingImportRenderer{decide: func(items []importtui.Item) importtui.Result {
		return importtui.Result{Action: importtui.ActionConfirm, SelectedIDs: []string{items[1].ID}}
	}}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec)

	stdout, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i")
	if err != nil {
		t.Fatalf("interactive import: %v", err)
	}
	// items[0] = organize-thoughts-prompt (deselected), items[1] = code-review (kept).
	if pathExists(filepath.Join(dir, "organize-thoughts-prompt.md")) {
		t.Error("deselected fresh item was written")
	}
	if !pathExists(filepath.Join(dir, "code-review.md")) {
		t.Error("selected fresh item was not written")
	}
	if strings.Contains(stdout, "organize-thoughts-prompt.md") {
		t.Errorf("stdout reported a deselected create:\n%s", stdout)
	}
}

// TestImportWispr_Interactive_DeselectHonorsDisambiguatedID pins the same-slice
// id invariant (DECISIONS AUR-528 D4): the id the picker showed is the id the
// writer uses. Two same-phrase snippets disambiguate to `code-review` and a
// suffixed sibling; deselecting the bare slug must skip exactly that file and
// write the suffixed one.
func TestImportWispr_Interactive_DeselectHonorsDisambiguatedID(t *testing.T) {
	dir := t.TempDir()
	snips := []wispr.Snippet{
		{ID: "aaaaaaaa-0000-0000-0000-000000000000", Phrase: "code review", Replacement: "body A"},
		{ID: "bbbbbbbb-1111-0000-0000-000000000000", Phrase: "code review", Replacement: "body B"},
	}
	// Keep only the SECOND (suffixed) item; deselect the bare slug.
	rec := &recordingImportRenderer{decide: func(items []importtui.Item) importtui.Result {
		return importtui.Result{Action: importtui.ActionConfirm, SelectedIDs: []string{items[1].ID}}
	}}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: snips}), rec)

	if _, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i"); err != nil {
		t.Fatalf("interactive import: %v", err)
	}
	if rec.gotItems[0].ID != "code-review" {
		t.Fatalf("first item id = %q, want code-review (bare slug)", rec.gotItems[0].ID)
	}
	if !strings.HasPrefix(rec.gotItems[1].ID, "code-review-") {
		t.Fatalf("second item id = %q, want a suffixed code-review-*", rec.gotItems[1].ID)
	}
	if pathExists(filepath.Join(dir, "code-review.md")) {
		t.Error("deselected bare-slug item was written")
	}
	if !pathExists(filepath.Join(dir, rec.gotItems[1].ID+".md")) {
		t.Errorf("selected suffixed item %q was not written", rec.gotItems[1].ID)
	}
}

// TestImportWispr_Interactive_OverwriteShowsRefreshAsFresh pins D10: under
// --overwrite an existing target classifies planImportable, so it appears as a
// selectable fresh row and confirm-all refreshes it.
func TestImportWispr_Interactive_OverwriteShowsRefreshAsFresh(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "code-review.md")
	if err := os.WriteFile(stale, []byte("STALE content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := &recordingImportRenderer{decide: confirmAll}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec)

	if _, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i", "--overwrite"); err != nil {
		t.Fatalf("interactive overwrite: %v", err)
	}
	// The refresh row was offered alongside the create.
	ids := []string{rec.gotItems[0].ID, rec.gotItems[1].ID}
	if !contains(ids, "code-review") {
		t.Errorf("picker items = %v, want code-review offered as a refresh row", ids)
	}
	body, err := os.ReadFile(stale)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(body), "STALE") {
		t.Errorf("confirm-all did not refresh the existing file: %q", body)
	}
}

// TestImportWispr_Interactive_HiddenConflictDoesNotAbort pins that a snippet the
// picker never showed (a cross-path duplicate) does not abort an interactive
// import: it is skipped, not hard-errored, because the user could not see or
// deselect it (conflict review is AUR-529). The selected fresh snippet still
// imports and the run exits 0.
func TestImportWispr_Interactive_HiddenConflictDoesNotAbort(t *testing.T) {
	dir := t.TempDir()
	// Seed a same-stem prompt at another path so `code-review` classifies as a
	// cross-path duplicate (never shown in the picker).
	sub := filepath.Join(dir, "agents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "code-review.md"), []byte("---\ntitle: x\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := &recordingImportRenderer{decide: confirmAll}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec)

	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i")
	if err != nil {
		t.Fatalf("hidden cross-path conflict aborted interactive import: %v", err)
	}
	// Only the fresh snippet was shown, and it imported; the cross-path snippet
	// was skipped, not written, not aborted.
	if len(rec.gotItems) != 1 || rec.gotItems[0].ID != "organize-thoughts-prompt" {
		t.Fatalf("picker items = %+v, want only the fresh organize-thoughts-prompt", rec.gotItems)
	}
	if !pathExists(filepath.Join(dir, "organize-thoughts-prompt.md")) {
		t.Error("selected fresh snippet was not imported")
	}
	if pathExists(filepath.Join(dir, "code-review.md")) {
		t.Error("cross-path snippet was written at the top-level target")
	}
}

// TestImportWispr_Interactive_CancelLeavesNoAutoCreatedDir pins that cancelling
// the picker creates no destination directory. --project auto-creates the
// <gitroot>/tprompt overlay; a cancel must leave nothing behind.
func TestImportWispr_Interactive_CancelLeavesNoAutoCreatedDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	globalPrompts := filepath.Join(t.TempDir(), "global")
	if err := os.Mkdir(globalPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := &recordingImportRenderer{decide: func([]importtui.Item) importtui.Result {
		return importtui.Result{Action: importtui.ActionCancel}
	}}
	deps := withImportRenderer(importCmdDeps(t, globalPrompts, &fakeWisprReader{snippets: liveSnippets()}), rec)

	if _, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--project", "-i"); err != nil {
		t.Fatalf("cancel should exit 0: %v", err)
	}
	if pathExists(filepath.Join(root, "tprompt")) {
		t.Error("cancel created the auto-create project overlay directory")
	}
}

// TestImportWispr_Interactive_DeselectAllLeavesNoAutoCreatedDir pins that
// confirming an empty selection (the user unchecked every row) writes nothing
// and creates no auto-create destination — a no-op import has no filesystem
// side effects.
func TestImportWispr_Interactive_DeselectAllLeavesNoAutoCreatedDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	globalPrompts := filepath.Join(t.TempDir(), "global")
	if err := os.Mkdir(globalPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	// Confirm with nothing selected.
	rec := &recordingImportRenderer{decide: func([]importtui.Item) importtui.Result {
		return importtui.Result{Action: importtui.ActionConfirm, SelectedIDs: nil}
	}}
	deps := withImportRenderer(importCmdDeps(t, globalPrompts, &fakeWisprReader{snippets: liveSnippets()}), rec)

	stdout, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--project", "-i")
	if err != nil {
		t.Fatalf("empty-selection confirm should exit 0: %v", err)
	}
	if stdout != "" {
		t.Errorf("empty-selection stdout = %q, want empty", stdout)
	}
	if pathExists(filepath.Join(root, "tprompt")) {
		t.Error("empty-selection confirm created the auto-create project overlay directory")
	}
}

func TestImportWispr_Interactive_DryRunConflictIsUsageError(t *testing.T) {
	dir := t.TempDir()
	deps := importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()})

	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i", "--dry-run")
	var conflict *InteractiveDryRunConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want InteractiveDryRunConflictError", err)
	}
	if got := ExitCode(err); got != ExitUsage {
		t.Errorf("exit code = %d, want %d", got, ExitUsage)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("-i --dry-run wrote %d files, want none", len(entries))
	}
}

// TestNewImportRenderer_TTYGate covers the production factory directly: a non-tty
// environment is rejected with a usage error, while an injected test stub
// bypasses the gate (so the black-box testscript can drive `-i` over pipes).
func TestNewImportRenderer_TTYGate(t *testing.T) {
	var out, errOut bytes.Buffer
	in := strings.NewReader("")

	t.Run("non-tty is rejected", func(t *testing.T) {
		deps := ProductionDeps(&out, &errOut, in)
		_, err := deps.NewImportRenderer()
		var tty *InteractiveRequiresTTYError
		if !errors.As(err, &tty) {
			t.Fatalf("err = %v, want InteractiveRequiresTTYError", err)
		}
	})

	t.Run("test stub bypasses the gate", func(t *testing.T) {
		t.Setenv("TPROMPT_TEST_IMPORT_RENDERER", "confirm-all")
		deps := ProductionDeps(&out, &errOut, in)
		r, err := deps.NewImportRenderer()
		if err != nil {
			t.Fatalf("stub bypass returned err: %v", err)
		}
		if r == nil {
			t.Fatal("stub bypass returned nil renderer")
		}
	})
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
