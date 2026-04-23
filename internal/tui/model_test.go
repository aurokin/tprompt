package tui

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/store"
)

func sampleState() State {
	return State{
		Rows: []Row{
			{Key: 'p', Description: "(read on select)"},
			{Key: '1', PromptID: "alpha", Description: "first"},
			{Key: '2', PromptID: "beta", Description: "second"},
			{Key: 'c', PromptID: "code-review", Title: "Code Review"},
		},
		Reserved: ReservedKeys{
			Clipboard: ReservedBinding{Printable: 'p'},
			Search:    ReservedBinding{Printable: '/'},
			Cancel:    ReservedBinding{Symbolic: "Esc"},
			Select:    ReservedBinding{Symbolic: "Enter"},
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
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
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

func TestUpdate_RemappedPrintableCancelOnBoardCancelsAndQuits(t *testing.T) {
	state := sampleState()
	state.Reserved.Cancel = ReservedBinding{Printable: 'x'}
	m := NewModel(state, ModelDeps{})
	next, cmd := m.Update(keyMsg("x"))
	got := next.(Model)

	if got.result.Action != ActionCancel {
		t.Fatalf("Action = %q, want %q", got.result.Action, ActionCancel)
	}
	if !cmdIsQuit(cmd) {
		t.Fatal("remapped cancel must return tea.Quit")
	}
}

func TestUpdate_SymbolicTabCancelOnBoardCancelsAndQuits(t *testing.T) {
	state := sampleState()
	state.Reserved.Cancel = ReservedBinding{Symbolic: "Tab"}
	m := NewModel(state, ModelDeps{})
	next, cmd := m.Update(keyMsg("tab"))
	got := next.(Model)

	if got.result.Action != ActionCancel {
		t.Fatalf("Action = %q, want %q", got.result.Action, ActionCancel)
	}
	if !cmdIsQuit(cmd) {
		t.Fatal("tab cancel must return tea.Quit")
	}
}

func TestUpdate_EscDoesNotCancelWhenCancelIsRemapped(t *testing.T) {
	state := sampleState()
	state.Reserved.Cancel = ReservedBinding{Printable: 'x'}
	m := NewModel(state, ModelDeps{})
	before := m
	next, cmd := m.Update(keyMsg("esc"))
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("expected nil cmd, got %T", cmd())
	}
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("Esc should be noop when cancel is remapped: got %+v want %+v", got, before)
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
	state := State{
		Rows: []Row{{Key: 'p', Description: "(read on select)"}},
		Reserved: ReservedKeys{
			Clipboard: ReservedBinding{Printable: 'p'},
			Cancel:    ReservedBinding{Symbolic: "Esc"},
		},
	}
	m := NewModel(state, ModelDeps{})
	m.width = 80

	out := m.View()
	if !strings.Contains(out, "no prompts found — press P for clipboard or Esc to exit") {
		t.Fatalf("empty-store footer missing. Got:\n%s", out)
	}
}

func TestView_EmptyStoreHintUsesResolvedClipboardKey(t *testing.T) {
	state := State{
		Rows: []Row{{Key: 'x'}},
		Reserved: ReservedKeys{
			Clipboard: ReservedBinding{Printable: 'x'},
			Cancel:    ReservedBinding{Symbolic: "Esc"},
		},
	}
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
	m := NewModel(State{
		Rows: nil,
		Reserved: ReservedKeys{
			Clipboard: ReservedBinding{Disabled: true},
			Cancel:    ReservedBinding{Symbolic: "Esc"},
		},
	}, ModelDeps{})
	m.width = 80

	out := m.View()
	if strings.Contains(out, "for clipboard") {
		t.Fatalf("clipboard-disabled hint must not mention clipboard. Got:\n%s", out)
	}
	if !strings.Contains(out, "no prompts found — press Esc to exit") {
		t.Fatalf("clipboard-disabled hint missing. Got:\n%s", out)
	}
}

func TestView_EmptyStoreUsesResolvedCancelKey(t *testing.T) {
	m := NewModel(State{
		Rows: []Row{{Key: 'x', Description: "(read on select)"}},
		Reserved: ReservedKeys{
			Clipboard: ReservedBinding{Printable: 'x'},
			Cancel:    ReservedBinding{Symbolic: "Tab"},
		},
	}, ModelDeps{})
	m.width = 80

	out := m.View()
	if !strings.Contains(out, "press X for clipboard or Tab to exit") {
		t.Fatalf("empty-store footer must use resolved cancel key. Got:\n%s", out)
	}
}

func TestView_EmptyStoreOmitsDisabledCancelHint(t *testing.T) {
	m := NewModel(State{
		Rows: []Row{{Key: 'x', Description: "(read on select)"}},
		Reserved: ReservedKeys{
			Clipboard: ReservedBinding{Printable: 'x'},
			Cancel:    ReservedBinding{Disabled: true},
		},
	}, ModelDeps{})
	m.width = 80

	out := m.View()
	if strings.Contains(out, "to exit") {
		t.Fatalf("disabled cancel must not be advertised. Got:\n%s", out)
	}
	if !strings.Contains(out, "press X for clipboard") {
		t.Fatalf("clipboard hint missing. Got:\n%s", out)
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

func TestView_NonEmptyFooterUsesResolvedReservedKeys(t *testing.T) {
	state := sampleState()
	state.Reserved.Search = ReservedBinding{Symbolic: "Tab"}
	state.Reserved.Cancel = ReservedBinding{Printable: 'x'}
	m := NewModel(state, ModelDeps{})
	m.width = 80

	out := m.View()
	if !strings.Contains(out, "[Tab search]") || !strings.Contains(out, "[X cancel]") {
		t.Fatalf("board footer must use resolved reserved keys. Got:\n%s", out)
	}
}

func TestView_NonEmptyFooterOmitsDisabledHints(t *testing.T) {
	state := sampleState()
	state.Reserved.Search = ReservedBinding{Disabled: true}
	state.Reserved.Cancel = ReservedBinding{Disabled: true}
	m := NewModel(state, ModelDeps{})
	m.width = 80

	out := m.View()
	if strings.Contains(out, "search]") || strings.Contains(out, "cancel]") {
		t.Fatalf("disabled reserved hints must be omitted. Got:\n%s", out)
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

func TestRenderer_RunUsesInjectedInput(t *testing.T) {
	r := NewRenderer(ModelDeps{}, ProgramIO{
		Input:  strings.NewReader("\x1b"),
		Output: io.Discard,
	})

	got, err := r.Run(sampleState())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if got.Action != ActionCancel {
		t.Fatalf("Action = %q, want %q", got.Action, ActionCancel)
	}
}

func TestRenderer_RunUsesInjectedOutput(t *testing.T) {
	var out bytes.Buffer
	r := NewRenderer(ModelDeps{}, ProgramIO{
		Input:  strings.NewReader("\x03"),
		Output: &out,
	})

	if _, err := r.Run(sampleState()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("expected Bubble Tea to write to the injected output stream")
	}
}

// --- AUR-24: prompt selection + oversize inline error --- //

type fakeSubmitter struct {
	calls []Result
	err   error
}

func (f *fakeSubmitter) Submit(r Result) error {
	f.calls = append(f.calls, r)
	return f.err
}

type fakeStore struct {
	bodies map[string]string
	err    error
}

func (f *fakeStore) Discover() error                { return nil }
func (f *fakeStore) List() ([]store.Summary, error) { return nil, nil }
func (f *fakeStore) Resolve(id string) (store.Prompt, error) {
	if f.err != nil {
		return store.Prompt{}, f.err
	}
	body, ok := f.bodies[id]
	if !ok {
		return store.Prompt{}, &store.NotFoundError{ID: id}
	}
	return store.Prompt{Summary: store.Summary{ID: id}, Body: body}, nil
}

// runCmd drives the returned tea.Cmd to completion and returns the emitted msg.
// Test-only shortcut for assertions against submitResultMsg.
func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func promptDeps(bodies map[string]string, storeErr error, subErr error) (ModelDeps, *fakeSubmitter, *fakeStore) {
	sub := &fakeSubmitter{err: subErr}
	st := &fakeStore{bodies: bodies, err: storeErr}
	return ModelDeps{
		Submitter:     sub,
		Store:         st,
		MaxPasteBytes: 1 << 20,
	}, sub, st
}

func TestUpdate_BoundKeySubmitsPromptAndQuits(t *testing.T) {
	deps, sub, _ := promptDeps(map[string]string{"code-review": "body"}, nil, nil)
	m := NewModel(sampleState(), deps)

	next, cmd := m.Update(keyMsg("c"))
	got := next.(Model)

	// At this point we've fired the submit cmd but not yet received the
	// submitResultMsg — result is still zero, inlineError cleared.
	if got.inlineError != "" {
		t.Fatalf("inlineError should be empty, got %q", got.inlineError)
	}
	if cmd == nil {
		t.Fatal("expected submit cmd, got nil")
	}

	// Running the cmd should produce a submitResultMsg carrying ActionPrompt.
	msg := runCmd(cmd)
	sr, ok := msg.(submitResultMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want submitResultMsg", msg)
	}
	if sr.err != nil {
		t.Fatalf("submitResultMsg.err = %v, want nil", sr.err)
	}
	if sr.result.Action != ActionPrompt || sr.result.PromptID != "code-review" {
		t.Fatalf("submitResultMsg.result = %+v, want ActionPrompt/code-review", sr.result)
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submitter.Submit called %d times, want 1", len(sub.calls))
	}

	// Feeding the msg back into Update sets Result and returns tea.Quit.
	next2, cmd2 := got.Update(sr)
	final := next2.(Model)
	if final.result.Action != ActionPrompt || final.result.PromptID != "code-review" {
		t.Fatalf("final result = %+v, want ActionPrompt/code-review", final.result)
	}
	if final.submitErr != nil {
		t.Fatalf("submitErr = %v, want nil", final.submitErr)
	}
	if !cmdIsQuit(cmd2) {
		t.Fatal("submitResultMsg must return tea.Quit")
	}
}

func TestUpdate_BoundKeyUppercaseAlsoSubmits(t *testing.T) {
	deps, sub, _ := promptDeps(map[string]string{"code-review": "body"}, nil, nil)
	m := NewModel(sampleState(), deps)

	_, cmd := m.Update(keyMsg("C")) // uppercase should fold to 'c'
	if cmd == nil {
		t.Fatal("uppercase bound key should fire submit cmd")
	}
	if msg := runCmd(cmd); msg == nil {
		t.Fatal("cmd should emit a submitResultMsg")
	}
	if len(sub.calls) != 1 || sub.calls[0].PromptID != "code-review" {
		t.Fatalf("Submitter.Submit calls = %+v, want one call for code-review", sub.calls)
	}
}

func TestUpdate_UnassignedKeyPreservesInlineError(t *testing.T) {
	deps, sub, _ := promptDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.inlineError = "prior error"

	next, cmd := m.Update(keyMsg("z")) // not assigned to any row
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("expected nil cmd on unassigned key, got %T", cmd())
	}
	if got.inlineError != "prior error" {
		t.Fatalf("inlineError = %q, want %q", got.inlineError, "prior error")
	}
	if len(sub.calls) != 0 {
		t.Fatalf("Submitter.Submit must not be called on unassigned key, got %d calls", len(sub.calls))
	}
}

func TestUpdate_OversizePromptSetsInlineErrorAndStaysOpen(t *testing.T) {
	deps, sub, _ := promptDeps(map[string]string{"code-review": "xxxxxxxxxx"}, nil, nil)
	deps.MaxPasteBytes = 4 // body length 10 > 4 triggers oversize
	m := NewModel(sampleState(), deps)

	next, cmd := m.Update(keyMsg("c"))
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("oversize path must not fire a cmd, got %T", cmd())
	}
	if got.inlineError == "" {
		t.Fatal("oversize path must set inlineError")
	}
	if !strings.Contains(got.inlineError, "max_paste_bytes") {
		t.Fatalf("inlineError should mention max_paste_bytes, got %q", got.inlineError)
	}
	if got.result.Action != "" {
		t.Fatalf("result should not be set on oversize path, got %+v", got.result)
	}
	if len(sub.calls) != 0 {
		t.Fatalf("Submitter.Submit must not be called on oversize path, got %d calls", len(sub.calls))
	}
}

func TestUpdate_NavKeysPreserveInlineError(t *testing.T) {
	deps, _, _ := promptDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.inlineError = "stale error"

	for _, k := range []string{"down", "up"} {
		next, _ := m.Update(keyMsg(k))
		got := next.(Model)
		if got.inlineError != "stale error" {
			t.Fatalf("after %s: inlineError = %q, want stale error preserved", k, got.inlineError)
		}
		m = got
	}
}

func TestUpdate_SearchKeyClearsInlineError(t *testing.T) {
	deps, _, _ := promptDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.inlineError = "some error"

	next, _ := m.Update(keyMsg("/"))
	got := next.(Model)
	if got.inlineError != "" {
		t.Fatalf("search key must clear inlineError, got %q", got.inlineError)
	}
}

func TestUpdate_ClipboardKeyClearsInlineError(t *testing.T) {
	deps, _, _ := promptDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.inlineError = "some error"

	next, _ := m.Update(keyMsg("p"))
	got := next.(Model)
	if got.inlineError != "" {
		t.Fatalf("clipboard key must clear inlineError, got %q", got.inlineError)
	}
}

func TestUpdate_CancelClearsInlineErrorAndQuits(t *testing.T) {
	deps, _, _ := promptDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.inlineError = "some error"

	next, cmd := m.Update(keyMsg("esc"))
	got := next.(Model)
	if got.inlineError != "" {
		t.Fatalf("cancel must clear inlineError, got %q", got.inlineError)
	}
	if got.result.Action != ActionCancel {
		t.Fatalf("Action = %q, want ActionCancel", got.result.Action)
	}
	if !cmdIsQuit(cmd) {
		t.Fatal("Esc must return tea.Quit")
	}
}

func TestUpdate_ValidSelectClearsExistingInlineError(t *testing.T) {
	deps, sub, _ := promptDeps(map[string]string{"code-review": "ok"}, nil, nil)
	m := NewModel(sampleState(), deps)
	m.inlineError = "prior oversize error"

	next, cmd := m.Update(keyMsg("c"))
	got := next.(Model)
	if got.inlineError != "" {
		t.Fatalf("valid select must clear inlineError, got %q", got.inlineError)
	}
	if cmd == nil {
		t.Fatal("valid select must fire submit cmd")
	}
	if msg := runCmd(cmd); msg == nil {
		t.Fatal("submit cmd should emit a msg")
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submitter.Submit calls = %d, want 1", len(sub.calls))
	}
}

func TestUpdate_SubmitResultMsgSetsResultAndErrAndQuits(t *testing.T) {
	deps, _, _ := promptDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)

	boom := errors.New("daemon down")
	next, cmd := m.Update(submitResultMsg{
		result: Result{Action: ActionPrompt, PromptID: "code-review"},
		err:    boom,
	})
	got := next.(Model)

	if got.result.Action != ActionPrompt || got.result.PromptID != "code-review" {
		t.Fatalf("result = %+v, want ActionPrompt/code-review", got.result)
	}
	if !errors.Is(got.submitErr, boom) {
		t.Fatalf("submitErr = %v, want %v", got.submitErr, boom)
	}
	if !cmdIsQuit(cmd) {
		t.Fatal("submitResultMsg must return tea.Quit")
	}
}

func TestUpdate_StoreResolveErrorBubblesViaSubmitErr(t *testing.T) {
	notFound := &store.NotFoundError{ID: "code-review"}
	deps, sub, _ := promptDeps(nil, notFound, nil)
	m := NewModel(sampleState(), deps)

	next, cmd := m.Update(keyMsg("c"))
	got := next.(Model)

	if !errors.Is(got.submitErr, notFound) {
		t.Fatalf("submitErr = %v, want %v", got.submitErr, notFound)
	}
	if got.result.Action != ActionPrompt || got.result.PromptID != "code-review" {
		t.Fatalf("result = %+v, want ActionPrompt/code-review", got.result)
	}
	if !cmdIsQuit(cmd) {
		t.Fatal("Store.Resolve failure must return tea.Quit")
	}
	if len(sub.calls) != 0 {
		t.Fatalf("Submitter.Submit must not be called when Resolve fails, got %d calls", len(sub.calls))
	}
}

func TestView_InlineErrorPrependedToFooter(t *testing.T) {
	deps, _, _ := promptDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.width = 80
	m.inlineError = "body too large"

	out := m.View()
	if !strings.Contains(out, "body too large") {
		t.Fatalf("view should include inline error. Got:\n%s", out)
	}
	// Assert ordering: error text precedes the mode-hint label in the footer.
	errIdx := strings.Index(out, "body too large")
	hintIdx := strings.Index(out, "[Esc cancel]")
	if errIdx < 0 || hintIdx < 0 || errIdx >= hintIdx {
		t.Fatalf("inline error must be prepended to hints (err=%d, hint=%d). Got:\n%s", errIdx, hintIdx, out)
	}
}

func TestUpdate_PromptKeyIgnoredWhileSubmitPending(t *testing.T) {
	// Codex P1: a slow Submitter plus key repeat must not enqueue duplicate
	// submit commands. After the first prompt-key press, further prompt
	// keypresses are no-ops until submitResultMsg clears pendingSubmit.
	deps, sub, _ := promptDeps(map[string]string{"code-review": "ok"}, nil, nil)
	m := NewModel(sampleState(), deps)

	next, cmd := m.Update(keyMsg("c"))
	got := next.(Model)
	if cmd == nil {
		t.Fatal("first press should fire submit cmd")
	}
	if !got.pendingSubmit {
		t.Fatal("first press must set pendingSubmit")
	}

	next2, cmd2 := got.Update(keyMsg("c"))
	got2 := next2.(Model)
	if cmd2 != nil {
		t.Fatalf("second press while pending must be a no-op, got cmd %T", cmd2())
	}
	if !got2.pendingSubmit {
		t.Fatal("pendingSubmit must remain true after gated keypress")
	}

	// The first cmd is still the only submit — running it confirms exactly
	// one Submitter.Submit call.
	if msg := runCmd(cmd); msg == nil {
		t.Fatal("first cmd should emit a submitResultMsg")
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submitter.Submit calls = %d, want 1", len(sub.calls))
	}
}

func TestUpdate_CancelIgnoredWhileSubmitPending(t *testing.T) {
	// Codex P1: after a prompt select, the submit is already in flight against
	// the daemon. Esc/Ctrl+C before submitResultMsg arrives must not exit 0 —
	// that would drop the submit outcome and hide failures behind a fake
	// cancel success.
	deps, _, _ := promptDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.pendingSubmit = true

	for _, k := range []string{"esc", "ctrl+c"} {
		next, cmd := m.Update(keyMsg(k))
		got := next.(Model)
		if cmd != nil {
			t.Fatalf("%s while pending must not return a cmd, got %T", k, cmd())
		}
		if got.result.Action == ActionCancel {
			t.Fatalf("%s while pending must not set ActionCancel", k)
		}
		if !got.pendingSubmit {
			t.Fatalf("%s while pending must preserve pendingSubmit", k)
		}
	}
}

func TestUpdate_SubmitResultMsgClearsPendingSubmit(t *testing.T) {
	deps, _, _ := promptDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.pendingSubmit = true

	next, _ := m.Update(submitResultMsg{
		result: Result{Action: ActionPrompt, PromptID: "code-review"},
		err:    nil,
	})
	got := next.(Model)
	if got.pendingSubmit {
		t.Fatal("submitResultMsg must clear pendingSubmit")
	}
}

func TestRenderer_RunSurfacesSubmitErrFromFinalModel(t *testing.T) {
	boom := errors.New("daemon down")
	sub := &fakeSubmitter{err: boom}
	st := &fakeStore{bodies: map[string]string{"code-review": "ok"}}
	deps := ModelDeps{
		Submitter:     sub,
		Store:         st,
		MaxPasteBytes: 1 << 20,
	}
	// "c" selects the code-review row; the Model invokes fakeSubmitter.Submit
	// which returns boom. The Model then transitions to tea.Quit with
	// submitErr set, which Renderer.Run must surface.
	r := NewRenderer(deps, ProgramIO{
		Input:  strings.NewReader("c"),
		Output: io.Discard,
	})

	result, err := r.Run(sampleState())
	if !errors.Is(err, boom) {
		t.Fatalf("Run() err = %v, want %v", err, boom)
	}
	if result.Action != ActionPrompt || result.PromptID != "code-review" {
		t.Fatalf("Run() result = %+v, want ActionPrompt/code-review", result)
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submitter.Submit calls = %d, want 1", len(sub.calls))
	}
}

// --- AUR-25: clipboard P flow via tea.Cmd --- //

type fakeClip struct {
	body  []byte
	err   error
	calls int
}

func (f *fakeClip) Read() ([]byte, error) {
	f.calls++
	return f.body, f.err
}

func clipboardDeps(body []byte, readErr, subErr error) (ModelDeps, *fakeSubmitter, *fakeClip) {
	sub := &fakeSubmitter{err: subErr}
	clip := &fakeClip{body: body, err: readErr}
	return ModelDeps{
		Submitter:     sub,
		Clip:          clip,
		MaxPasteBytes: 1 << 20,
	}, sub, clip
}

func TestUpdate_ClipboardKeySetsPendingAndFiresReadCmd(t *testing.T) {
	deps, _, clip := clipboardDeps([]byte("ok"), nil, nil)
	m := NewModel(sampleState(), deps)

	next, cmd := m.Update(keyMsg("p"))
	got := next.(Model)

	if !got.pendingClipboard {
		t.Fatal("clipboard key must set pendingClipboard")
	}
	if cmd == nil {
		t.Fatal("clipboard key must return a read cmd")
	}
	msg := runCmd(cmd)
	rm, ok := msg.(clipboardReadMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want clipboardReadMsg", msg)
	}
	if rm.err != nil {
		t.Fatalf("clipboardReadMsg.err = %v, want nil", rm.err)
	}
	if string(rm.body) != "ok" {
		t.Fatalf("clipboardReadMsg.body = %q, want %q", rm.body, "ok")
	}
	if clip.calls != 1 {
		t.Fatalf("clip.Read calls = %d, want 1", clip.calls)
	}
}

func TestUpdate_ClipboardKeyCaseInsensitive(t *testing.T) {
	// sampleState binds clipboard to 'p'; pressing 'P' must also trigger.
	deps, _, clip := clipboardDeps([]byte("ok"), nil, nil)
	m := NewModel(sampleState(), deps)

	_, cmd := m.Update(keyMsg("P"))
	if cmd == nil {
		t.Fatal("uppercase clipboard key must return a read cmd")
	}
	if msg := runCmd(cmd); msg == nil {
		t.Fatal("cmd should emit a clipboardReadMsg")
	}
	if clip.calls != 1 {
		t.Fatalf("clip.Read calls = %d, want 1", clip.calls)
	}
}

func TestUpdate_ClipboardReadMsgSuccessFiresSubmitCmd(t *testing.T) {
	deps, sub, _ := clipboardDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.pendingClipboard = true // simulate post-keypress state

	next, cmd := m.Update(clipboardReadMsg{body: []byte("ok")})
	got := next.(Model)

	if got.pendingClipboard {
		t.Fatal("pendingClipboard must be cleared after read msg")
	}
	if !got.pendingSubmit {
		t.Fatal("successful read must set pendingSubmit before submit completes")
	}
	if got.inlineError != "" {
		t.Fatalf("inlineError = %q, want empty on success", got.inlineError)
	}
	if cmd == nil {
		t.Fatal("successful read must fire submit cmd")
	}
	msg := runCmd(cmd)
	sr, ok := msg.(submitResultMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want submitResultMsg", msg)
	}
	if sr.result.Action != ActionClipboard || string(sr.result.ClipboardBody) != "ok" {
		t.Fatalf("submit result = %+v, want ActionClipboard/ok", sr.result)
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submitter.Submit calls = %d, want 1", len(sub.calls))
	}
}

func TestUpdate_ClipboardReadMsgEmptyError_SetsInlineErrorAndStaysOpen(t *testing.T) {
	deps, sub, _ := clipboardDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.pendingClipboard = true

	next, cmd := m.Update(clipboardReadMsg{err: &clipboard.EmptyClipboardError{}})
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("empty-clipboard error must not fire a cmd, got %T", cmd())
	}
	if got.pendingClipboard {
		t.Fatal("pendingClipboard must be cleared on error")
	}
	if got.pendingSubmit {
		t.Fatal("pendingSubmit must not be set on error")
	}
	want := "clipboard is empty — choose another option"
	if got.inlineError != want {
		t.Fatalf("inlineError = %q, want %q", got.inlineError, want)
	}
	if len(sub.calls) != 0 {
		t.Fatalf("Submitter.Submit must not be called on error, got %d calls", len(sub.calls))
	}
}

func TestUpdate_ClipboardReadMsgInvalidUTF8_SetsInlineError(t *testing.T) {
	deps, _, _ := clipboardDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.pendingClipboard = true

	next, cmd := m.Update(clipboardReadMsg{err: &clipboard.InvalidUTF8Error{}})
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("utf-8 error must not fire a cmd, got %T", cmd())
	}
	if !strings.Contains(got.inlineError, "non-UTF-8") {
		t.Fatalf("inlineError = %q, want mention of non-UTF-8", got.inlineError)
	}
	if !strings.Contains(got.inlineError, "choose another option") {
		t.Fatalf("inlineError = %q, want — choose another option suffix", got.inlineError)
	}
}

func TestUpdate_ClipboardReadMsgOversize_SetsInlineError(t *testing.T) {
	deps, _, _ := clipboardDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)
	m.pendingClipboard = true

	next, cmd := m.Update(clipboardReadMsg{err: &clipboard.OversizeError{Bytes: 42, Limit: 10}})
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("oversize error must not fire a cmd, got %T", cmd())
	}
	if !strings.Contains(got.inlineError, "max_paste_bytes") {
		t.Fatalf("inlineError = %q, want mention of max_paste_bytes", got.inlineError)
	}
	if !strings.Contains(got.inlineError, "42") || !strings.Contains(got.inlineError, "10") {
		t.Fatalf("inlineError = %q, want Bytes/Limit figures", got.inlineError)
	}
}

