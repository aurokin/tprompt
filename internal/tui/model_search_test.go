package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hsadler/tprompt/internal/clipboard"
)

// searchStateWithRows returns a State shaped like the production buildTUIState
// output: clip row pinned first, then board rows, plus optional overflow. Keys
// default to digits so we don't collide with letter queries in tests.
func searchStateWithRows(board []Row, overflow []Row) State {
	rows := []Row{{Key: 'p', Description: "(read on select)"}}
	rows = append(rows, board...)
	return State{
		Rows:               rows,
		Overflow:           overflow,
		ClipboardAvailable: true,
		Reserved: ReservedKeys{
			Clipboard: ReservedBinding{Printable: 'p'},
			Search:    ReservedBinding{Printable: '/'},
			Cancel:    ReservedBinding{Symbolic: "Esc"},
			Select:    ReservedBinding{Symbolic: "Enter"},
		},
	}
}

func enterSearchViaSlash(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(keyMsg("/"))
	if cmd != nil {
		t.Fatalf("/ must not emit a cmd, got %T", cmd())
	}
	got := next.(Model)
	if got.mode != modeSearch {
		t.Fatalf("mode = %v, want modeSearch", got.mode)
	}
	return got
}

func promptIDs(results []MatchedRow) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Row.PromptID
	}
	return out
}

func TestUpdate_SlashEntersSearchModeWithCatalog(t *testing.T) {
	board := []Row{
		{Key: '1', PromptID: "zeta"},
		{Key: '2', PromptID: "alpha"},
	}
	overflow := []Row{{PromptID: "hidden"}}
	state := searchStateWithRows(board, overflow)
	m := NewModel(state, ModelDeps{})

	got := enterSearchViaSlash(t, m)

	if got.query != "" {
		t.Fatalf("query = %q, want empty", got.query)
	}
	want := []string{"", "alpha", "hidden", "zeta"} // clip first, then alpha
	if ids := promptIDs(got.results); !equalStringSlices(ids, want) {
		t.Fatalf("catalog = %v, want %v", ids, want)
	}
	if got.searchCursor != 0 {
		t.Fatalf("searchCursor = %d, want 0", got.searchCursor)
	}
	if got.highlightedPromptID != "" {
		t.Fatalf("highlightedPromptID = %q, want %q", got.highlightedPromptID, "")
	}
}

func TestUpdate_SearchLetterAppendsToQueryAndFilters(t *testing.T) {
	board := []Row{
		{Key: '1', PromptID: "apple"},
		{Key: '2', PromptID: "banana"},
	}
	m := NewModel(searchStateWithRows(board, nil), ModelDeps{})
	m = enterSearchViaSlash(t, m)

	next, _ := m.Update(keyMsg("a"))
	got := next.(Model)

	if got.query != "a" {
		t.Fatalf("query = %q, want %q", got.query, "a")
	}
	// Both ids contain 'a'. At minimum, non-matching clip row must be gone.
	if len(got.results) == 0 {
		t.Fatal("expected at least one match for 'a'")
	}
	for _, r := range got.results {
		if r.Row.PromptID == "" {
			t.Fatal("clip row leaked into non-empty query results")
		}
	}
}

func TestUpdate_SearchPAndSlashGoIntoQuery(t *testing.T) {
	board := []Row{{Key: '1', PromptID: "prompt/one"}}
	m := NewModel(searchStateWithRows(board, nil), ModelDeps{})
	m = enterSearchViaSlash(t, m)

	for _, r := range []string{"p", "/"} {
		next, _ := m.Update(keyMsg(r))
		m = next.(Model)
	}
	if m.query != "p/" {
		t.Fatalf("query = %q, want %q", m.query, "p/")
	}
}

func TestUpdate_SearchSpaceAppendsToQuery(t *testing.T) {
	// Bubble Tea emits a standalone space as tea.KeySpace; the search input
	// path must accept it so multi-word queries like "code review" work.
	board := []Row{
		{Key: '1', PromptID: "code-review"},
		{Key: '2', PromptID: "unrelated"},
	}
	m := NewModel(searchStateWithRows(board, nil), ModelDeps{})
	m = enterSearchViaSlash(t, m)

	for _, r := range []string{"c", "o", "d", "e"} {
		next, _ := m.Update(keyMsg(r))
		m = next.(Model)
	}
	next, _ := m.Update(keyMsg("space"))
	m = next.(Model)
	if m.query != "code " {
		t.Fatalf("query = %q, want %q (KeySpace must append a space)", m.query, "code ")
	}

	// And as a sanity check, a space emitted as KeyRunes works too.
	next2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = next2.(Model)
	if m.query != "code  " {
		t.Fatalf("query = %q, want %q (KeyRunes space must also append)", m.query, "code  ")
	}
}

