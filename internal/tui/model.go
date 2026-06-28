package tui

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aurokin/tprompt/internal/clipboard"
	"github.com/aurokin/tprompt/internal/prompttmpl"
	"github.com/aurokin/tprompt/internal/sanitize"
	"github.com/aurokin/tprompt/internal/store"
	layout "github.com/aurokin/tprompt/internal/tuilayout"
)

// Submitter is the subset of internal/submitter.Submitter the Model depends
// on. Declared locally so the tui package does not import submitter (which
// imports tui for Result).
type Submitter interface {
	Submit(Result) error
}

type scopedStore interface {
	ResolveScoped(id, scope string) (store.Prompt, error)
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
	modeTemplate
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

	// Template input-mode state.
	templatePromptID string
	templateScope    string
	templateBody     string
	templateVars     []prompttmpl.Variable
	templateValues   map[string]string
	templateIndex    int
	templateInput    string
}

// footerLines is the fixed chrome subtracted from terminal height to compute
// rowsPerFrame: a single hint/error line composed by m.footer(). The header is
// dynamic (see m.headerLines) because the optional direct-mode banner occupies
// lines only when State.Banner is set.
const footerLines = 1

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
	return layout.RowsPerFrame(m.height, m.headerLines(), footerLines)
}

// headerLines is the height of the rendered header chrome. It is zero in the
// common case (no banner) so existing viewport math is unchanged; when
// State.Banner is set it equals the banner block's wrapped height so
// rowsPerFrame reserves exactly the lines View prepends, keeping the bottom
// board row on screen.
func (m Model) headerLines() int {
	if m.state.Banner == "" {
		return 0
	}
	return lipgloss.Height(m.renderBanner())
}

// renderBanner wraps the direct-mode banner to the current view width. A long
// banner wraps across lines; headerLines counts those lines so the viewport
// accounting stays exact at any width.
func (m Model) renderBanner() string {
	return bannerStyle.Width(m.viewWidth()).Render(m.state.Banner)
}

// viewWidth is the effective render width: the terminal width once known, or an
// 80-column default before the first WindowSizeMsg (so headless tests render).
func (m Model) viewWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

// Update handles inbound messages. Keypress handling forks by mode so search
// can layer in without disturbing board handling.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.scrollOffset = layout.ClampScrollOffset(m.cursor, m.scrollOffset, len(m.state.Rows), m.rowsPerFrame())
		m.searchScrollOffset = layout.ClampScrollOffset(m.searchCursor, m.searchScrollOffset, len(m.results), m.rowsPerFrame())
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
		case modeTemplate:
			return m.updateTemplate(msg)
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
			// any error the handoff path returns, silently losing the outcome.
			// Wait for submitResultMsg to decide the exit code.
			return m, nil
		}
		m.inlineError = ""
		m.result = Result{Action: ActionCancel}
		return m, tea.Quit
	case msg.Type == tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			m.scrollOffset = layout.ClampScrollOffset(m.cursor, m.scrollOffset, len(m.state.Rows), m.rowsPerFrame())
		}
		// ↑/↓ preserve inlineError per §19.
		return m, nil
	case msg.Type == tea.KeyDown:
		if m.cursor < len(m.state.Rows)-1 {
			m.cursor++
			m.scrollOffset = layout.ClampScrollOffset(m.cursor, m.scrollOffset, len(m.state.Rows), m.rowsPerFrame())
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

	// Select fires after the switch so a remapped binding (e.g. Reserved.Select
	// = a printable rune) still wins over the rune-based prompt-key dispatch
	// below. The default Enter binding lands here as well.
	if matchesReserved(msg, m.state.Reserved.Select) {
		return m.boardSelectHighlighted()
	}

	return m.tryPromptSelect(msg)
}