func TestUpdate_ClipboardKeyRetryClearsPriorError(t *testing.T) {
	deps, _, clip := clipboardDeps([]byte("ok"), nil, nil)
	m := NewModel(sampleState(), deps)
	m.inlineError = "clipboard is empty — choose another option"

	next, cmd := m.Update(keyMsg("p"))
	got := next.(Model)

	if got.inlineError != "" {
		t.Fatalf("retry must clear inlineError, got %q", got.inlineError)
	}
	if !got.pendingClipboard {
		t.Fatal("retry must set pendingClipboard")
	}
	if cmd == nil {
		t.Fatal("retry must fire a fresh read cmd")
	}
	if msg := runCmd(cmd); msg == nil {
		t.Fatal("fresh read cmd should emit a clipboardReadMsg")
	}
	if clip.calls != 1 {
		t.Fatalf("clip.Read calls = %d, want 1 fresh read", clip.calls)
	}
}

func TestUpdate_ClipboardKeyIgnoredWhilePendingClipboard(t *testing.T) {
	// Key repeat during the ~20ms read window must not enqueue duplicate read
	// commands or reset state mid-flight.
	deps, _, clip := clipboardDeps([]byte("ok"), nil, nil)
	m := NewModel(sampleState(), deps)
	m.pendingClipboard = true

	next, cmd := m.Update(keyMsg("p"))
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("clipboard key while pending must not return a cmd, got %T", cmd())
	}
	if !got.pendingClipboard {
		t.Fatal("pendingClipboard must remain true")
	}
	if clip.calls != 0 {
		t.Fatalf("clip.Read must not be invoked while pending, got %d calls", clip.calls)
	}
}

