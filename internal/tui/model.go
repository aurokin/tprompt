package tui

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/store"
)

// Submitter is the subset of internal/submitter.Submitter the Model depends
// on. Declared locally so the tui package does not import submitter (which
// imports tui for Result).
type Submitter interface {
	Submit(Result) error
}

// submitResultMsg carries the outcome of an async Submitter.Submit call back
// into Update so the Model can transition to tea.Quit with the captured
// Result and error.
type submitResultMsg struct {
	result Result
	err    error
}

// submitCmd returns a tea.Cmd that invokes the injected Submitter and reports
// the outcome via submitResultMsg. The result is echoed back on the message so
// Update doesn't have to stash pending selections on the Model.
func submitCmd(sub Submitter, result Result) tea.Cmd {
	return func() tea.Msg {
		return submitResultMsg{result: result, err: sub.Submit(result)}
	}
}

// clipboardReadMsg carries the outcome of an async clipboard Read + Validate
// back into Update. err is non-nil for empty/non-UTF-8/oversize or reader
// failure; body is the validated bytes on success.
type clipboardReadMsg struct {
	body []byte
	err  error
}

// errClipboardUnavailable is surfaced as a clipboardReadMsg error when the
// Model was constructed without a clipboard.Reader. Production never trips
// this (buildTUIState omits the clipboard row + matchesReserved ignores a
// disabled binding), but zero-value ModelDeps in tests or bespoke wiring
// should degrade to an inline footer error rather than panicking.
var errClipboardUnavailable = errors.New("clipboard is unavailable — choose another option")

// readClipboardCmd returns a tea.Cmd that invokes the injected Reader and
// validates the result against the paste limit, emitting clipboardReadMsg.
func readClipboardCmd(r clipboard.Reader, limit int64) tea.Cmd {
	return func() tea.Msg {
		if r == nil {
			return clipboardReadMsg{err: errClipboardUnavailable}
		}
		body, err := r.Read()
		if err != nil {
			return clipboardReadMsg{err: err}
		}
		if err := clipboard.Validate(body, limit); err != nil {
			return clipboardReadMsg{err: err}
		}
		return clipboardReadMsg{body: body}
	}
}

// ModelDeps are the capabilities the Model reaches into during event handling.
// Phase 5b slices beyond the tracer bullet (AUR-24, AUR-25) populate these;
// the tracer bullet leaves the Submitter/Clip/Store fields zero-valued because
// Esc/Ctrl+C/cursor-nav do not touch them.
type ModelDeps struct {
	Submitter     Submitter
	Clip          clipboard.Reader
	Store         store.Store
	MaxPasteBytes int64
}

// mode distinguishes board rendering from search.
type mode int

const (
	modeBoard mode = iota
	modeSearch
)

// Model is the single bubbletea model for the TUI.
type Model struct {
	state            State
	deps             ModelDeps
	mode             mode
	cursor           int
	scrollOffset     int
	width            int
	height           int
	result           Result
	inlineError      string
	submitErr        error
	pendingSubmit    bool
	pendingClipboard bool

	// Search-mode state.
	query               string
	searchCursor        int
	searchScrollOffset  int
	highlightedPromptID string
	results             []MatchedRow
	index               *SearchIndex
}

// headerLines and footerLines are the fixed chrome subtracted from terminal
// height to compute rowsPerFrame. No header is rendered yet; the footer is a
// single hint/error line composed by m.footer(). Future slices that grow the
// chrome (e.g. a multi-line search query line) should update these.
const (
	headerLines = 0
	footerLines = 1
)

// NewModel constructs a Model seeded with the rendered state and deps.
func NewModel(state State, deps ModelDeps) Model {
	return Model{state: state, deps: deps, mode: modeBoard}
}

// Result returns the Result captured at the moment the Model issued tea.Quit.
// The Renderer wrapper reads this after bubbletea returns.
func (m Model) Result() Result { return m.result }

// SubmitErr returns any error returned by the injected Submitter during the
// session. The Renderer wrapper surfaces this up from Run so runTUI can map
// it to the appropriate exit code.
func (m Model) SubmitErr() error { return m.submitErr }

