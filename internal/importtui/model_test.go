package importtui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func sampleState() State {
	return State{Items: []Item{
		{ID: "code-review", Title: "code review"},
		{ID: "organize-thoughts", Title: "organize thoughts prompt"},
		{ID: "fire", Title: "🔥🔥🔥"},
	}}
}

func send(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func keyRune(r rune) tea.KeyMsg        { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestModel_PreChecksAllItems(t *testing.T) {
	m := send(NewModel(sampleState()), keyType(tea.KeyEnter))
	got := m.Result()
	if got.Action != ActionConfirm {
		t.Fatalf("action = %v, want ActionConfirm", got.Action)
	}
	want := []string{"code-review", "organize-thoughts", "fire"}
	if strings.Join(got.SelectedIDs, ",") != strings.Join(want, ",") {
		t.Errorf("SelectedIDs = %v, want %v (all pre-checked, in item order)", got.SelectedIDs, want)
	}
}

func TestModel_SpaceTogglesCurrentRow(t *testing.T) {
	m := NewModel(sampleState())
	// Deselect the first row (cursor starts at 0), then confirm.
	m = send(m, keyType(tea.KeySpace))
	m = send(m, keyType(tea.KeyEnter))
	got := m.Result().SelectedIDs
	want := []string{"organize-thoughts", "fire"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("SelectedIDs = %v, want %v (first row deselected)", got, want)
	}
}

func TestModel_SpaceAsRuneAlsoToggles(t *testing.T) {
	// Bubble Tea may deliver a bare space as KeyRunes{' '} rather than KeySpace.
	m := NewModel(sampleState())
	m = send(m, keyRune(' '))
	m = send(m, keyType(tea.KeyEnter))
	if got := m.Result().SelectedIDs; strings.Join(got, ",") != "organize-thoughts,fire" {
		t.Errorf("SelectedIDs = %v, want first row deselected via rune-space", got)
	}
}

func TestModel_SelectAllReChecksEverything(t *testing.T) {
	m := NewModel(sampleState())
	// Deselect two rows, then 'a' re-selects all.
	m = send(m, keyType(tea.KeySpace)) // deselect row 0
	m = send(m, keyType(tea.KeyDown))  // cursor → row 1
	m = send(m, keyType(tea.KeySpace)) // deselect row 1
	m = send(m, keyRune('a'))          // select all
	m = send(m, keyType(tea.KeyEnter))
	if got := m.Result().SelectedIDs; len(got) != 3 {
		t.Errorf("SelectedIDs = %v, want all 3 after select-all", got)
	}
}

func TestModel_EscAndCtrlCCancel(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.KeyMsg
	}{
		{"esc", keyType(tea.KeyEsc)},
		{"ctrl+c", keyType(tea.KeyCtrlC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := send(NewModel(sampleState()), tc.msg)
			got := m.Result()
			if got.Action != ActionCancel {
				t.Errorf("action = %v, want ActionCancel", got.Action)
			}
			if got.SelectedIDs != nil {
				t.Errorf("SelectedIDs = %v, want nil on cancel", got.SelectedIDs)
			}
		})
	}
}

func TestModel_CursorStaysInBounds(t *testing.T) {
	m := NewModel(sampleState())
	// Up at the top is a no-op.
	m = send(m, keyType(tea.KeyUp))
	if m.cursor != 0 {
		t.Errorf("cursor = %d after Up at top, want 0", m.cursor)
	}
	// Down past the end clamps at the last row.
	for range 10 {
		m = send(m, keyType(tea.KeyDown))
	}
	if m.cursor != len(m.items)-1 {
		t.Errorf("cursor = %d after many Downs, want %d", m.cursor, len(m.items)-1)
	}
}

// TestView_DoesNotOverflowViewport mirrors the board's banner-viewport
// regression (internal/tui TestView_BannerHeaderDoesNotOverflowViewport): with
// more items than fit, the rendered view must stay within the terminal height
// and fill it exactly as header + rowsPerFrame rows + one footer line. A
// wide-rune title is included so truncation/width math is exercised.
func TestView_DoesNotOverflowViewport(t *testing.T) {
	state := State{Items: []Item{
		{ID: "a", Title: "alpha"},
		{ID: "b", Title: "🔥🔥🔥 wide runes"},
		{ID: "c", Title: "gamma"},
		{ID: "d", Title: "delta"},
	}}
	m := NewModel(state)
	const termHeight = 6 // header(1) + footer(2) → rowsPerFrame 3, overflow worst case
	m = send(m, tea.WindowSizeMsg{Width: 40, Height: termHeight})

	lines := strings.Count(m.View(), "\n") + 1
	if lines > termHeight {
		t.Fatalf("View rendered %d lines, exceeds terminal height %d", lines, termHeight)
	}
	if want := m.headerLines() + m.rowsPerFrame() + footerLines; lines != want {
		t.Fatalf("View rendered %d lines, want %d (header %d + rows %d + footer %d)",
			lines, want, m.headerLines(), m.rowsPerFrame(), footerLines)
	}
}

// TestView_RowsNeverExceedWidth guards the no-wrap invariant: a long id at a
// narrow width is truncated, not wrapped, so every rendered line stays within
// the terminal width and the one-line-per-row viewport math holds.
func TestView_RowsNeverExceedWidth(t *testing.T) {
	state := State{Items: []Item{
		{ID: "a-short-id", Title: "short"},
		{ID: strings.Repeat("very-long-snippet-slug-", 5), Title: "a long title that also overflows"},
	}}
	// Include a pathologically narrow width (< the 7-cell fixed chrome) so the
	// no-wrap invariant is pinned even where the checkbox + gaps don't fit.
	for _, width := range []int{4, 30} {
		m := NewModel(state)
		m = send(m, tea.WindowSizeMsg{Width: width, Height: 10})
		for _, line := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("at width %d: rendered line width %d exceeds it: %q", width, w, line)
			}
		}
	}
}

