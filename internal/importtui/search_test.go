package importtui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// sendCmd is send's sibling that also returns the emitted command, for asserting
// on tea.Quit (cancel/confirm) vs. a no-op key.
func sendCmd(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func cmdIsQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func visibleIDs(m Model) []string {
	out := make([]string, len(m.visible))
	for i, it := range m.visible {
		out[i] = it.ID
	}
	return out
}

func typeRunes(m Model, s string) Model {
	for _, r := range s {
		m = send(m, keyRune(r))
	}
	return m
}

func TestModel_SlashEntersSearchMode(t *testing.T) {
	m, cmd := sendCmd(NewModel(sampleState()), keyRune('/'))
	if m.mode != modeSearch {
		t.Fatalf("mode = %v, want modeSearch", m.mode)
	}
	if cmd != nil {
		t.Fatalf("/ must not emit a cmd, got %T", cmd())
	}
	if m.query != "" {
		t.Fatalf("query = %q, want empty on entering search", m.query)
	}
}

func TestModel_SearchFiltersRows(t *testing.T) {
	// sampleState ids: code-review, organize-thoughts, fire. "fire" matches only
	// the fire row (no f-i-r-e subsequence in the other ids/titles).
	m := NewModel(sampleState())
	m = send(m, keyRune('/'))
	m = typeRunes(m, "fire")
	if got := visibleIDs(m); len(got) != 1 || got[0] != "fire" {
		t.Fatalf("visible under %q = %v, want [fire]", m.query, got)
	}
}

func TestModel_SearchMatchesTagsCorpus(t *testing.T) {
	// "starred" appears only as a tag on beta, and is not a subsequence of either
	// row's id or title — so a hit proves the tags corpus is searched.
	state := State{Items: []Item{
		{ID: "alpha", Title: "first", Conflict: ConflictNone, Tags: []string{"wispr"}},
		{ID: "beta", Title: "second", Conflict: ConflictNone, Tags: []string{"wispr", "starred"}},
	}}
	m := NewModel(state)
	m = send(m, keyRune('/'))
	m = typeRunes(m, "starred")
	if got := visibleIDs(m); len(got) != 1 || got[0] != "beta" {
		t.Fatalf("visible under %q = %v, want [beta] (matched on the starred tag)", m.query, got)
	}
}

func TestModel_SearchMatchesID(t *testing.T) {
	state := State{Items: []Item{
		{ID: "kubernetes-deploy", Title: "ship it", Conflict: ConflictNone},
		{ID: "git-rebase", Title: "history", Conflict: ConflictNone},
	}}
	m := NewModel(state)
	m = send(m, keyRune('/'))
	m = typeRunes(m, "kube")
	if got := visibleIDs(m); len(got) != 1 || got[0] != "kubernetes-deploy" {
		t.Fatalf("visible under %q = %v, want [kubernetes-deploy]", m.query, got)
	}
}

func TestModel_SearchMatchesTitle(t *testing.T) {
	state := State{Items: []Item{
		{ID: "p1", Title: "deploy to kubernetes", Conflict: ConflictNone},
		{ID: "p2", Title: "rebase history", Conflict: ConflictNone},
	}}
	m := NewModel(state)
	m = send(m, keyRune('/'))
	m = typeRunes(m, "kube")
	if got := visibleIDs(m); len(got) != 1 || got[0] != "p1" {
		t.Fatalf("visible under %q = %v, want [p1]", m.query, got)
	}
}

// In search mode every printable rune is query text — including a/j/k/space —
// so they must NOT trigger select-all / move / toggle (AUR-530 D9).
func TestModel_SearchLetterAAppendsNotSelectAll(t *testing.T) {
	m := NewModel(conflictState())     // fresh pre-checked; cursor at fresh
	m = send(m, keyType(tea.KeySpace)) // deselect fresh
	if m.selected["fresh"] {
		t.Fatalf("setup: fresh should be deselected")
	}
	m = send(m, keyRune('/'))
	m = send(m, keyRune('a'))
	if m.query != "a" {
		t.Fatalf("query = %q, want %q ('a' appends in search)", m.query, "a")
	}
	if m.selected["fresh"] {
		t.Fatalf("'a' in search must not re-select-all (fresh got re-selected)")
	}
}

func TestModel_SearchSpaceAppendsToQuery(t *testing.T) {
	m := NewModel(sampleState())
	m = send(m, keyRune('/'))
	m = typeRunes(m, "code")
	m = send(m, keyType(tea.KeySpace))
	if m.query != "code " {
		t.Fatalf("query = %q, want %q (KeySpace appends)", m.query, "code ")
	}
	// And a space delivered as KeyRunes also appends.
	m = send(m, keyRune(' '))
	if m.query != "code  " {
		t.Fatalf("query = %q, want %q (rune space appends)", m.query, "code  ")
	}
}

func TestModel_SearchBackspacePopsRune(t *testing.T) {
	m := NewModel(sampleState())
	m = send(m, keyRune('/'))
	m = typeRunes(m, "abc")
	m = send(m, keyType(tea.KeyBackspace))
	if m.query != "ab" {
		t.Fatalf("query = %q, want %q after backspace", m.query, "ab")
	}
	// Backspace on an empty query is a no-op (stays in search).
	m = NewModel(sampleState())
	m = send(m, keyRune('/'))
	m = send(m, keyType(tea.KeyBackspace))
	if m.query != "" || m.mode != modeSearch {
		t.Fatalf("empty backspace: query=%q mode=%v, want empty/modeSearch", m.query, m.mode)
	}
}

func TestModel_SearchEscClearsFilterDoesNotCancel(t *testing.T) {
	m := NewModel(sampleState())
	m = send(m, keyRune('/'))
	m = send(m, keyRune('f'))
	if m.query != "f" {
		t.Fatalf("pre-esc query = %q, want f", m.query)
	}

	m, cmd := sendCmd(m, keyType(tea.KeyEsc))
	if m.mode != modeList {
		t.Fatalf("mode = %v, want modeList after esc", m.mode)
	}
	if m.query != "" {
		t.Fatalf("query = %q, want empty after esc", m.query)
	}
	if cmdIsQuit(cmd) {
		t.Fatal("Esc in search must not quit the picker")
	}
	if m.result.Action == ActionCancel {
		t.Fatal("Esc in search must not set ActionCancel")
	}
	// The full list is restored.
	if got := visibleIDs(m); len(got) != len(sampleState().Items) {
		t.Fatalf("visible after esc = %v, want the full list", got)
	}
}

func TestModel_SearchCtrlCCancels(t *testing.T) {
	m := NewModel(sampleState())
	m = send(m, keyRune('/'))
	m, cmd := sendCmd(m, keyType(tea.KeyCtrlC))
	if !cmdIsQuit(cmd) {
		t.Fatal("Ctrl+C in search must quit")
	}
	if m.Result().Action != ActionCancel {
		t.Fatalf("action = %v, want ActionCancel", m.Result().Action)
	}
}

func TestModel_SearchEnterCommitsFilterKeepsQuery(t *testing.T) {
	m := NewModel(sampleState())
	m = send(m, keyRune('/'))
	m = typeRunes(m, "fire")
	m, cmd := sendCmd(m, keyType(tea.KeyEnter))
	if cmdIsQuit(cmd) {
		t.Fatal("Enter in search commits the filter; it must not quit")
	}
	if m.mode != modeList {
		t.Fatalf("mode = %v, want modeList after commit", m.mode)
	}
	if m.query != "fire" {
		t.Fatalf("query = %q, want %q kept after commit", m.query, "fire")
	}
	if got := visibleIDs(m); len(got) != 1 || got[0] != "fire" {
		t.Fatalf("committed filter visible = %v, want [fire]", got)
	}
}

// select-all over an active filter resets only the visible rows and leaves
// off-filter selections untouched (AUR-530 D7).
func TestModel_SelectAllRespectsFilterAndLeavesOffFilterAlone(t *testing.T) {
	state := State{Items: []Item{
		{ID: "apple", Title: "x", Conflict: ConflictNone},
		{ID: "apply", Title: "y", Conflict: ConflictNone},
		{ID: "banana", Title: "z", Conflict: ConflictNone},
	}}
	m := NewModel(state)
	// Deselect banana — an off-filter row.
	m = send(m, keyType(tea.KeyDown))
	m = send(m, keyType(tea.KeyDown))
	m = send(m, keyType(tea.KeySpace))
	// Filter to the "app" rows (banana has no 'p') and commit.
	m = send(m, keyRune('/'))
	m = typeRunes(m, "app")
	if got := visibleIDs(m); len(got) != 2 {
		t.Fatalf("visible under 'app' = %v, want apple + apply", got)
	}
	m = send(m, keyType(tea.KeyEnter)) // commit filter → modeList, query kept
	// Deselect both visible rows, then select-all over the filter.
	m = send(m, keyType(tea.KeySpace)) // cursor 0
	m = send(m, keyType(tea.KeyDown))
	m = send(m, keyType(tea.KeySpace)) // other visible row
	m = send(m, keyRune('a'))          // select-all over visible only
	m = send(m, keyType(tea.KeyEnter)) // confirm

	got := m.Result().SelectedIDs
	if contains(got, "banana") {
		t.Fatalf("SelectedIDs = %v, off-filter banana must stay deselected through select-all", got)
	}
	if !contains(got, "apple") || !contains(got, "apply") {
		t.Fatalf("SelectedIDs = %v, want both filtered rows re-selected by 'a'", got)
	}
}

// `a` must never leave a surprise destructive overwrite armed (AUR-529's safety
// contract): an exact-target armed ad-hoc and then filtered out of view is
// disarmed globally by select-all, even though it is not in the visible set.
func TestModel_SelectAllDisarmsHiddenOverwrite(t *testing.T) {
	state := State{Items: []Item{
		{ID: "apple", Title: "x", Conflict: ConflictNone},
		{ID: "exists", Title: "dup", Conflict: ConflictExactTarget},
	}}
	m := NewModel(state)
	// Arm the exact-target overwrite ad-hoc.
	m = send(m, keyType(tea.KeyDown))  // → exists
	m = send(m, keyType(tea.KeySpace)) // arm
	// Filter so the armed row is hidden, commit, then select-all.
	m = send(m, keyRune('/'))
	m = typeRunes(m, "apple")
	if got := visibleIDs(m); len(got) != 1 || got[0] != "apple" {
		t.Fatalf("visible = %v, want [apple] (exists hidden)", got)
	}
	m = send(m, keyType(tea.KeyEnter)) // commit filter → modeList, query kept
	m = send(m, keyRune('a'))          // select-all
	m = send(m, keyType(tea.KeyEnter)) // confirm

	got := m.Result()
	if contains(got.OverwriteIDs, "exists") {
		t.Fatalf("OverwriteIDs = %v, a hidden ad-hoc overwrite must be disarmed by select-all", got.OverwriteIDs)
	}
	if contains(got.SelectedIDs, "exists") {
		t.Fatalf("SelectedIDs = %v, the disarmed hidden overwrite must not be written", got.SelectedIDs)
	}
}

// A CLI-authorized refresh (Armed) that is filtered out of view is NOT a
// surprise, so `a` leaves it armed even while hidden — the complement of the
// ad-hoc disarm above.
func TestModel_SelectAllKeepsHiddenCLIAuthorizedRefresh(t *testing.T) {
	state := State{Items: []Item{
		{ID: "apple", Title: "x", Conflict: ConflictNone},
		{ID: "refresh", Title: "dup", Conflict: ConflictExactTarget, Armed: true},
	}}
	m := NewModel(state)
	m = send(m, keyRune('/'))
	m = typeRunes(m, "apple") // hides refresh
	m = send(m, keyType(tea.KeyEnter))
	m = send(m, keyRune('a'))
	m = send(m, keyType(tea.KeyEnter))

	got := m.Result()
	if !contains(got.OverwriteIDs, "refresh") {
		t.Fatalf("OverwriteIDs = %v, a hidden CLI-authorized refresh must survive select-all", got.OverwriteIDs)
	}
}

// A selection made before search survives entering, typing, clearing, and
// leaving search — selection is keyed by id, independent of mode/filter.
func TestModel_SelectionSurvivesSearchRoundTrip(t *testing.T) {
	m := NewModel(sampleState())       // 3 fresh, all pre-checked
	m = send(m, keyType(tea.KeySpace)) // deselect code-review (cursor 0)

	m = send(m, keyRune('/'))
	m = typeRunes(m, "organize")
	m = send(m, keyType(tea.KeyEsc)) // clear filter, back to full list (not cancel)

	m, cmd := sendCmd(m, keyType(tea.KeyEnter)) // confirm
	if !cmdIsQuit(cmd) {
		t.Fatal("Enter in list must confirm (quit)")
	}
	got := m.Result()
	if got.Action != ActionConfirm {
		t.Fatalf("action = %v, want ActionConfirm (esc cleared the filter, did not cancel)", got.Action)
	}
	if contains(got.SelectedIDs, "code-review") {
		t.Fatalf("SelectedIDs = %v, code-review must stay deselected across the search round trip", got.SelectedIDs)
	}
	if len(got.SelectedIDs) != 2 {
		t.Fatalf("SelectedIDs = %v, want the other 2 still selected", got.SelectedIDs)
	}
}

// A committed filter active in list mode must be visible (so a partial list is
// never mistaken for the full list) and Esc must clear it rather than cancel.
func TestModel_ListShowsActiveFilterAndEscClearsIt(t *testing.T) {
	state := State{Items: []Item{
		{ID: "apple", Title: "x", Conflict: ConflictNone},
		{ID: "banana", Title: "y", Conflict: ConflictNone},
	}}
	m := NewModel(state)
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 20})
	m = send(m, keyRune('/'))
	m = typeRunes(m, "apple")
	m = send(m, keyType(tea.KeyEnter)) // commit filter → modeList, query kept
	if m.mode != modeList || m.query != "apple" {
		t.Fatalf("setup: mode=%v query=%q, want modeList/apple", m.mode, m.query)
	}

	view := m.View()
	if !strings.Contains(view, "filtered") {
		t.Fatalf("committed-filter list view must show a filter badge:\n%s", view)
	}
	if !strings.Contains(view, "esc clear filter") {
		t.Fatalf("committed-filter list footer must say esc clears the filter:\n%s", view)
	}
	if strings.Contains(view, "banana") {
		t.Fatalf("filtered-out row must not render in the committed-filter list:\n%s", view)
	}

	// Esc clears the filter (does NOT cancel) — the full list returns.
	m, cmd := sendCmd(m, keyType(tea.KeyEsc))
	if cmdIsQuit(cmd) {
		t.Fatal("Esc with an active committed filter must clear it, not cancel")
	}
	if m.result.Action == ActionCancel {
		t.Fatal("Esc with an active filter must not set ActionCancel")
	}
	if m.query != "" {
		t.Fatalf("query = %q, want cleared after esc", m.query)
	}
	if got := visibleIDs(m); len(got) != 2 {
		t.Fatalf("visible after esc = %v, want the full list restored", got)
	}

	// A second Esc, now with no filter, cancels the picker.
	m, cmd = sendCmd(m, keyType(tea.KeyEsc))
	if !cmdIsQuit(cmd) || m.Result().Action != ActionCancel {
		t.Fatalf("Esc with no active filter must cancel; action=%v quit=%v", m.Result().Action, cmdIsQuit(cmd))
	}
}

