package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aurokin/tprompt/internal/importtui"
	"github.com/aurokin/tprompt/internal/promptsource"
	"github.com/aurokin/tprompt/internal/wispr"
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

// confirmAll selects every id the picker was shown — the stub equivalent of
// opening the picker and pressing Enter with the pre-checked defaults. (It echoes
// every id, including non-selectable rows, to exercise the writer's own filter;
// the model is unit-tested separately for real selectability.)
func confirmAll(items []importtui.Item) importtui.Result {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return importtui.Result{Action: importtui.ActionConfirm, SelectedIDs: ids}
}

// confirmAllArmingOverwrites simulates a user who checks every selectable row and
// arms each exact-target conflict for overwrite (cross-path rows stay unselected).
func confirmAllArmingOverwrites(items []importtui.Item) importtui.Result {
	var selected, overwrite []string
	for _, it := range items {
		switch it.Conflict {
		case importtui.ConflictNone:
			selected = append(selected, it.ID)
		case importtui.ConflictExactTarget:
			selected = append(selected, it.ID)
			overwrite = append(overwrite, it.ID)
		}
	}
	return importtui.Result{Action: importtui.ActionConfirm, SelectedIDs: selected, OverwriteIDs: overwrite}
}

// findItem returns the picker item with the given id, or nil.
func findItem(items []importtui.Item, id string) *importtui.Item {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
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

// TestImportWispr_Interactive_OverwriteShowsRefreshAsArmed pins the AUR-528 D10
// behavior as completed by AUR-529: under --overwrite an existing target
// classifies planImportable, and the picker shows it as a PRE-ARMED exact-target
// overwrite (not a fresh create), so the glyph/counter report the refresh
// truthfully; confirm-all refreshes it.
func TestImportWispr_Interactive_OverwriteShowsRefreshAsArmed(t *testing.T) {
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
	// The refresh row was offered as a pre-armed exact-target overwrite.
	refresh := findItem(rec.gotItems, "code-review")
	if refresh == nil {
		t.Fatalf("picker items = %+v, want code-review offered as a refresh row", rec.gotItems)
	}
	if refresh.Conflict != importtui.ConflictExactTarget || !refresh.Armed {
		t.Errorf("code-review = %+v, want a pre-armed ConflictExactTarget refresh", *refresh)
	}
	body, err := os.ReadFile(stale)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(body), "STALE") {
		t.Errorf("confirm-all did not refresh the existing file: %q", body)
	}
}