func TestUpdate_SearchBackspacePopsLastRune(t *testing.T) {
	m := NewModel(searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil), ModelDeps{})
	m = enterSearchViaSlash(t, m)
	for _, r := range []string{"a", "b", "c"} {
		next, _ := m.Update(keyMsg(r))
		m = next.(Model)
	}
	if m.query != "abc" {
		t.Fatalf("query = %q before backspace", m.query)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	got := next.(Model)
	if got.query != "ab" {
		t.Fatalf("query = %q, want %q", got.query, "ab")
	}
}

func TestUpdate_SearchEscReturnsToBoardAndDoesNotQuit(t *testing.T) {
	m := NewModel(searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil), ModelDeps{})
	m = enterSearchViaSlash(t, m)
	next1, _ := m.Update(keyMsg("a"))
	m = next1.(Model)
	if m.query != "a" {
		t.Fatalf("pre-esc query = %q, want %q", m.query, "a")
	}

	next2, cmd := m.Update(keyMsg("esc"))
	got := next2.(Model)

	if got.mode != modeBoard {
		t.Fatalf("mode = %v, want modeBoard", got.mode)
	}
	if got.query != "" {
		t.Fatalf("query = %q, want empty", got.query)
	}
	if cmdIsQuit(cmd) {
		t.Fatal("Esc in search must not quit")
	}
	if got.result.Action != "" {
		t.Fatalf("result.Action = %q, want empty", got.result.Action)
	}
}

func TestUpdate_SearchEnterOnPromptSubmits(t *testing.T) {
	deps, sub, _ := promptDeps(map[string]string{"alpha": "body"}, nil, nil)
	state := searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil)
	m := NewModel(state, deps)
	m = enterSearchViaSlash(t, m)

	// Catalog is [clip, alpha]. Move cursor to alpha.
	next1, _ := m.Update(keyMsg("down"))
	m = next1.(Model)

	next2, cmd := m.Update(keyMsg("enter"))
	got := next2.(Model)

	if !got.pendingSubmit {
		t.Fatal("Enter on prompt must set pendingSubmit")
	}
	if cmd == nil {
		t.Fatal("Enter on prompt must return a cmd")
	}
	msg := runCmd(cmd)
	res, ok := msg.(submitResultMsg)
	if !ok {
		t.Fatalf("expected submitResultMsg, got %T", msg)
	}
	if res.result.Action != ActionPrompt || res.result.PromptID != "alpha" {
		t.Fatalf("submit result = %+v, want ActionPrompt/alpha", res.result)
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submit called %d times, want 1", len(sub.calls))
	}
}

func TestUpdate_SearchEnterOnOversizePromptSetsInlineError(t *testing.T) {
	deps, sub, _ := promptDeps(map[string]string{"alpha": "0123456789"}, nil, nil)
	deps.MaxPasteBytes = 4
	state := searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil)
	m := NewModel(state, deps)
	m = enterSearchViaSlash(t, m)

	next1, _ := m.Update(keyMsg("down"))
	m = next1.(Model)

	next2, cmd := m.Update(keyMsg("enter"))
	got := next2.(Model)

	if cmd != nil {
		t.Fatalf("oversize Enter must not return a cmd, got %T", cmd())
	}
	if got.pendingSubmit {
		t.Fatal("oversize Enter must not set pendingSubmit")
	}
	if !strings.Contains(got.inlineError, "max_paste_bytes") {
		t.Fatalf("inlineError = %q, want oversize message", got.inlineError)
	}
	if got.mode != modeSearch {
		t.Fatalf("mode = %v, want modeSearch (stay open)", got.mode)
	}
	if len(sub.calls) != 0 {
		t.Fatalf("Submit called %d times on oversize, want 0", len(sub.calls))
	}
}