// Init satisfies tea.Model. No startup command yet.
func (m Model) Init() tea.Cmd { return nil }

// rowsPerFrame returns how many row lines fit in the current viewport. Returns
// 0 pre-WindowSizeMsg (m.height == 0) or when the chrome exceeds the window;
// View treats that as "render all rows" so tests without a terminal still
// see everything.
func (m Model) rowsPerFrame() int {
	rpf := m.height - headerLines - footerLines
	if rpf <= 0 {
		return 0
	}
	return rpf
}

// clampScrollOffset returns the scrollOffset that keeps cursor visible in the
// window [offset, offset+rpf) and prevents overscroll past the list tail.
// rpf <= 0 (pre-WindowSizeMsg) or an empty row set collapse to offset 0.
func clampScrollOffset(cursor, offset, rowCount, rpf int) int {
	if rpf <= 0 || rowCount == 0 {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+rpf {
		offset = cursor - rpf + 1
	}
	max := rowCount - rpf
	if max < 0 {
		max = 0
	}
	if offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// Update handles inbound messages. Keypress handling forks by mode so search
// can layer in without disturbing board handling.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.scrollOffset = clampScrollOffset(m.cursor, m.scrollOffset, len(m.state.Rows), m.rowsPerFrame())
		m.searchScrollOffset = clampScrollOffset(m.searchCursor, m.searchScrollOffset, len(m.results), m.rowsPerFrame())
		return m, nil
	case submitResultMsg:
		m.pendingSubmit = false
		m.result = msg.result
		m.submitErr = msg.err
		return m, tea.Quit
	case clipboardReadMsg:
		m.pendingClipboard = false
		if msg.err != nil {
			m.inlineError = clipboardErrorText(msg.err)
			return m, nil
		}
		m.pendingSubmit = true
		return m, submitCmd(m.deps.Submitter, Result{
			Action:        ActionClipboard,
			ClipboardBody: msg.body,
		})
	case tea.KeyMsg:
		switch m.mode {
		case modeBoard:
			return m.updateBoard(msg)
		case modeSearch:
			return m.updateSearch(msg)
		}
	}
	return m, nil
}

func (m Model) updateBoard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Catch Ctrl+C explicitly before bubbletea's default SIGINT path so the
	// cancel Result is captured instead of surfacing as ErrProgramKilled.
	switch {
	case msg.Type == tea.KeyCtrlC, matchesReserved(msg, m.state.Reserved.Cancel):
		if m.pendingSubmit {
			// A submit is in flight; cancelling here would exit 0 and drop
			// any error the daemon returns, silently losing the outcome.
			// Wait for submitResultMsg to decide the exit code.
			return m, nil
		}
		m.inlineError = ""
		m.result = Result{Action: ActionCancel}
		return m, tea.Quit
	case msg.Type == tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			m.scrollOffset = clampScrollOffset(m.cursor, m.scrollOffset, len(m.state.Rows), m.rowsPerFrame())
		}
		// ↑/↓ preserve inlineError per §19.
		return m, nil
	case msg.Type == tea.KeyDown:
		if m.cursor < len(m.state.Rows)-1 {
			m.cursor++
			m.scrollOffset = clampScrollOffset(m.cursor, m.scrollOffset, len(m.state.Rows), m.rowsPerFrame())
		}
		return m, nil
	case matchesReserved(msg, m.state.Reserved.Clipboard):
		// Ignore re-presses while an async read or submit is already in
		// flight — prevents key repeat from enqueuing duplicate reads or
		// racing the submit that follows a successful read.
		if m.pendingClipboard || m.pendingSubmit {
			return m, nil
		}
		m.inlineError = ""
		m.pendingClipboard = true
		return m, readClipboardCmd(m.deps.Clip, m.deps.MaxPasteBytes)
	case matchesReserved(msg, m.state.Reserved.Search):
		if m.pendingSubmit || m.pendingClipboard {
			return m, nil
		}
		m.inlineError = ""
		return m.enterSearch(), nil
	}

	return m.tryPromptSelect(msg)
}