// TestView_SanitizesTitleControlBytes pins that a verbatim snippet phrase with a
// newline or an ANSI escape cannot spill the row onto extra lines or inject
// terminal control codes: control bytes are neutralized to spaces before render.
func TestView_SanitizesTitleControlBytes(t *testing.T) {
	state := State{Items: []Item{
		{ID: "multi", Title: "first\nsecond"},
		{ID: "ansi", Title: "danger\x1b[31mred\x1b[0m"},
	}}
	m := NewModel(state)
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	view := m.View()

	// All items fit (tall terminal), so the rendered lines are exactly header +
	// one line per item + footer; a title newline must not spill an extra line.
	if got, want := strings.Count(view, "\n")+1, m.headerLines()+len(state.Items)+footerLines; got != want {
		t.Fatalf("View has %d lines, want %d — a title newline spilled a row", got, want)
	}
	// No raw ESC survives into the rendered output.
	if strings.ContainsRune(view, '\x1b') {
		t.Errorf("rendered view contains a raw ESC byte from a snippet phrase:\n%q", view)
	}
}

func TestView_RendersCheckboxes(t *testing.T) {
	m := NewModel(sampleState())
	m = send(m, keyType(tea.KeySpace)) // deselect row 0
	view := m.View()
	if !strings.Contains(view, "[ ]  code-review") {
		t.Errorf("deselected row should render an empty checkbox:\n%s", view)
	}
	if !strings.Contains(view, "[x]  organize-thoughts") {
		t.Errorf("selected row should render a checked checkbox:\n%s", view)
	}
}

// conflictState mixes the three row kinds: a fresh row (cursor starts here), an
// exact-target conflict, and a cross-path duplicate.
func conflictState() State {
	return State{Items: []Item{
		{ID: "fresh", Title: "a fresh snippet", Conflict: ConflictNone},
		{ID: "exists", Title: "already imported", Conflict: ConflictExactTarget},
		{ID: "dup", Title: "duplicate snippet", Conflict: ConflictCrossPath, Blocker: "/prompts/agents/dup.md"},
	}}
}

func TestModel_PreChecksByConflictKind(t *testing.T) {
	m := send(NewModel(conflictState()), keyType(tea.KeyEnter))
	got := m.Result()
	// Only the fresh row is pre-checked; exact-target is skip-by-default and
	// cross-path is non-selectable.
	if strings.Join(got.SelectedIDs, ",") != "fresh" {
		t.Errorf("SelectedIDs = %v, want [fresh] (only fresh pre-checked)", got.SelectedIDs)
	}
	if len(got.OverwriteIDs) != 0 {
		t.Errorf("OverwriteIDs = %v, want none (nothing armed)", got.OverwriteIDs)
	}
}

func TestModel_SpaceArmsExactTargetOverwrite(t *testing.T) {
	m := NewModel(conflictState())
	m = send(m, keyType(tea.KeyDown))  // cursor → exact-target row
	m = send(m, keyType(tea.KeySpace)) // arm overwrite
	m = send(m, keyType(tea.KeyEnter))
	got := m.Result()
	// The armed exact-target is in BOTH the write set and the overwrite set.
	if !contains(got.SelectedIDs, "exists") {
		t.Errorf("SelectedIDs = %v, want it to include armed exact-target 'exists'", got.SelectedIDs)
	}
	if strings.Join(got.OverwriteIDs, ",") != "exists" {
		t.Errorf("OverwriteIDs = %v, want [exists]", got.OverwriteIDs)
	}
}

func TestModel_CrossPathIsNotSelectable(t *testing.T) {
	m := NewModel(conflictState())
	m = send(m, keyType(tea.KeyDown))  // → exact-target
	m = send(m, keyType(tea.KeyDown))  // → cross-path
	m = send(m, keyType(tea.KeySpace)) // no-op on cross-path
	m = send(m, keyRune('a'))          // select-all must skip it too
	m = send(m, keyType(tea.KeyEnter))
	got := m.Result()
	if contains(got.SelectedIDs, "dup") {
		t.Errorf("SelectedIDs = %v, cross-path 'dup' must never be selectable", got.SelectedIDs)
	}
	if contains(got.OverwriteIDs, "dup") {
		t.Errorf("OverwriteIDs = %v, cross-path 'dup' must never be armed", got.OverwriteIDs)
	}
}