func TestUpdate_ClipboardKeyIgnoredWhilePendingSubmit(t *testing.T) {
	// A prompt or clipboard submit already dialing the daemon must not be
	// preempted by a clipboard retry — the submit outcome is still pending.
	deps, _, clip := clipboardDeps([]byte("ok"), nil, nil)
	m := NewModel(sampleState(), deps)
	m.pendingSubmit = true

	next, cmd := m.Update(keyMsg("p"))
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("clipboard key while submit pending must not return a cmd, got %T", cmd())
	}
	if got.pendingClipboard {
		t.Fatal("clipboard key while submit pending must not set pendingClipboard")
	}
	if clip.calls != 0 {
		t.Fatalf("clip.Read must not be invoked while submit pending, got %d calls", clip.calls)
	}
}

func TestUpdate_PromptKeyIgnoredWhilePendingClipboard(t *testing.T) {
	// A prompt keypress arriving between clipboard-key press and
	// clipboardReadMsg would race the submit that follows a successful read.
	deps, sub, _ := promptDeps(map[string]string{"code-review": "body"}, nil, nil)
	m := NewModel(sampleState(), deps)
	m.pendingClipboard = true

	next, cmd := m.Update(keyMsg("c"))
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("prompt key while clipboard pending must not return a cmd, got %T", cmd())
	}
	if !got.pendingClipboard {
		t.Fatal("pendingClipboard must remain true through gated prompt keypress")
	}
	if len(sub.calls) != 0 {
		t.Fatalf("Submitter.Submit must not be called while clipboard pending, got %d", len(sub.calls))
	}
}