// TestImportWispr_Interactive_CrossPathShownButWritesNothing pins that a
// cross-path duplicate is now SHOWN in the picker (AUR-529) as a non-selectable
// row carrying the conflicting path, but confirming around it writes nothing and
// does not abort: the fresh snippet still imports and the run exits 0.
func TestImportWispr_Interactive_CrossPathShownButWritesNothing(t *testing.T) {
	dir := t.TempDir()
	// Seed a same-stem prompt at another path so `code-review` classifies as a
	// cross-path duplicate.
	sub := filepath.Join(dir, "agents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	conflictPath := filepath.Join(sub, "code-review.md")
	if err := os.WriteFile(conflictPath, []byte("---\ntitle: x\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Real picker behavior: confirm only the selectable fresh row; the cross-path
	// row cannot be checked, so its id is not returned.
	rec := &recordingImportRenderer{decide: func(items []importtui.Item) importtui.Result {
		return importtui.Result{Action: importtui.ActionConfirm, SelectedIDs: []string{"organize-thoughts-prompt"}}
	}}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec)

	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i")
	if err != nil {
		t.Fatalf("cross-path conflict aborted interactive import: %v", err)
	}
	// Both rows were shown; the cross-path row carries its kind and the blocker.
	cross := findItem(rec.gotItems, "code-review")
	if cross == nil {
		t.Fatalf("picker items = %+v, want code-review shown as a cross-path row", rec.gotItems)
	}
	if cross.Conflict != importtui.ConflictCrossPath {
		t.Errorf("code-review Conflict = %v, want ConflictCrossPath", cross.Conflict)
	}
	if cross.Blocker != conflictPath {
		t.Errorf("code-review Blocker = %q, want the conflicting path %q", cross.Blocker, conflictPath)
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
// TestImportWispr_Interactive_FirstRunAutoCreateShowsAndImports pins that a
// first interactive import into a not-yet-created auto-create destination
// (--project overlay missing) still classifies fresh snippets as importable —
// the missing destination is handled gracefully by the collision scan — so the
// picker shows them and confirming creates the overlay and writes the prompts.
// (Deferring directory creation past selection does not blank the picker.)
func TestImportWispr_Interactive_FirstRunAutoCreateShowsAndImports(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	globalPrompts := filepath.Join(t.TempDir(), "global")
	if err := os.Mkdir(globalPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := &recordingImportRenderer{decide: confirmAll}
	deps := withImportRenderer(importCmdDeps(t, globalPrompts, &fakeWisprReader{snippets: liveSnippets()}), rec)

	if _, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--project", "-i"); err != nil {
		t.Fatalf("first-run --project interactive import: %v", err)
	}
	// The picker was offered the fresh snippets even though the overlay did not exist.
	if len(rec.gotItems) != 2 {
		t.Fatalf("picker items = %+v, want both fresh snippets on a first run", rec.gotItems)
	}
	// Confirming created the overlay and wrote the prompts into it.
	if !pathExists(filepath.Join(root, "tprompt", "code-review.md")) {
		t.Error("first-run interactive import did not write into the created project overlay")
	}
}

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

// TestImportWispr_Interactive_ClassifyErrorStillSurfaces pins that a genuine IO
// failure during classification (an unreadable subtree under the prompts dir
// breaks the collision scan) is NOT swallowed by the interactive selection
// filter: it surfaces as an error and a non-zero exit, exactly as a
// non-interactive run would, rather than silently exiting 0 having imported
// nothing.
func TestImportWispr_Interactive_ClassifyErrorStillSurfaces(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions, so the unreadable subtree would not error")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) }) // let TempDir cleanup remove it
	// Confirm the chmod actually denies traversal; some filesystems ignore it.
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("filesystem does not enforce the unreadable subtree; cannot provoke a walk error")
	}

	rec := &recordingImportRenderer{decide: confirmAll}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec)

	if _, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i"); err == nil {
		t.Fatal("interactive import swallowed a collision-scan failure and exited 0")
	}
}

// TestImportWispr_Interactive_SelectedRowRacedToConflictSkips pins that a row
// the user kept checked but which becomes a cross-path conflict while the picker
// is open is skipped (not aborted) at write time — the same as a pre-existing
// hidden conflict. The conflict is created from inside the picker's decide hook,
// after the plan was shown but before the writer re-classifies.
func TestImportWispr_Interactive_SelectedRowRacedToConflictSkips(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "agents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := &recordingImportRenderer{decide: func(items []importtui.Item) importtui.Result {
		// Race: another prompt with the `code-review` id appears at a second path
		// after the picker showed code-review as importable.
		if err := os.WriteFile(filepath.Join(sub, "code-review.md"), []byte("---\ntitle: x\n---\n\nb\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return confirmAll(items)
	}}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec)

	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i")
	if err != nil {
		t.Fatalf("a raced cross-path conflict aborted the run instead of skipping: %v", err)
	}
	// The non-conflicting selection still imported; the raced row was skipped.
	if !pathExists(filepath.Join(dir, "organize-thoughts-prompt.md")) {
		t.Error("the non-conflicting confirmed row was not imported")
	}
	if pathExists(filepath.Join(dir, "code-review.md")) {
		t.Error("the raced cross-path row was written at the top-level target")
	}
}