// boardSelectHighlighted resolves the row at the board cursor. Mirrors
// searchSelectHighlighted: both gate on pending submit/clipboard and route
// the pinned clipboard row through selectClipboard.
func (m Model) boardSelectHighlighted() (tea.Model, tea.Cmd) {
	if m.pendingSubmit || m.pendingClipboard {
		return m, nil
	}
	if m.cursor < 0 || m.cursor >= len(m.state.Rows) {
		return m, nil
	}
	row := m.state.Rows[m.cursor]
	if row.PromptID == "" {
		return m.selectClipboard()
	}
	return m.selectPrompt(row)
}

// tryPromptSelect matches a single-rune printable keypress against the
// assigned row keys and dispatches prompt selection. Returns the model and
// any cmd; unassigned keypresses are no-ops that preserve inlineError.
func (m Model) tryPromptSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Gate prompt selection while a submit is in flight. Without this a slow
	// Submitter combined with key repeat could enqueue multiple submitCmds
	// and produce duplicate handoff submissions from one interaction. Same
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
		return m.selectPrompt(row)
	}
	return m, nil
}

// selectPrompt resolves the prompt body, enforces MaxPasteBytes inline, and
// fires the submit cmd when the body is within limits. Store or resolve
// failures propagate through submitErr so the Renderer wrapper surfaces them.
func (m Model) selectPrompt(row Row) (tea.Model, tea.Cmd) {
	id := row.PromptID
	scope := row.Scope
	var (
		prompt store.Prompt
		err    error
	)
	if scope != "" {
		if scoped, ok := m.deps.Store.(scopedStore); ok {
			prompt, err = scoped.ResolveScoped(id, scope)
		} else {
			prompt, err = m.deps.Store.Resolve(id)
		}
	} else {
		prompt, err = m.deps.Store.Resolve(id)
	}
	if err != nil {
		// The pre-flight validated the store, but a prompt file can still be
		// removed between List and Resolve. Bubble as a submit failure so the
		// exit-code mapping in runTUI handles it like any other store error.
		m.result = Result{Action: ActionPrompt, PromptID: id, Scope: scope}
		m.submitErr = err
		return m, tea.Quit
	}
	if len(prompt.Variables) > 0 {
		return m.enterTemplateInput(row, prompt), nil
	}
	if m.deps.MaxPasteBytes > 0 && int64(len(prompt.Body)) > m.deps.MaxPasteBytes {
		m.inlineError = "prompt body exceeds max_paste_bytes — choose another prompt"
		return m, nil
	}
	m.inlineError = ""
	m.pendingSubmit = true
	return m, submitCmd(m.deps.Submitter, Result{Action: ActionPrompt, PromptID: id, Scope: scope})
}