func TestUpdate_NavKeysPreserveInlineErrorAfterClipboardError(t *testing.T) {
	// §19: ↑/↓ preserve inlineError across clipboard validation failures.
	deps, _, _ := clipboardDeps(nil, nil, nil)
	m := NewModel(sampleState(), deps)

	next, _ := m.Update(clipboardReadMsg{err: &clipboard.EmptyClipboardError{}})
	m = next.(Model)
	before := m.inlineError
	if before == "" {
		t.Fatal("precondition: inlineError should be set after empty-clipboard error")
	}

	for _, k := range []string{"down", "up"} {
		next, _ := m.Update(keyMsg(k))
		m = next.(Model)
		if m.inlineError != before {
			t.Fatalf("after %s: inlineError = %q, want preserved %q", k, m.inlineError, before)
		}
	}
}

func TestReadClipboardCmd_NilReaderEmitsInlineError(t *testing.T) {
	// Zero-value ModelDeps or misconfigured wiring should degrade to an
	// inline footer error — never a nil-pointer panic.
	cmd := readClipboardCmd(nil, 1<<20)
	msg := cmd()

	rm, ok := msg.(clipboardReadMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want clipboardReadMsg", msg)
	}
	if rm.err == nil {
		t.Fatal("nil Reader must surface an error")
	}
	if rm.body != nil {
		t.Fatalf("body = %q, want nil on error", rm.body)
	}
	// Error text must flow cleanly through clipboardErrorText as a user-facing
	// footer message (not stack-trace noise).
	got := clipboardErrorText(rm.err)
	if !strings.Contains(got, "clipboard is unavailable") {
		t.Fatalf("clipboardErrorText = %q, want footer-style 'clipboard is unavailable' message", got)
	}
}