// tryPromptSelect matches a single-rune printable keypress against the
// assigned row keys and dispatches prompt selection. Returns the model and
// any cmd; unassigned keypresses are no-ops that preserve inlineError.
func (m Model) tryPromptSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Gate prompt selection while a submit is in flight. Without this a slow
	// Submitter combined with key repeat could enqueue multiple submitCmds
	// and produce duplicate daemon submissions from one interaction. Same
	// treatment for an in-flight clipboard read: a prompt keypress arriving
	// between the read cmd and clipboardReadMsg would race the pending flow.
	if m.pendingSubmit || m.pendingClipboard {
		return m, nil
	}
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return m, nil
	}
	got := msg.Runes[0]

	for _, row := range m.state.Rows {
		if row.PromptID == "" {
			// Pinned clipboard row — its key is handled via the Reserved.Clipboard
			// case above, so skip it here to avoid a double match.
			continue
		}
		if !promptKeyMatches(got, row.Key) {
			continue
		}
		return m.selectPrompt(row.PromptID)
	}
	return m, nil
}

// selectPrompt resolves the prompt body, enforces MaxPasteBytes inline, and
// fires the submit cmd when the body is within limits. Store or resolve
// failures propagate through submitErr so the Renderer wrapper surfaces them.
func (m Model) selectPrompt(id string) (tea.Model, tea.Cmd) {
	prompt, err := m.deps.Store.Resolve(id)
	if err != nil {
		// The pre-flight validated the store, but a prompt file can still be
		// removed between List and Resolve. Bubble as a submit failure so the
		// exit-code mapping in runTUI handles it like any other store error.
		m.result = Result{Action: ActionPrompt, PromptID: id}
		m.submitErr = err
		return m, tea.Quit
	}
	if m.deps.MaxPasteBytes > 0 && int64(len(prompt.Body)) > m.deps.MaxPasteBytes {
		m.inlineError = "prompt body exceeds max_paste_bytes — choose another prompt"
		return m, nil
	}
	m.inlineError = ""
	m.pendingSubmit = true
	return m, submitCmd(m.deps.Submitter, Result{Action: ActionPrompt, PromptID: id})
}

// selectClipboard kicks off an async clipboard read via readClipboardCmd. The
// resulting clipboardReadMsg hands off to submitCmd in Update on success, or
// populates inlineError on failure. Shared between board-mode P and
// search-mode Enter-on-clipboard-row.
func (m Model) selectClipboard() (tea.Model, tea.Cmd) {
	if m.pendingSubmit || m.pendingClipboard {
		return m, nil
	}
	m.inlineError = ""
	m.pendingClipboard = true
	return m, readClipboardCmd(m.deps.Clip, m.deps.MaxPasteBytes)
}

// clipboardErrorText maps a clipboard read/validate error to the inline
// footer text shown after a failed P-press. The empty-clipboard string is
// fixed by user story 17; the other two follow the same "— choose another
// option" pattern so the footer stays consistent across validation failures.
func clipboardErrorText(err error) string {
	switch e := err.(type) {
	case *clipboard.EmptyClipboardError:
		return "clipboard is empty — choose another option"
	case *clipboard.InvalidUTF8Error:
		return "clipboard contains non-UTF-8 data — choose another option"
	case *clipboard.OversizeError:
		return fmt.Sprintf("clipboard exceeds max_paste_bytes (%d > %d) — choose another option", e.Bytes, e.Limit)
	default:
		return err.Error()
	}
}

// promptKeyMatches implements the case-insensitive keybind contract: letters
// fold to lowercase on both sides, non-letters must match literally.
func promptKeyMatches(got, bound rune) bool {
	if unicode.IsLetter(bound) && unicode.IsLetter(got) {
		return unicode.ToLower(got) == unicode.ToLower(bound)
	}
	return got == bound
}