func (m Model) enterTemplateInput(row Row, prompt store.Prompt) Model {
	values := prompttmpl.Defaults(prompt.Variables)
	input := ""
	if len(prompt.Variables) > 0 {
		input = values[prompt.Variables[0].Name]
	}
	m.mode = modeTemplate
	m.inlineError = ""
	m.templatePromptID = row.PromptID
	m.templateScope = row.Scope
	m.templateBody = prompt.Body
	m.templateVars = append([]prompttmpl.Variable(nil), prompt.Variables...)
	m.templateValues = values
	m.templateIndex = 0
	m.templateInput = input
	return m
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

func (m Model) updateTemplate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		if m.pendingSubmit {
			return m, nil
		}
		m.inlineError = ""
		m.result = Result{Action: ActionCancel}
		return m, tea.Quit
	}
	if msg.Type == tea.KeyEsc {
		if m.pendingSubmit {
			return m, nil
		}
		return m.exitTemplateInput(), nil
	}
	if m.pendingSubmit {
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEnter:
		return m.acceptTemplateInput()
	case tea.KeyShiftTab:
		return m.previousTemplateInput(), nil
	case tea.KeyBackspace:
		runes := []rune(m.templateInput)
		if len(runes) > 0 {
			m.templateInput = string(runes[:len(runes)-1])
			m.inlineError = ""
		}
		return m, nil
	case tea.KeySpace:
		if !msg.Alt {
			m.templateInput += " "
			m.inlineError = ""
		}
		return m, nil
	case tea.KeyRunes:
		if len(msg.Runes) > 0 && !msg.Alt {
			m.templateInput += string(msg.Runes)
			m.inlineError = ""
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) exitTemplateInput() Model {
	m.mode = modeBoard
	m.inlineError = ""
	m.templatePromptID = ""
	m.templateScope = ""
	m.templateBody = ""
	m.templateVars = nil
	m.templateValues = nil
	m.templateIndex = 0
	m.templateInput = ""
	return m
}

func (m Model) previousTemplateInput() Model {
	if m.templateIndex <= 0 || m.templateIndex >= len(m.templateVars) {
		return m
	}
	current := m.templateVars[m.templateIndex]
	m.templateValues[current.Name] = m.templateInput
	m.templateIndex--
	previous := m.templateVars[m.templateIndex]
	m.templateInput = m.templateValues[previous.Name]
	m.inlineError = ""
	return m
}

func (m Model) acceptTemplateInput() (tea.Model, tea.Cmd) {
	if m.templateIndex < 0 || m.templateIndex >= len(m.templateVars) {
		m.submitErr = fmt.Errorf("tui: template input index out of range")
		m.result = Result{Action: ActionPrompt, PromptID: m.templatePromptID, Scope: m.templateScope}
		return m, tea.Quit
	}
	current := m.templateVars[m.templateIndex]
	if current.Required && strings.TrimSpace(m.templateInput) == "" {
		m.inlineError = fmt.Sprintf("%s is required", templateVarLabel(current))
		return m, nil
	}
	m.templateValues[current.Name] = m.templateInput
	if m.templateIndex < len(m.templateVars)-1 {
		m.templateIndex++
		next := m.templateVars[m.templateIndex]
		m.templateInput = m.templateValues[next.Name]
		m.inlineError = ""
		return m, nil
	}
	return m.submitTemplatePrompt()
}

func (m Model) submitTemplatePrompt() (tea.Model, tea.Cmd) {
	rendered, err := prompttmpl.Render(m.templateBody, m.templateVars, m.templateValues)
	if err != nil {
		m.submitErr = err
		m.result = Result{Action: ActionPrompt, PromptID: m.templatePromptID, Scope: m.templateScope}
		return m, tea.Quit
	}
	if m.deps.MaxPasteBytes > 0 && int64(len(rendered)) > m.deps.MaxPasteBytes {
		m.inlineError = "prompt body exceeds max_paste_bytes — edit a value or press Esc"
		return m, nil
	}
	m.inlineError = ""
	m.pendingSubmit = true
	return m, submitCmd(m.deps.Submitter, Result{
		Action:         ActionPrompt,
		PromptID:       m.templatePromptID,
		Scope:          m.templateScope,
		TemplateValues: cloneTemplateValues(m.templateValues),
	})
}

func cloneTemplateValues(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for name, value := range values {
		cloned[name] = value
	}
	return cloned
}

func templateVarLabel(v prompttmpl.Variable) string {
	if v.Label != "" {
		return v.Label
	}
	return v.Name
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
	m.searchScrollOffset = layout.ClampScrollOffset(m.searchCursor, m.searchScrollOffset, len(m.results), m.rowsPerFrame())
	return m
}

// updateSearch routes search-mode keypresses. Reserved semantics (docs
// §Search mode): Esc returns to board (never quits); Ctrl+C cancels; Enter
// selects; ↑/↓ navigate; Backspace pops one rune; any other single-rune
// keypress appends to the query.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Esc and literal Ctrl+C are not user-remappable in search mode: Esc
	// always exits search, and literal Ctrl+C always cancels (a Cancel
	// binding remapped to a printable rune must remain searchable).
	switch msg.Type {
	case tea.KeyEsc:
		return m.searchExitToBoard()
	case tea.KeyCtrlC:
		return m.searchCancel()
	}

	// A remapped Select binding wins over default rune/space handling so a
	// printable or Space-symbolic Select still selects in search mode rather
	// than appending to the query.
	if matchesReserved(msg, m.state.Reserved.Select) {
		return m.searchSelectHighlighted()
	}

	switch msg.Type {
	case tea.KeyUp:
		return m.searchMoveCursor(-1), nil
	case tea.KeyDown:
		return m.searchMoveCursor(+1), nil
	case tea.KeyBackspace:
		return m.searchBackspace(), nil
	case tea.KeySpace:
		if !msg.Alt {
			return m.searchAppendRune(' '), nil
		}
	case tea.KeyRunes:
		if len(msg.Runes) == 1 && !msg.Alt {
			return m.searchAppendRune(msg.Runes[0]), nil
		}
	}

	return m, nil
}