// The footer counter reports the GLOBAL selection even while a filter hides some
// of the selected rows (AUR-530 D5).
func TestModel_SearchFooterCountsStayGlobal(t *testing.T) {
	state := State{Items: []Item{
		{ID: "apple", Title: "x", Conflict: ConflictNone},
		{ID: "apply", Title: "y", Conflict: ConflictNone},
		{ID: "banana", Title: "z", Conflict: ConflictNone},
	}}
	m := NewModel(state)
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m = send(m, keyRune('/'))
	m = typeRunes(m, "app") // hides banana, but it is still selected
	view := m.View()
	if !strings.Contains(view, "3 selected · 0 overwrite · 0 blocked") {
		t.Fatalf("counter must report all 3 selected despite the filter:\n%s", view)
	}
	if !strings.Contains(view, "2 matches") {
		t.Fatalf("search hint should report the 2 visible matches:\n%s", view)
	}
	// The hidden, still-selected banana must not render among the filtered rows.
	if strings.Contains(view, "banana") {
		t.Fatalf("filtered-out row leaked into the search view:\n%s", view)
	}
}

// The search view honors the viewport: with more matches than fit, only a
// window of rows renders and the cursor re-clamps as it moves past the frame.
func TestView_SearchSlicesRowsToViewport(t *testing.T) {
	state := State{Items: []Item{
		{ID: "apex", Title: "1", Conflict: ConflictNone},
		{ID: "apple", Title: "2", Conflict: ConflictNone},
		{ID: "apply", Title: "3", Conflict: ConflictNone},
		{ID: "april", Title: "4", Conflict: ConflictNone},
	}}
	m := NewModel(state)
	m = send(m, tea.WindowSizeMsg{Width: 40, Height: 4}) // header(1)+footer(2) → rpf 1
	m = send(m, keyRune('/'))
	m = typeRunes(m, "ap") // all four match
	if got := len(m.visible); got != 4 {
		t.Fatalf("visible = %d, want 4", got)
	}
	// rowsPerFrame is 1, so the view never exceeds the terminal height.
	lines := strings.Count(m.View(), "\n") + 1
	if want := m.headerLines() + m.rowsPerFrame() + footerLines; lines != want {
		t.Fatalf("search view rendered %d lines, want %d", lines, want)
	}
	// Move the cursor down past the single-row frame; scrollOffset must follow.
	m = send(m, keyType(tea.KeyDown))
	m = send(m, keyType(tea.KeyDown))
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.cursor)
	}
	if m.scrollOffset != 2 {
		t.Fatalf("scrollOffset = %d, want 2 (cursor dragged the frame)", m.scrollOffset)
	}
}