// Remapped Select bindings must still trigger selection in search mode
// rather than being treated as printable query input. Regression coverage
// for the updateSearch dispatcher: matchesReserved(Select) is checked
// before KeyRunes/KeySpace fall through to query append.
func TestUpdate_SearchRemappedPrintableSelectSelectsHighlightedRow(t *testing.T) {
	deps, sub, _ := promptDeps(map[string]string{"alpha": "body"}, nil, nil)
	state := searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil)
	state.Reserved.Select = ReservedBinding{Printable: 'g'}
	m := NewModel(state, deps)
	m = enterSearchViaSlash(t, m)

	// Catalog is [clip, alpha]. Move cursor to alpha so 'g' selects a prompt.
	next1, _ := m.Update(keyMsg("down"))
	m = next1.(Model)

	next2, cmd := m.Update(keyMsg("g"))
	got := next2.(Model)

	if got.query != "" {
		t.Fatalf("remapped Select must not append to query, got %q", got.query)
	}
	if !got.pendingSubmit {
		t.Fatal("remapped Select on prompt must set pendingSubmit")
	}
	if cmd == nil {
		t.Fatal("remapped Select on prompt must return a cmd")
	}
	if msg := runCmd(cmd); msg == nil {
		t.Fatal("submit cmd must produce a result message")
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submit called %d times, want 1", len(sub.calls))
	}
}

func TestUpdate_SearchRemappedSpaceSelectSelectsHighlightedRow(t *testing.T) {
	deps, sub, _ := promptDeps(map[string]string{"alpha": "body"}, nil, nil)
	state := searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil)
	state.Reserved.Select = ReservedBinding{Symbolic: "Space"}
	m := NewModel(state, deps)
	m = enterSearchViaSlash(t, m)

	next1, _ := m.Update(keyMsg("down"))
	m = next1.(Model)

	next2, cmd := m.Update(keyMsg("space"))
	got := next2.(Model)

	if got.query != "" {
		t.Fatalf("Space-symbolic Select must not append space to query, got %q", got.query)
	}
	if !got.pendingSubmit {
		t.Fatal("Space-symbolic Select on prompt must set pendingSubmit")
	}
	if cmd == nil {
		t.Fatal("Space-symbolic Select on prompt must return a cmd")
	}
	if msg := runCmd(cmd); msg == nil {
		t.Fatal("submit cmd must produce a result message")
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submit called %d times, want 1", len(sub.calls))
	}
}

func TestUpdate_SearchEnterOnClipboardRowTriggersClipboardPath(t *testing.T) {
	deps, _, _ := promptDeps(nil, nil, nil)
	deps.Clip = clipboard.NewStatic([]byte("pasted"))

	state := searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil)
	m := NewModel(state, deps)
	m = enterSearchViaSlash(t, m)
	// Cursor starts at 0 → clip row is selected by default.
	if got := m.results[m.searchCursor].Row.PromptID; got != "" {
		t.Fatalf("initial cursor row PromptID = %q, want clip row", got)
	}

	next, cmd := m.Update(keyMsg("enter"))
	got := next.(Model)

	if !got.pendingClipboard {
		t.Fatal("Enter on clip row must set pendingClipboard")
	}
	if got.pendingSubmit {
		t.Fatal("Enter on clip row must not set pendingSubmit until after the read")
	}
	if cmd == nil {
		t.Fatal("Enter on clip row must return a clipboard read cmd")
	}
	msg := runCmd(cmd)
	crm, ok := msg.(clipboardReadMsg)
	if !ok {
		t.Fatalf("expected clipboardReadMsg, got %T", msg)
	}
	if crm.err != nil {
		t.Fatalf("read err = %v, want nil", crm.err)
	}
	if string(crm.body) != "pasted" {
		t.Fatalf("body = %q, want %q", crm.body, "pasted")
	}
}