// TestImportWispr_Interactive_BrokenSymlinkDestFailsBeforePicker pins that a
// destination occupied by a broken symlink (which os.Stat reports as
// ErrNotExist) is still caught by the eager preflight, not deferred — the picker
// never opens for it.
func TestImportWispr_Interactive_BrokenSymlinkDestFailsBeforePicker(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if err := os.Symlink(filepath.Join(root, "nonexistent-target"), filepath.Join(root, "tprompt")); err != nil {
		t.Fatal(err)
	}
	globalPrompts := filepath.Join(t.TempDir(), "global")
	if err := os.Mkdir(globalPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := &recordingImportRenderer{decide: confirmAll}
	deps := withImportRenderer(importCmdDeps(t, globalPrompts, &fakeWisprReader{snippets: liveSnippets()}), rec)

	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--project", "-i")
	if err == nil {
		t.Fatal("expected an error for a broken-symlink destination")
	}
	if rec.gotItems != nil {
		t.Error("picker opened for an unusable broken-symlink destination")
	}
}

// TestImportWispr_Interactive_NonDirDestinationFailsBeforePicker pins that an
// auto-create destination occupied by a regular file is surfaced eagerly (before
// the picker opens), not deferred to confirm — so a cancel can't hide an
// unusable destination. The picker's Run is never reached.
func TestImportWispr_Interactive_NonDirDestinationFailsBeforePicker(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	// Occupy the project overlay path with a regular file.
	if err := os.WriteFile(filepath.Join(root, "tprompt"), []byte("not a dir\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	globalPrompts := filepath.Join(t.TempDir(), "global")
	if err := os.Mkdir(globalPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := &recordingImportRenderer{decide: confirmAll}
	deps := withImportRenderer(importCmdDeps(t, globalPrompts, &fakeWisprReader{snippets: liveSnippets()}), rec)

	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "--project", "-i")
	if err == nil {
		t.Fatal("expected an error for a non-directory destination")
	}
	if got := ExitCode(err); got != ExitUsage {
		t.Errorf("exit code = %d, want %d", got, ExitUsage)
	}
	if rec.gotItems != nil {
		t.Error("picker opened for an unusable destination; should fail before the picker")
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

// TestImportWispr_Interactive_PerItemOverwriteRefreshesExisting pins that arming
// an exact-target conflict (without the CLI --overwrite flag) routes through the
// existing overwrite write-path: the existing file is refreshed from the snippet,
// while a fresh row in the same run is created. Exact-target-only per-item overwrite.
func TestImportWispr_Interactive_PerItemOverwriteRefreshesExisting(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "code-review.md")
	if err := os.WriteFile(stale, []byte("STALE content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := &recordingImportRenderer{decide: confirmAllArmingOverwrites}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec)

	if _, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i"); err != nil {
		t.Fatalf("per-item overwrite import: %v", err)
	}
	// code-review was offered as an exact-target conflict, armed, and refreshed.
	if it := findItem(rec.gotItems, "code-review"); it == nil || it.Conflict != importtui.ConflictExactTarget {
		t.Fatalf("code-review item = %+v, want ConflictExactTarget", it)
	}
	body, err := os.ReadFile(stale)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(body), "STALE") {
		t.Errorf("armed exact-target was not overwritten: %q", body)
	}
	if !strings.Contains(string(body), "Review this code.") {
		t.Errorf("refreshed file missing snippet body: %q", body)
	}
	// The fresh row in the same run was still created.
	if !pathExists(filepath.Join(dir, "organize-thoughts-prompt.md")) {
		t.Error("fresh row was not imported alongside the armed overwrite")
	}
}

// TestImportWispr_Interactive_ForcedCrossPathOverwriteIsHardError pins that the
// writer stays authoritative: even when a cross-path id is FORCED into the
// overwrite set (bypassing the picker's non-selectability), per-item overwrite
// cannot create a §4/§18 duplicate — the write is refused with a prompt-store hard
// error (exit 3) and nothing is written at the exact target.
func TestImportWispr_Interactive_ForcedCrossPathOverwriteIsHardError(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "agents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Same-id prompt at another path; the exact target dir/code-review.md is absent.
	if err := os.WriteFile(filepath.Join(sub, "code-review.md"), []byte("---\ntitle: x\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	onlyCodeReview := []wispr.Snippet{{ID: "uuid-2", Phrase: "code review", Replacement: "Review this code."}}
	rec := &recordingImportRenderer{decide: func(items []importtui.Item) importtui.Result {
		// Force the cross-path row into both the write and overwrite sets.
		return importtui.Result{Action: importtui.ActionConfirm, SelectedIDs: []string{"code-review"}, OverwriteIDs: []string{"code-review"}}
	}}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: onlyCodeReview}), rec)

	_, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i")
	var exists *PromptFileExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("err = %v, want PromptFileExistsError (writer refuses a forced cross-path)", err)
	}
	if got := ExitCode(err); got != ExitPrompt {
		t.Errorf("exit code = %d, want %d", got, ExitPrompt)
	}
	if pathExists(filepath.Join(dir, "code-review.md")) {
		t.Error("a forced cross-path overwrite wrote a duplicate at the exact target")
	}
}

// TestImportWispr_Interactive_IdempotentReRunAllSkip pins that re-running an
// interactive import over already-imported prompts defaults to all-skip: every
// row is now an exact-target conflict (skip-by-default), so confirm-all without
// arming writes nothing and leaves the files untouched.
func TestImportWispr_Interactive_IdempotentReRunAllSkip(t *testing.T) {
	dir := t.TempDir()
	rec := &recordingImportRenderer{decide: confirmAll}
	deps := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec)

	if _, _, err := executeRootWith(t, deps, "import", "wispr", "--db-path", "x", "-i"); err != nil {
		t.Fatalf("first import: %v", err)
	}
	created := filepath.Join(dir, "code-review.md")
	before, err := os.ReadFile(created)
	if err != nil {
		t.Fatalf("read after first import: %v", err)
	}

	// Re-run: every row is now an exact-target conflict; confirm-all does not arm.
	rec2 := &recordingImportRenderer{decide: confirmAll}
	deps2 := withImportRenderer(importCmdDeps(t, dir, &fakeWisprReader{snippets: liveSnippets()}), rec2)
	stdout, _, err := executeRootWith(t, deps2, "import", "wispr", "--db-path", "x", "-i")
	if err != nil {
		t.Fatalf("re-run import: %v", err)
	}
	if it := findItem(rec2.gotItems, "code-review"); it == nil || it.Conflict != importtui.ConflictExactTarget {
		t.Fatalf("re-run code-review item = %+v, want ConflictExactTarget", it)
	}
	if stdout != "" {
		t.Errorf("re-run created paths = %q, want none (all skipped)", stdout)
	}
	after, err := os.ReadFile(created)
	if err != nil {
		t.Fatalf("read after re-run: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("re-run modified an existing prompt: before=%q after=%q", before, after)
	}
}

// TestClassifySnippet_ArmedExactTargetBecomesImportable pins the classification
// transition per-item overwrite relies on: an exact-target whose id is armed
// (in sel.overwrite) classifies planImportable with overwrite=true — NOT planExists
// — so it overwrites rather than skips. Without arming the same snippet is planExists.
func TestClassifySnippet_ArmedExactTargetBecomesImportable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "code-review.md")
	if err := os.WriteFile(target, []byte("STALE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := promptsource.Source{Path: dir, Scope: promptsource.ScopeGlobal}
	sources := []promptsource.Source{source}
	snip := wispr.Snippet{ID: "uuid-2", Phrase: "code review", Replacement: "Review this code."}
	flags := importFlags{tag: defaultWisprTag}

	unarmed := classifySnippet(source, sources, snip, flags, map[string]bool{}, nil)
	if unarmed.status != planExists {
		t.Fatalf("unarmed status = %v, want planExists", unarmed.status)
	}

	sel := &importSelection{write: map[string]bool{"code-review": true}, overwrite: map[string]bool{"code-review": true}}
	armed := classifySnippet(source, sources, snip, flags, map[string]bool{}, sel)
	if armed.status != planImportable {
		t.Fatalf("armed status = %v, want planImportable", armed.status)
	}
	if !armed.overwrite {
		t.Error("armed item.overwrite = false, want true")
	}
	if !armed.targetExisted {
		t.Error("armed item.targetExisted = false, want true (overwrite, not create)")
	}
}