// enterSearch transitions from board to search mode. It builds the index on
// first use, seeds results with the empty-query catalog, and anchors the
// search cursor at the first entry.
func (m Model) enterSearch() Model {
	m.mode = modeSearch
	m.query = ""
	if m.index == nil {
		clip, hasClip := clipboardRow(m.state.Rows)
		boardRows := m.state.Rows
		if hasClip {
			boardRows = boardRows[1:]
		}
		if !hasClip && m.state.ClipboardAvailable {
			clip = Row{Description: "(read on select)"}
		}
		m.index = newSearchIndex(boardRows, m.state.Overflow, clip)
	}
	m.results = m.index.Query("")
	m.searchCursor = 0
	m.searchScrollOffset = 0
	if len(m.results) > 0 {
		m.highlightedPromptID = m.results[0].Row.PromptID
	} else {
		m.highlightedPromptID = ""
	}
	return m
}

// refilter rebuilds results from the current query and relocates the cursor
// to the prior highlighted PromptID if it still appears in the new result set;
// otherwise the cursor lands at index 0.
func (m Model) refilter() Model {
	anchorID := m.highlightedPromptID
	m.results = m.index.Query(m.query)
	m.searchCursor = 0
	if anchorID != "" {
		for i, r := range m.results {
			if r.Row.PromptID == anchorID {
				m.searchCursor = i
				break
			}
		}
	}
	if m.searchCursor < len(m.results) {
		m.highlightedPromptID = m.results[m.searchCursor].Row.PromptID
	} else {
		m.highlightedPromptID = ""
	}
	m.searchScrollOffset = clampScrollOffset(m.searchCursor, m.searchScrollOffset, len(m.results), m.rowsPerFrame())
	return m
}

// updateSearch routes search-mode keypresses. Reserved semantics (docs
// §Search mode): Esc returns to board (never quits); Ctrl+C cancels; Enter
// selects; ↑/↓ navigate; Backspace pops one rune; any other single-rune
// keypress appends to the query.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Esc always exits search mode back to the board — even if Cancel is
	// bound to Esc at the config level. Per ticket: "Esc does NOT exit the
	// TUI in search mode".
	if msg.Type == tea.KeyEsc {
		if m.pendingSubmit || m.pendingClipboard {
			return m, nil
		}
		m.mode = modeBoard
		m.query = ""
		m.results = nil
		m.highlightedPromptID = ""
		m.searchCursor = 0
		m.searchScrollOffset = 0
		m.inlineError = ""
		return m, nil
	}

	// Ctrl+C: cancel + Quit. Literal Ctrl+C only — remapped printable Cancel
	// bindings go into the search query like any other printable rune so the
	// user can actually search for that character.
	if msg.Type == tea.KeyCtrlC {
		if m.pendingSubmit {
			return m, nil
		}
		m.inlineError = ""
		m.result = Result{Action: ActionCancel}
		return m, tea.Quit
	}

	// Enter (or remapped Select): select the highlighted row.
	if matchesReserved(msg, m.state.Reserved.Select) {
		if m.pendingSubmit || m.pendingClipboard {
			return m, nil
		}
		if m.searchCursor < 0 || m.searchCursor >= len(m.results) {
			return m, nil
		}
		row := m.results[m.searchCursor].Row
		if row.PromptID == "" {
			return m.selectClipboard()
		}
		return m.selectPrompt(row.PromptID)
	}

	// ↑/↓: navigate within results. Navigation preserves inlineError (§19).
	if msg.Type == tea.KeyUp {
		if m.searchCursor > 0 {
			m.searchCursor--
			m.searchScrollOffset = clampScrollOffset(m.searchCursor, m.searchScrollOffset, len(m.results), m.rowsPerFrame())
			m.highlightedPromptID = m.results[m.searchCursor].Row.PromptID
		}
		return m, nil
	}
	if msg.Type == tea.KeyDown {
		if m.searchCursor < len(m.results)-1 {
			m.searchCursor++
			m.searchScrollOffset = clampScrollOffset(m.searchCursor, m.searchScrollOffset, len(m.results), m.rowsPerFrame())
			m.highlightedPromptID = m.results[m.searchCursor].Row.PromptID
		}
		return m, nil
	}

	// Backspace: pop one rune and re-filter. A query edit is a real action,
	// so inlineError clears.
	if msg.Type == tea.KeyBackspace {
		if len(m.query) > 0 {
			runes := []rune(m.query)
			m.query = string(runes[:len(runes)-1])
			m = m.refilter()
			m.inlineError = ""
		}
		return m, nil
	}

	// Any other single-rune keypress (no Alt modifier): append to query.
	// Bubble Tea can emit a standalone space as either tea.KeySpace or a
	// tea.KeyRunes with Runes[0] == ' ', so accept both forms — otherwise
	// multi-word queries like "code review" would drop the space.
	if msg.Type == tea.KeySpace && !msg.Alt {
		m.query += " "
		m = m.refilter()
		m.inlineError = ""
		return m, nil
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && !msg.Alt {
		m.query += string(msg.Runes[0])
		m = m.refilter()
		m.inlineError = ""
		return m, nil
	}

	return m, nil
}

