package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleState() State {
	return State{
		Rows: []Row{
			{Key: 'P', Description: "(read on select)"},
			{Key: '1', PromptID: "alpha", Description: "first"},
			{Key: '2', PromptID: "beta", Description: "second"},
			{Key: 'c', PromptID: "code-review", Title: "Code Review"},
		},
	}
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func cmdIsQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	_, ok := msg.(tea.QuitMsg)
	return ok
}

func TestUpdate_EscOnBoardCancelsAndQuits(t *testing.T) {
	m := NewModel(sampleState(), ModelDeps{})
	next, cmd := m.Update(keyMsg("esc"))
	got := next.(Model)

	if got.result.Action != ActionCancel {
		t.Fatalf("Action = %q, want %q", got.result.Action, ActionCancel)
	}
	if !cmdIsQuit(cmd) {
		t.Fatal("Esc must return tea.Quit")
	}
}

func TestUpdate_CtrlCOnBoardCancelsAndQuits(t *testing.T) {
	m := NewModel(sampleState(), ModelDeps{})
	next, cmd := m.Update(keyMsg("ctrl+c"))
	got := next.(Model)

	if got.result.Action != ActionCancel {
		t.Fatalf("Action = %q, want %q", got.result.Action, ActionCancel)
	}
	if !cmdIsQuit(cmd) {
		t.Fatal("Ctrl+C must return tea.Quit")
	}
}

func TestUpdate_DownArrowAdvancesCursorWithinBounds(t *testing.T) {
	m := NewModel(sampleState(), ModelDeps{})

	for i := 0; i < 10; i++ {
		next, cmd := m.Update(keyMsg("down"))
		m = next.(Model)
		if cmd != nil {
			t.Fatalf("iter %d: expected nil cmd, got %T", i, cmd())
		}
	}
	want := len(sampleState().Rows) - 1
	if m.cursor != want {
		t.Fatalf("cursor = %d, want clamp to %d", m.cursor, want)
	}
}

func TestUpdate_UpArrowClampsAtZero(t *testing.T) {
	m := NewModel(sampleState(), ModelDeps{})
	m.cursor = 2

	for i := 0; i < 5; i++ {
		next, _ := m.Update(keyMsg("up"))
		m = next.(Model)
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
}

func TestUpdate_UnboundLetterIsNoop(t *testing.T) {
	m := NewModel(sampleState(), ModelDeps{})
	before := m
	next, cmd := m.Update(keyMsg("z"))
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("expected nil cmd, got %T", cmd())
	}
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("model mutated on no-op key: got %+v want %+v", got, before)
	}
}

func TestUpdate_WindowSizeMsgStoresDimensions(t *testing.T) {
	m := NewModel(sampleState(), ModelDeps{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	got := next.(Model)

	if got.width != 120 || got.height != 40 {
		t.Fatalf("width/height = %d/%d, want 120/40", got.width, got.height)
	}
}

func TestView_EmptyStoreShowsClipboardHint(t *testing.T) {
	state := State{Rows: []Row{{Key: 'P', Description: "(read on select)"}}}
	m := NewModel(state, ModelDeps{})
	m.width = 80

	out := m.View()
	if !strings.Contains(out, "no prompts found — press P for clipboard or Esc to exit") {
		t.Fatalf("empty-store footer missing. Got:\n%s", out)
	}
}

func TestView_EmptyStoreHintUsesResolvedClipboardKey(t *testing.T) {
	state := State{Rows: []Row{{Key: 'X'}}}
	m := NewModel(state, ModelDeps{})
	m.width = 80

	if !strings.Contains(m.View(), "press X for clipboard") {
		t.Fatalf("hint must render resolved clipboard key. Got:\n%s", m.View())
	}
}

func TestView_EmptyStoreAndClipboardDisabledOmitsClipboardHint(t *testing.T) {
	// With clipboard disabled in config, buildTUIState omits the pinned
	// clipboard row entirely. The footer must not advertise a clipboard
	// shortcut that does not exist.
	m := NewModel(State{Rows: nil}, ModelDeps{})
	m.width = 80

	out := m.View()
	if strings.Contains(out, "for clipboard") {
		t.Fatalf("clipboard-disabled hint must not mention clipboard. Got:\n%s", out)
	}
	if !strings.Contains(out, "no prompts found — press Esc to exit") {
		t.Fatalf("clipboard-disabled hint missing. Got:\n%s", out)
	}
}

func TestView_NonEmptyShowsBoardFooter(t *testing.T) {
	m := NewModel(sampleState(), ModelDeps{})
	m.width = 80

	out := m.View()
	if !strings.Contains(out, "[/ search]") || !strings.Contains(out, "[Esc cancel]") {
		t.Fatalf("board footer missing. Got:\n%s", out)
	}
}

func TestView_RowRendersLowercaseLetterKey(t *testing.T) {
	m := NewModel(sampleState(), ModelDeps{})
	m.width = 80

	out := m.View()
	// sample row has Key: 'c' already; to cover uppercase → lowercase:
	state := sampleState()
	state.Rows = append(state.Rows, Row{Key: 'Q', PromptID: "quit-things"})
	m2 := NewModel(state, ModelDeps{})
	m2.width = 80
	out2 := m2.View()

	if !strings.Contains(out, "[c]") {
		t.Fatalf("expected [c] in output, got:\n%s", out)
	}
	if !strings.Contains(out2, "[q]") {
		t.Fatalf("uppercase Q must render as [q], got:\n%s", out2)
	}
}

func TestDisplayKey(t *testing.T) {
	cases := []struct {
		name string
		in   rune
		want string
	}{
		{"uppercase letter lowercased", 'P', "p"},
		{"lowercase letter unchanged", 'a', "a"},
		{"digit unchanged", '1', "1"},
		{"symbol unchanged", '/', "/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := displayKey(tc.in); got != tc.want {
				t.Fatalf("displayKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTruncateToWidth(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{"fits as-is", "abc", 10, "abc"},
		{"zero width blank", "abc", 0, ""},
		{"negative width blank", "abc", -1, ""},
		{"truncates with ellipsis", "abcdefghij", 5, "abcd…"},
		{"single ellipsis at width 1", "abcdef", 1, "…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncateToWidth(tc.in, tc.width); got != tc.want {
				t.Fatalf("truncateToWidth(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
			}
		})
	}
}
