package tui

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