func TestRenderer_RunSubmitsClipboardAndSurfacesErr(t *testing.T) {
	boom := errors.New("daemon down")
	sub := &fakeSubmitter{err: boom}
	clip := &fakeClip{body: []byte("ok")}
	deps := ModelDeps{
		Submitter:     sub,
		Clip:          clip,
		MaxPasteBytes: 1 << 20,
	}
	// "p" triggers the clipboard read; readClipboardCmd emits clipboardReadMsg,
	// the Model fires submitCmd against the fake Submitter which returns boom,
	// submitResultMsg captures the err and Quits, Run surfaces it.
	r := NewRenderer(deps, ProgramIO{
		Input:  strings.NewReader("p"),
		Output: io.Discard,
	})

	result, err := r.Run(sampleState())
	if !errors.Is(err, boom) {
		t.Fatalf("Run() err = %v, want %v", err, boom)
	}
	if result.Action != ActionClipboard || string(result.ClipboardBody) != "ok" {
		t.Fatalf("Run() result = %+v, want ActionClipboard/ok", result)
	}
	if clip.calls != 1 {
		t.Fatalf("clip.Read calls = %d, want 1", clip.calls)
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submitter.Submit calls = %d, want 1", len(sub.calls))
	}
}

// --- AUR-27: viewport scrolling --- //