// searchExitToBoard handles Esc in search mode. Esc always exits search mode
// back to the board — even if Cancel is bound to Esc at the config level.
// Per ticket: "Esc does NOT exit the TUI in search mode".
func (m Model) searchExitToBoard() (tea.Model, tea.Cmd) {
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

// searchCancel handles Ctrl+C in search mode: cancel + Quit. Literal Ctrl+C
// only — remapped printable Cancel bindings go into the search query like
// any other printable rune so the user can actually search for that character.
func (m Model) searchCancel() (tea.Model, tea.Cmd) {
	if m.pendingSubmit {
		return m, nil
	}
	m.inlineError = ""
	m.result = Result{Action: ActionCancel}
	return m, tea.Quit
}

// searchSelectHighlighted resolves the highlighted search result.
func (m Model) searchSelectHighlighted() (tea.Model, tea.Cmd) {
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
	return m.selectPrompt(row)
}

// searchMoveCursor advances the search cursor by delta (±1) within bounds.
// Navigation preserves inlineError (§19).
func (m Model) searchMoveCursor(delta int) Model {
	next := m.searchCursor + delta
	if next < 0 || next >= len(m.results) {
		return m
	}
	m.searchCursor = next
	m.searchScrollOffset = layout.ClampScrollOffset(m.searchCursor, m.searchScrollOffset, len(m.results), m.rowsPerFrame())
	m.highlightedPromptID = m.results[m.searchCursor].Row.PromptID
	return m
}

// searchBackspace pops one rune from the query and re-filters. A query edit
// is a real action, so inlineError clears.
func (m Model) searchBackspace() Model {
	if len(m.query) == 0 {
		return m
	}
	runes := []rune(m.query)
	m.query = string(runes[:len(runes)-1])
	m = m.refilter()
	m.inlineError = ""
	return m
}

// searchAppendRune appends a single rune to the query and re-filters. Bubble
// Tea can emit a standalone space as either tea.KeySpace or a tea.KeyRunes
// with Runes[0] == ' ', so the dispatcher routes both forms here — otherwise
// multi-word queries like "code review" would drop the space.
func (m Model) searchAppendRune(r rune) Model {
	m.query += string(r)
	m = m.refilter()
	m.inlineError = ""
	return m
}

// View renders the optional header banner, the mode-appropriate body, and the
// footer hint. The banner is prepended in View (not the per-mode helpers) so it
// shows in both board and search modes and so its height matches what
// headerLines reserves in rowsPerFrame.
func (m Model) View() string {
	width := m.viewWidth()
	var body string
	switch m.mode {
	case modeSearch:
		body = m.viewSearch(width)
	case modeTemplate:
		body = m.viewTemplate(width)
	default:
		body = m.viewBoard(width)
	}
	if m.state.Banner == "" {
		return body
	}
	return m.renderBanner() + "\n" + body
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

func (m Model) viewTemplate(width int) string {
	if len(m.templateVars) == 0 || m.templateIndex >= len(m.templateVars) {
		return m.footer()
	}
	current := m.templateVars[m.templateIndex]
	label := templateVarLabel(current)
	if current.Required {
		label += " *"
	}

	lines := []string{
		fmt.Sprintf("%s  [%d/%d]", m.templatePromptID, m.templateIndex+1, len(m.templateVars)),
		layout.TruncateToWidth(label, width),
	}
	if current.Description != "" {
		lines = append(lines, layout.TruncateToWidth(current.Description, width))
	}
	lines = append(lines, "> "+layout.TruncateToWidth(templateInputDisplay(m.templateInput), max(0, width-2)))
	return strings.Join(lines, "\n") + "\n" + m.footer()
}

func templateInputDisplay(value string) string {
	return string(sanitize.StripAll([]byte(value)))
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
	return layout.VisibleRange(m.searchScrollOffset, len(m.results), m.rowsPerFrame())
}

// visibleRowRange returns [start, end) of m.state.Rows that fit in the current
// viewport. Pre-WindowSizeMsg (rpf == 0) or when every row fits, it returns
// the full range so the render-all fallback satisfies pre-WindowSize tests
// and avoids division-by-zero paths.
func (m Model) visibleRowRange() (int, int) {
	return layout.VisibleRange(m.scrollOffset, len(m.state.Rows), m.rowsPerFrame())
}

var selectedStyle = lipgloss.NewStyle().Reverse(true)

// bannerStyle renders the direct-mode header banner faint so it reads as a
// nudge rather than a selectable board row.
var bannerStyle = lipgloss.NewStyle().Faint(true)

func renderRow(row Row, idWidth, descWidth int) string {
	key := "[" + displayKey(row.Key) + "]"
	id := layout.PadRight(row.DisplayName(), idWidth)
	desc := layout.TruncateToWidth(row.DisplayDescription(), descWidth)
	return fmt.Sprintf("%s  %s  %s", key, id, desc)
}

func maxIDWidth(rows []Row) int {
	max := 0
	for _, r := range rows {
		if w := lipgloss.Width(r.DisplayName()); w > max {
			max = w
		}
	}
	return max
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
	switch m.mode {
	case modeSearch:
		return m.searchFooterHints()
	case modeTemplate:
		return m.templateFooterHints()
	}
	if m.isEmptyStore() {
		return m.emptyStoreFooter()
	}
	var parts []string
	if search := m.boardSearchHint(); search != "" {
		parts = append(parts, search)
	}
	if sel := footerHint(m.state.Reserved.Select, "select"); sel != "" {
		parts = append(parts, sel)
	}
	if cancel := footerHint(m.state.Reserved.Cancel, "cancel"); cancel != "" {
		parts = append(parts, cancel)
	}
	hints := strings.Join(parts, "  ")
	return m.withKeyLegend(hints)
}

func (m Model) templateFooterHints() string {
	action := "next"
	if m.templateIndex == len(m.templateVars)-1 {
		action = "submit"
	}
	parts := []string{fmt.Sprintf("[Enter %s]", action)}
	if m.templateIndex > 0 {
		parts = append(parts, "[Shift+Tab prev]")
	}
	parts = append(parts, "[Esc back]", "[Ctrl+C cancel]")
	return strings.Join(parts, "  ")
}

// withKeyLegend prepends a legend telling users each board row's bracketed
// [key] (e.g. `[c]`) is pressable — the primary, otherwise-undiscoverable
// selection mechanism (AUR-449). The legend is only added when the whole
// footer line still fits the current width: rowsPerFrame reserves exactly one
// footer line (footerLines = 1), so a wrapped footer would push the bottom
// board row out of view. Under width pressure (narrow terminal, large overflow
// `(N more)` suffix, or a long inline error) the legend is dropped — never the
// functional hints.
func (m Model) withKeyLegend(hints string) string {
	const legend = "press a row's [key] to select"
	width := m.width
	if width <= 0 {
		width = 80
	}
	// Model the exact rendered footer width: footer() prepends the inline
	// error with a two-space separator when present, and the legend is joined
	// to the hints with two spaces only when hints are non-empty.
	full := lipgloss.Width(legend)
	if hints != "" {
		full += 2 + lipgloss.Width(hints)
	}
	if m.inlineError != "" {
		full += lipgloss.Width(m.inlineError) + 2
	}
	if full > width {
		return hints
	}
	if hints == "" {
		return legend
	}
	return legend + "  " + hints
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
