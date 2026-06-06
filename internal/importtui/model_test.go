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
	const termHeight = 5 // header(1) + footer(1) → rowsPerFrame 3, overflow worst case
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