// sampleState has 4 rows; height=3 subtracts 1 footer line for rowsPerFrame=2
// so only 2 rows fit. Shared by the scroll tests below.
func scrollableModel() Model {
	m := NewModel(sampleState(), ModelDeps{})
	m.width = 80
	m.height = 3
	return m
}

func TestUpdate_DownArrowScrollsOffsetPastBottom(t *testing.T) {
	m := scrollableModel()

	// Rows: [clipboard, alpha, beta, code-review]. rpf=2, so visible is [0,2).
	// Pressing down three times walks the cursor to row 3 and drags the
	// viewport with it so scrollOffset lands at 2 (tail frame).
	for i := 0; i < 3; i++ {
		next, _ := m.Update(keyMsg("down"))
		m = next.(Model)
	}
	if m.cursor != 3 {
		t.Fatalf("cursor = %d, want 3", m.cursor)
	}
	if m.scrollOffset != 2 {
		t.Fatalf("scrollOffset = %d, want 2", m.scrollOffset)
	}
}

func TestUpdate_UpArrowScrollsOffsetPastTop(t *testing.T) {
	m := scrollableModel()
	m.cursor = 3
	m.scrollOffset = 2

	for i := 0; i < 3; i++ {
		next, _ := m.Update(keyMsg("up"))
		m = next.(Model)
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
	if m.scrollOffset != 0 {
		t.Fatalf("scrollOffset = %d, want 0", m.scrollOffset)
	}
}

func TestUpdate_ScrollOffsetClampedAtTail(t *testing.T) {
	m := scrollableModel()

	// Spam down far past the end. cursor clamps at len-1 (3); scrollOffset
	// clamps at len-rpf (2) so the last frame shows the tail without blank
	// rows below.
	for i := 0; i < 20; i++ {
		next, _ := m.Update(keyMsg("down"))
		m = next.(Model)
	}
	if m.cursor != 3 {
		t.Fatalf("cursor = %d, want 3 (len-1)", m.cursor)
	}
	if m.scrollOffset != 2 {
		t.Fatalf("scrollOffset = %d, want 2 (len-rpf)", m.scrollOffset)
	}
}

func TestUpdate_WindowResizeShrinkKeepsCursorVisible(t *testing.T) {
	m := NewModel(sampleState(), ModelDeps{})
	m.cursor = 3
	// Start in a tall terminal where everything fits (rpf=19); scrollOffset
	// is 0 because no scrolling has been needed yet.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = next.(Model)
	if m.scrollOffset != 0 {
		t.Fatalf("precondition: scrollOffset = %d, want 0", m.scrollOffset)
	}

	// Shrink to rpf=2. Cursor at row 3 must stay in-frame, so offset drags
	// up to 2 (cursor - rpf + 1).
	next, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 3})
	m = next.(Model)
	if m.scrollOffset != 2 {
		t.Fatalf("scrollOffset = %d, want 2 (cursor kept visible after shrink)", m.scrollOffset)
	}
}