func TestUpdate_SearchIncludesClipboardWhenBoardKeyDisabled(t *testing.T) {
	deps, _, _ := promptDeps(nil, nil, nil)
	deps.Clip = clipboard.NewStatic([]byte("pasted"))
	state := State{
		Rows:               []Row{{Key: '1', PromptID: "alpha"}},
		ClipboardAvailable: true,
		Reserved: ReservedKeys{
			Clipboard: ReservedBinding{Disabled: true},
			Search:    ReservedBinding{Printable: '/'},
			Cancel:    ReservedBinding{Symbolic: "Esc"},
			Select:    ReservedBinding{Symbolic: "Enter"},
		},
	}
	m := NewModel(state, deps)
	m = enterSearchViaSlash(t, m)

	if ids := promptIDs(m.results); len(ids) != 2 || ids[0] != "" || ids[1] != "alpha" {
		t.Fatalf("search catalog = %v, want clipboard row then alpha", ids)
	}
	if m.results[0].Row.Key != 0 {
		t.Fatalf("search-only clipboard row key = %q, want zero/keyless", m.results[0].Row.Key)
	}

	next, cmd := m.Update(keyMsg("enter"))
	got := next.(Model)
	if !got.pendingClipboard {
		t.Fatal("Enter on search-only clipboard row must set pendingClipboard")
	}
	if cmd == nil {
		t.Fatal("Enter on search-only clipboard row must return clipboard read cmd")
	}
}

func TestUpdate_SearchCtrlCCancelsAndQuits(t *testing.T) {
	m := NewModel(searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil), ModelDeps{})
	m = enterSearchViaSlash(t, m)

	next, cmd := m.Update(keyMsg("ctrl+c"))
	got := next.(Model)

	if got.result.Action != ActionCancel {
		t.Fatalf("Action = %q, want %q", got.result.Action, ActionCancel)
	}
	if !cmdIsQuit(cmd) {
		t.Fatal("Ctrl+C in search must return tea.Quit")
	}
}

func TestUpdate_SearchHighlightAnchoringPreserved(t *testing.T) {
	// Rows all contain 'a', so typing 'a' keeps every row in results.
	// Moving to alpha then typing 'a' must preserve alpha as the highlight.
	board := []Row{
		{Key: '1', PromptID: "alpha"},
		{Key: '2', PromptID: "banana"},
		{Key: '3', PromptID: "apricot"},
	}
	m := NewModel(searchStateWithRows(board, nil), ModelDeps{})
	m = enterSearchViaSlash(t, m)

	// Catalog: [clip, alpha, apricot, banana]. Move down to alpha (index 1).
	next, _ := m.Update(keyMsg("down"))
	m = next.(Model)
	if m.highlightedPromptID != "alpha" {
		t.Fatalf("setup: highlightedPromptID = %q, want %q", m.highlightedPromptID, "alpha")
	}

	next2, _ := m.Update(keyMsg("a"))
	got := next2.(Model)
	if got.highlightedPromptID != "alpha" {
		t.Fatalf("after query edit: highlightedPromptID = %q, want %q", got.highlightedPromptID, "alpha")
	}
	// Find alpha in results and confirm cursor matches that index.
	foundIdx := -1
	for i, r := range got.results {
		if r.Row.PromptID == "alpha" {
			foundIdx = i
			break
		}
	}
	if foundIdx < 0 {
		t.Fatalf("alpha missing from results: %v", promptIDs(got.results))
	}
	if got.searchCursor != foundIdx {
		t.Fatalf("searchCursor = %d, want %d (alpha's index)", got.searchCursor, foundIdx)
	}
}

func TestUpdate_SearchHighlightAnchoringLostFallsToZero(t *testing.T) {
	board := []Row{
		{Key: '1', PromptID: "apple"},
		{Key: '2', PromptID: "banana"},
	}
	m := NewModel(searchStateWithRows(board, nil), ModelDeps{})
	m = enterSearchViaSlash(t, m)
	// Catalog: [clip, apple, banana]. Move to banana (index 2).
	for i := 0; i < 2; i++ {
		next, _ := m.Update(keyMsg("down"))
		m = next.(Model)
	}
	if m.highlightedPromptID != "banana" {
		t.Fatalf("setup: highlighted = %q, want banana", m.highlightedPromptID)
	}

	// Type 'p' — "banana" has no 'p', so it drops from results; "apple" matches.
	next, _ := m.Update(keyMsg("p"))
	got := next.(Model)

	if got.searchCursor != 0 {
		t.Fatalf("searchCursor = %d, want 0 after anchor lost", got.searchCursor)
	}
	if len(got.results) > 0 && got.highlightedPromptID != got.results[0].Row.PromptID {
		t.Fatalf("highlightedPromptID = %q, want first result %q",
			got.highlightedPromptID, got.results[0].Row.PromptID)
	}
}