// View renders the mode-appropriate body and footer hint.
func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	if m.mode == modeSearch {
		return m.viewSearch(width)
	}
	return m.viewBoard(width)
}

func (m Model) viewBoard(width int) string {
	var sb strings.Builder
	idWidth := maxIDWidth(m.state.Rows)
	const keyCol = 3 // "[X]"
	const padding = 2
	descCol := width - keyCol - idWidth - padding*2
	if descCol < 0 {
		descCol = 0
	}

	start, end := m.visibleRowRange()
	for i, row := range m.state.Rows[start:end] {
		line := renderRow(row, idWidth, descCol)
		if start+i == m.cursor {
			line = selectedStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString(m.footer())
	return sb.String()
}

func (m Model) viewSearch(width int) string {
	rows := make([]Row, len(m.results))
	for i, r := range m.results {
		rows[i] = r.Row
	}
	start, end := m.visibleSearchRowRange()
	return renderRowList(rows[start:end], m.searchCursor-start, width, maxIDWidth(rows)) + m.footer()
}

// renderRowList formats rows as the three-column keybind board, highlighting
// the row at cursor. Used by the search view after slicing to the current
// viewport; idWidth is computed from the complete result set to keep columns
// stable while scrolling.
func renderRowList(rows []Row, cursor, width, idWidth int) string {
	var sb strings.Builder
	const keyCol = 3 // "[X]"
	const padding = 2
	descCol := width - keyCol - idWidth - padding*2
	if descCol < 0 {
		descCol = 0
	}
	for i, row := range rows {
		line := renderRow(row, idWidth, descCol)
		if i == cursor {
			line = selectedStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) visibleSearchRowRange() (int, int) {
	rows := len(m.results)
	rpf := m.rowsPerFrame()
	if rpf <= 0 || rows <= rpf {
		return 0, rows
	}
	end := m.searchScrollOffset + rpf
	if end > rows {
		end = rows
	}
	return m.searchScrollOffset, end
}

// visibleRowRange returns [start, end) of m.state.Rows that fit in the current
// viewport. Pre-WindowSizeMsg (rpf == 0) or when every row fits, it returns
// the full range so the render-all fallback satisfies pre-WindowSize tests
// and avoids division-by-zero paths.
func (m Model) visibleRowRange() (int, int) {
	rows := len(m.state.Rows)
	rpf := m.rowsPerFrame()
	if rpf <= 0 || rows <= rpf {
		return 0, rows
	}
	end := m.scrollOffset + rpf
	if end > rows {
		end = rows
	}
	return m.scrollOffset, end
}

var selectedStyle = lipgloss.NewStyle().Reverse(true)

func renderRow(row Row, idWidth, descWidth int) string {
	key := "[" + displayKey(row.Key) + "]"
	id := padRight(row.PromptID, idWidth)
	desc := truncateToWidth(row.DisplayDescription(), descWidth)
	return fmt.Sprintf("%s  %s  %s", key, id, desc)
}

func maxIDWidth(rows []Row) int {
	max := 0
	for _, r := range rows {
		if w := lipgloss.Width(r.PromptID); w > max {
			max = w
		}
	}
	return max
}

func padRight(s string, width int) string {
	pad := width - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// truncateToWidth returns s trimmed so its rendered width does not exceed
// maxWidth, appending an ellipsis when trimming occurred.
func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		candidate := string(runes) + "…"
		if lipgloss.Width(candidate) <= maxWidth {
			return candidate
		}
		runes = runes[:len(runes)-1]
	}
	return "…"
}

func (m Model) footer() string {
	base := m.footerHints()
	if m.inlineError == "" {
		return base
	}
	if base == "" {
		return m.inlineError
	}
	return m.inlineError + "  " + base
}

func (m Model) footerHints() string {
	if m.mode == modeSearch {
		return m.searchFooterHints()
	}
	if m.isEmptyStore() {
		return m.emptyStoreFooter()
	}
	var parts []string
	if search := m.boardSearchHint(); search != "" {
		parts = append(parts, search)
	}
	if cancel := footerHint(m.state.Reserved.Cancel, "cancel"); cancel != "" {
		parts = append(parts, cancel)
	}
	return strings.Join(parts, "  ")
}

// boardSearchHint returns the `[/ search]` hint with ` (N more)` suffixed
// inside the brackets when overflow rows exist. Returns empty when search is
// disabled.
func (m Model) boardSearchHint() string {
	label := footerHint(m.state.Reserved.Search, "search")
	if label == "" || len(m.state.Overflow) == 0 {
		return label
	}
	return strings.TrimSuffix(label, "]") + fmt.Sprintf(" (%d more)]", len(m.state.Overflow))
}

// searchFooterHints renders the search-mode footer:
// `/query  [Esc exit search]  [Enter select]  [N matches]`.
func (m Model) searchFooterHints() string {
	parts := []string{"/" + m.query}
	parts = append(parts, "[Esc exit search]")
	if sel := footerHint(m.state.Reserved.Select, "select"); sel != "" {
		parts = append(parts, sel)
	}
	parts = append(parts, fmt.Sprintf("[%d matches]", len(m.results)))
	return strings.Join(parts, "  ")
}

func (m Model) emptyStoreFooter() string {
	const prefix = "no prompts found"
	if clip, ok := clipboardRow(m.state.Rows); ok {
		clipHint := displayReserved(ReservedBinding{Printable: clip.Key})
		cancelHint := displayReserved(m.state.Reserved.Cancel)
		switch {
		case clipHint != "" && cancelHint != "":
			return fmt.Sprintf("%s — press %s for clipboard or %s to exit", prefix, clipHint, cancelHint)
		case clipHint != "":
			return fmt.Sprintf("%s — press %s for clipboard", prefix, clipHint)
		}
	}
	if cancelHint := displayReserved(m.state.Reserved.Cancel); cancelHint != "" {
		return fmt.Sprintf("%s — press %s to exit", prefix, cancelHint)
	}
	return prefix
}

// isEmptyStore reports whether the prompt pool is empty: either no rows at all
// (clipboard also disabled) or only the pinned clipboard row.
func (m Model) isEmptyStore() bool {
	switch len(m.state.Rows) {
	case 0:
		return true
	case 1:
		return m.state.Rows[0].PromptID == ""
	default:
		return false
	}
}

// clipboardRow returns the pinned clipboard row if one is present. The
// clipboard row is identified by an empty PromptID.
func clipboardRow(rows []Row) (Row, bool) {
	if len(rows) == 0 {
		return Row{}, false
	}
	if rows[0].PromptID == "" {
		return rows[0], true
	}
	return Row{}, false
}

// displayKey formats a keybind for the [key] column: letters render lowercase,
// digits and symbols render as declared. Keyless rows (Key == 0, which is
// how overflow prompts surface in search results) render as a space so the
// column stays fixed-width and no NUL byte reaches the terminal.
func displayKey(r rune) string {
	if r == 0 {
		return " "
	}
	if unicode.IsLetter(r) {
		return string(unicode.ToLower(r))
	}
	return string(r)
}