func TestModel_SelectAllResetsToSafeDefault(t *testing.T) {
	m := NewModel(conflictState())
	// Arm the exact-target first, then press `a`.
	m = send(m, keyType(tea.KeyDown))  // → exact-target
	m = send(m, keyType(tea.KeySpace)) // arm overwrite
	m = send(m, keyRune('a'))          // select all → safe default
	m = send(m, keyType(tea.KeyEnter))
	got := m.Result()
	// `a` checks fresh rows and RESETS the exact-target arm to skip-by-default, so
	// it never leaves a destructive overwrite armed; cross-path stays unselected.
	if strings.Join(got.SelectedIDs, ",") != "fresh" {
		t.Errorf("SelectedIDs = %v, want [fresh] (a selects fresh only)", got.SelectedIDs)
	}
	if len(got.OverwriteIDs) != 0 {
		t.Errorf("OverwriteIDs = %v, want none (a clears any armed overwrite)", got.OverwriteIDs)
	}
}

func TestView_RendersConflictGlyphs(t *testing.T) {
	m := NewModel(conflictState())
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	view := m.View()
	for _, want := range []string{"[x]  fresh", "[=]  exists", "[!]  dup"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	// The cross-path row shows the conflicting path, not its phrase.
	if !strings.Contains(view, "also at /prompts/agents/dup.md") {
		t.Errorf("cross-path row should show the conflicting path:\n%s", view)
	}
	// Arming the exact-target flips its glyph to a checked box.
	m = send(m, keyType(tea.KeyDown))  // → exact-target
	m = send(m, keyType(tea.KeySpace)) // arm
	if armed := m.View(); !strings.Contains(armed, "[x]  exists") {
		t.Errorf("armed exact-target should render [x]:\n%s", armed)
	}
}

func TestModel_SelectAllPreservesCLIAuthorizedRefresh(t *testing.T) {
	// A CLI --overwrite refresh (Armed) must survive `a`: select-all clears ad-hoc
	// per-row arms but keeps overwrites the --overwrite flag authorized.
	state := State{Items: []Item{
		{ID: "fresh", Title: "new", Conflict: ConflictNone},
		{ID: "refresh", Title: "existing", Conflict: ConflictExactTarget, Armed: true},
		{ID: "manual", Title: "also existing", Conflict: ConflictExactTarget},
	}}
	m := NewModel(state)
	m = send(m, keyType(tea.KeyDown))  // → refresh
	m = send(m, keyType(tea.KeyDown))  // → manual
	m = send(m, keyType(tea.KeySpace)) // arm the manual exact-target ad-hoc
	m = send(m, keyRune('a'))          // select all → reset to default
	m = send(m, keyType(tea.KeyEnter))
	got := m.Result()
	if strings.Join(got.SelectedIDs, ",") != "fresh,refresh" {
		t.Errorf("SelectedIDs = %v, want [fresh refresh] (CLI refresh kept, ad-hoc arm cleared)", got.SelectedIDs)
	}
	if strings.Join(got.OverwriteIDs, ",") != "refresh" {
		t.Errorf("OverwriteIDs = %v, want [refresh] (CLI-authorized overwrite preserved)", got.OverwriteIDs)
	}
}

func TestModel_ArmedExactTargetStartsChecked(t *testing.T) {
	// A CLI --overwrite refresh arrives as a pre-armed exact-target: it starts
	// checked and is reported as an overwrite, not a fresh create.
	state := State{Items: []Item{
		{ID: "refresh", Title: "existing prompt", Conflict: ConflictExactTarget, Armed: true},
	}}
	m := send(NewModel(state), keyType(tea.KeyEnter))
	got := m.Result()
	if strings.Join(got.SelectedIDs, ",") != "refresh" {
		t.Errorf("SelectedIDs = %v, want [refresh] (pre-armed)", got.SelectedIDs)
	}
	if strings.Join(got.OverwriteIDs, ",") != "refresh" {
		t.Errorf("OverwriteIDs = %v, want [refresh] (counted as an overwrite)", got.OverwriteIDs)
	}
}

func TestView_FooterCounter(t *testing.T) {
	m := NewModel(conflictState())
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	// Default: 1 fresh selected, 0 overwrite, 1 blocked.
	if view := m.View(); !strings.Contains(view, "1 selected · 0 overwrite · 1 blocked") {
		t.Errorf("footer counter wrong at defaults:\n%s", view)
	}
	// Arm the exact-target: now 2 selected, 1 overwrite, 1 blocked, and the
	// confirm line counts both writes.
	m = send(m, keyType(tea.KeyDown))
	m = send(m, keyType(tea.KeySpace))
	view := m.View()
	if !strings.Contains(view, "2 selected · 1 overwrite · 1 blocked") {
		t.Errorf("footer counter wrong after arming:\n%s", view)
	}
	if !strings.Contains(view, "write 2 prompts?") {
		t.Errorf("footer confirm line should count both writes:\n%s", view)
	}
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