func TestView_BoardFooterShowsNMoreWhenOverflow(t *testing.T) {
	board := []Row{{Key: '1', PromptID: "alpha"}}
	overflow := []Row{{PromptID: "x"}, {PromptID: "y"}, {PromptID: "z"}}
	state := searchStateWithRows(board, overflow)
	m := NewModel(state, ModelDeps{})
	m.width = 80

	out := m.View()
	if !strings.Contains(out, "[/ search (3 more)]") {
		t.Fatalf("board footer must show (3 more). Got:\n%s", out)
	}
}

func TestView_BoardFooterOmitsNMoreWhenOverflowEmpty(t *testing.T) {
	state := searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil)
	m := NewModel(state, ModelDeps{})
	m.width = 80

	out := m.View()
	if strings.Contains(out, "more)") {
		t.Fatalf("no-overflow footer must not show `more)`. Got:\n%s", out)
	}
	if !strings.Contains(out, "[/ search]") {
		t.Fatalf("no-overflow footer must show plain [/ search]. Got:\n%s", out)
	}
}

func TestView_SearchFooterShowsMatchCount(t *testing.T) {
	board := []Row{
		{Key: '1', PromptID: "alpha"},
		{Key: '2', PromptID: "beta"},
	}
	m := NewModel(searchStateWithRows(board, nil), ModelDeps{})
	m.width = 80
	m = enterSearchViaSlash(t, m)

	out := m.View()
	// Empty-query catalog = 3 entries (clip + alpha + beta).
	if !strings.Contains(out, "[3 matches]") {
		t.Fatalf("search footer must show [3 matches]. Got:\n%s", out)
	}
	if !strings.Contains(out, "[Esc exit search]") {
		t.Fatalf("search footer must show [Esc exit search]. Got:\n%s", out)
	}
	if !strings.Contains(out, "[Enter select]") {
		t.Fatalf("search footer must show [Enter select]. Got:\n%s", out)
	}
}

func TestView_SearchSlicesRowsToViewport(t *testing.T) {
	board := []Row{
		{Key: '1', PromptID: "alpha", Description: "first"},
		{Key: '2', PromptID: "beta", Description: "second"},
		{Key: '3', PromptID: "delta", Description: "third"},
		{Key: '4', PromptID: "gamma", Description: "fourth"},
	}
	m := NewModel(searchStateWithRows(board, nil), ModelDeps{})
	m.width = 80
	m.height = 3 // rowsPerFrame = 2
	m = enterSearchViaSlash(t, m)
	for i := 0; i < 3; i++ {
		next, _ := m.Update(keyMsg("down"))
		m = next.(Model)
	}

	if m.searchCursor != 3 {
		t.Fatalf("searchCursor = %d, want 3", m.searchCursor)
	}
	if m.searchScrollOffset != 2 {
		t.Fatalf("searchScrollOffset = %d, want 2", m.searchScrollOffset)
	}
	out := m.View()
	if !strings.Contains(out, "beta") || !strings.Contains(out, "delta") {
		t.Fatalf("visible search rows missing from view:\n%s", out)
	}
	if strings.Contains(out, "(read on select)") || strings.Contains(out, "alpha") {
		t.Fatalf("offscreen search rows leaked into view:\n%s", out)
	}
	if !strings.Contains(out, "[5 matches]") {
		t.Fatalf("search footer should remain visible after slicing:\n%s", out)
	}
}

func TestUpdate_SearchEnterIgnoredWhilePending(t *testing.T) {
	deps, sub, _ := promptDeps(map[string]string{"alpha": "body"}, nil, nil)
	m := NewModel(searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil), deps)
	m = enterSearchViaSlash(t, m)
	m.pendingSubmit = true

	next, cmd := m.Update(keyMsg("enter"))
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("Enter while pending must not emit cmd, got %T", cmd())
	}
	if !got.pendingSubmit {
		t.Fatal("pendingSubmit must remain true")
	}
	if len(sub.calls) != 0 {
		t.Fatalf("Submit calls = %d, want 0 while pending", len(sub.calls))
	}
}