func TestUpdate_WindowResizeGrowCollapsesBlankTail(t *testing.T) {
	m := scrollableModel()
	m.cursor = 3
	m.scrollOffset = 2

	// Grow to a terminal where every row fits. An offset of 2 would leave
	// blank rows below the tail; clamp collapses it to 0.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = next.(Model)
	if m.scrollOffset != 0 {
		t.Fatalf("scrollOffset = %d, want 0 (blank tail collapsed)", m.scrollOffset)
	}
}

func TestView_SlicesRowsToViewport(t *testing.T) {
	m := scrollableModel()
	m.cursor = 3
	m.scrollOffset = 2

	out := m.View()
	// Onscreen rows
	if !strings.Contains(out, "beta") || !strings.Contains(out, "code-review") {
		t.Fatalf("visible rows missing from view:\n%s", out)
	}
	// Offscreen rows (clipboard @0, alpha @1) must not appear. Match on
	// content that's unique to those rows so the footer doesn't false-
	// positive us.
	if strings.Contains(out, "alpha") || strings.Contains(out, "first") {
		t.Fatalf("offscreen alpha row leaked into view:\n%s", out)
	}
	if strings.Contains(out, "(read on select)") {
		t.Fatalf("offscreen clipboard row leaked into view:\n%s", out)
	}
}