func TestUpdate_SearchCancelIgnoredWhilePending(t *testing.T) {
	m := NewModel(searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil), ModelDeps{})
	m = enterSearchViaSlash(t, m)
	m.pendingSubmit = true

	for _, key := range []string{"ctrl+c", "esc"} {
		next, cmd := m.Update(keyMsg(key))
		got := next.(Model)
		if cmdIsQuit(cmd) {
			t.Fatalf("%s while pending must not quit", key)
		}
		if got.result.Action == ActionCancel {
			t.Fatalf("%s while pending must not set cancel", key)
		}
		if !got.pendingSubmit {
			t.Fatalf("%s while pending must preserve pendingSubmit", key)
		}
		if key == "esc" && got.mode != modeSearch {
			t.Fatalf("esc while pending must stay in modeSearch, got %v", got.mode)
		}
	}
}

func TestUpdate_ClipboardReadMsgOnErrorSetsInlineError(t *testing.T) {
	m := NewModel(searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil), ModelDeps{})
	m.pendingClipboard = true

	next, cmd := m.Update(clipboardReadMsg{err: errors.New("clipboard is empty")})
	got := next.(Model)

	if cmd != nil {
		t.Fatalf("error clipboardReadMsg must not emit a cmd, got %T", cmd())
	}
	if got.pendingClipboard {
		t.Fatal("error clipboardReadMsg must clear pendingClipboard")
	}
	if !strings.Contains(got.inlineError, "clipboard is empty") {
		t.Fatalf("inlineError = %q, want error text", got.inlineError)
	}
}

func TestUpdate_ClipboardReadMsgOnSuccessFiresSubmitCmd(t *testing.T) {
	deps, sub, _ := promptDeps(nil, nil, nil)
	m := NewModel(searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil), deps)
	m.pendingClipboard = true

	next, cmd := m.Update(clipboardReadMsg{body: []byte("pasted")})
	got := next.(Model)

	if got.pendingClipboard {
		t.Fatal("success clipboardReadMsg must clear pendingClipboard")
	}
	if !got.pendingSubmit {
		t.Fatal("success clipboardReadMsg must set pendingSubmit until submitResultMsg")
	}
	if cmd == nil {
		t.Fatal("success clipboardReadMsg must return submitCmd")
	}
	msg := runCmd(cmd)
	res, ok := msg.(submitResultMsg)
	if !ok {
		t.Fatalf("expected submitResultMsg, got %T", msg)
	}
	if res.result.Action != ActionClipboard {
		t.Fatalf("Action = %q, want %q", res.result.Action, ActionClipboard)
	}
	if string(res.result.ClipboardBody) != "pasted" {
		t.Fatalf("ClipboardBody = %q, want %q", res.result.ClipboardBody, "pasted")
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submit called %d times, want 1", len(sub.calls))
	}
}

func TestView_SearchRendersOverflowRowWithoutNUL(t *testing.T) {
	// Overflow rows have Key == 0 by design. When they surface in search
	// results they must not leak a NUL byte into the rendered [key] column.
	board := []Row{{Key: '1', PromptID: "alpha"}}
	overflow := []Row{{PromptID: "hidden-gem", Description: "an overflow prompt"}}
	m := NewModel(searchStateWithRows(board, overflow), ModelDeps{})
	m.width = 80
	m = enterSearchViaSlash(t, m)

	out := m.View()
	if strings.Contains(out, "\x00") {
		t.Fatalf("search view must not contain NUL bytes; got %q", out)
	}
	// The keyless row must still render something in the key column — a
	// space keeps the column width fixed.
	if !strings.Contains(out, "[ ]  hidden-gem") {
		t.Fatalf("expected overflow row to render with blank key column, got:\n%s", out)
	}
}

func TestUpdate_SearchSlashEntryIgnoredWhilePending(t *testing.T) {
	// Belt-and-suspenders: / in board mode must also be gated on pendingSubmit,
	// since entering search could let the user Enter-select mid-flight.
	m := NewModel(searchStateWithRows([]Row{{Key: '1', PromptID: "alpha"}}, nil), ModelDeps{})
	m.pendingSubmit = true
	before := m.mode

	next, cmd := m.Update(keyMsg("/"))
	got := next.(Model)
	if cmd != nil {
		t.Fatalf("/ while pending must not emit cmd, got %T", cmd())
	}
	if got.mode != before {
		t.Fatalf("mode changed to %v while pending, want %v", got.mode, before)
	}
}