func TestView_PreWindowSizeRendersAllRows(t *testing.T) {
	// No WindowSizeMsg ever sent, so height=0 and rowsPerFrame()==0. View
	// must render all rows instead of slicing with a zero window (which
	// would blank the board).
	m := NewModel(sampleState(), ModelDeps{})
	m.width = 80

	out := m.View()
	for _, id := range []string{"alpha", "beta", "code-review"} {
		if !strings.Contains(out, id) {
			t.Fatalf("pre-WindowSize view missing %q:\n%s", id, out)
		}
	}
}

func TestUpdate_BoundKeyOnScrolledOffRowSubmits(t *testing.T) {
	// The scroll invariant: pressing a row's assigned key must select it
	// even when the row is scrolled off-screen. Here alpha (row 1, key '1')
	// is offscreen — visible window is rows [2,4) — but pressing '1'
	// still resolves alpha and fires the submit cmd.
	deps, sub, _ := promptDeps(map[string]string{"alpha": "body"}, nil, nil)
	m := NewModel(sampleState(), deps)
	m.width = 80
	m.height = 3
	m.cursor = 3
	m.scrollOffset = 2

	next, cmd := m.Update(keyMsg("1"))
	got := next.(Model)

	if !got.pendingSubmit {
		t.Fatal("pendingSubmit should be true after selecting scrolled-off row")
	}
	if cmd == nil {
		t.Fatal("expected submit cmd for scrolled-off row key, got nil")
	}
	sr, ok := runCmd(cmd).(submitResultMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want submitResultMsg", sr)
	}
	if sr.result.Action != ActionPrompt || sr.result.PromptID != "alpha" {
		t.Fatalf("submitResultMsg.result = %+v, want ActionPrompt/alpha", sr.result)
	}
	if len(sub.calls) != 1 || sub.calls[0].PromptID != "alpha" {
		t.Fatalf("Submitter calls = %+v, want one call for alpha", sub.calls)
	}
}

func TestUpdate_ScrollingPreservesInlineError(t *testing.T) {
	// §19: navigation keys preserve inlineError even when they trigger a
	// scroll — otherwise a user with an error on screen would lose it the
	// moment they reach for the arrow keys.
	m := scrollableModel()
	m.inlineError = "prompt body exceeds max_paste_bytes — choose another prompt"

	for i := 0; i < 3; i++ {
		next, _ := m.Update(keyMsg("down"))
		m = next.(Model)
	}
	if m.scrollOffset == 0 {
		t.Fatal("precondition: expected scrolling to have advanced scrollOffset")
	}
	if m.inlineError == "" {
		t.Fatal("inlineError should be preserved through scroll-triggered navigation")
	}
}

func TestClampScrollOffset(t *testing.T) {
	cases := []struct {
		name     string
		cursor   int
		offset   int
		rowCount int
		rpf      int
		want     int
	}{
		{"pre-WindowSize rpf zero collapses", 2, 5, 10, 0, 0},
		{"empty row set collapses", 0, 3, 0, 2, 0},
		{"cursor inside window is noop", 2, 1, 10, 3, 1},
		{"cursor above window pulls offset down", 5, 0, 10, 2, 4},
		{"cursor below window pulls offset up", 0, 3, 10, 2, 0},
		{"offset past tail clamps to len-rpf", 3, 7, 4, 2, 2},
		{"negative offset clamped to zero", 0, -5, 4, 2, 0},
		{"rpf exceeds row count collapses to zero", 1, 3, 4, 10, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clampScrollOffset(tc.cursor, tc.offset, tc.rowCount, tc.rpf)
			if got != tc.want {
				t.Fatalf("clampScrollOffset(%d, %d, %d, %d) = %d, want %d",
					tc.cursor, tc.offset, tc.rowCount, tc.rpf, got, tc.want)
			}
		})
	}
}
