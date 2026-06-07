package importtui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aurokin/tprompt/internal/searchindex"
)

// footerLines is the fixed chrome subtracted from terminal height to compute
// rowsPerFrame: a counter line (selected/overwrite/blocked) plus a confirm +
// key-hint line. The header is one title line, counted dynamically via
// headerLines so the viewport math stays exact if it ever wraps.
const footerLines = 2

// mode is the picker's input mode. modeList (the zero value) is the default
// checkbox list; modeSearch captures printable runes into the query for live
// fuzzy filtering. Two stages keep the rune-vs-action conflict clean: `a`/space
// are select-all/toggle in modeList but query text in modeSearch (AUR-530 D9).
type mode int

const (
	modeList mode = iota
	modeSearch
)

// Model is the bubbletea model for the import picker: a checkbox list of fresh
// items. selected is keyed by item id and starts fully checked (locked decision
// D8: fresh items pre-checked); it is independent of mode and query, so a
// selection survives entering, filtering in, and leaving search. cursor and
// scrollOffset index the VISIBLE slice (visible, recomputed by refilter on every
// query change) with the same clamp math as the board (copied; see package doc).
// index is the shared fuzzy core over all items; query is the active filter.
type Model struct {
	items    []Item
	index    *searchindex.Index[Item]
	selected map[string]bool
	mode     mode
	query    string
	visible  []Item

	cursor       int
	scrollOffset int
	width        int
	height       int
	result       Result
}

// NewModel seeds a Model from State, pre-checking by conflict kind: fresh
// (ConflictNone) items start selected (locked decision: fresh pre-checked);
// exact-target conflicts start unchecked (§34 skip-by-default — check to arm an
// overwrite) unless Armed (a CLI --overwrite refresh, pre-armed); cross-path
// duplicates start unchecked and are never selectable. It also builds the shared
// fuzzy index over the items (matching id, title, and tags) for `/`-search.
func NewModel(state State) Model {
	selected := make(map[string]bool, len(state.Items))
	for _, it := range state.Items {
		selected[it.ID] = it.Conflict == ConflictNone || it.Armed
	}
	m := Model{
		items:    state.Items,
		index:    searchindex.New(state.Items, itemFields, itemTieKey),
		selected: selected,
	}
	m.visible = m.computeVisible()
	return m
}

// itemFields adapts an Item to the search corpus: id + title + tags (the import
// picker has no separate description field, so Description stays empty).
func itemFields(it Item) searchindex.Fields {
	return searchindex.Fields{ID: it.ID, Title: it.Title, Tags: it.Tags}
}

// itemTieKey is the stable tiebreak for equal-score matches: the id is unique
// per row (it is the write target), so it totally orders the results.
func itemTieKey(it Item) string { return it.ID }

// computeVisible returns the rows to show for the current query. An empty query
// keeps the original snippet order (the default view is unchanged from AUR-528/
// 529); a non-empty query returns the fuzzy-ranked subset.
func (m Model) computeVisible() []Item {
	if m.query == "" {
		return m.items
	}
	matches := m.index.Query(m.query)
	out := make([]Item, 0, len(matches))
	for _, mt := range matches {
		out = append(out, mt.Item)
	}
	return out
}

// refilter recomputes the visible set after a query change and resets the cursor
// to the top (the ranked order changed, so preserving the index is meaningless).
//
// It also enforces the picker's overwrite-safety invariant: an ad-hoc per-item
// overwrite is only ever armed while its row is VISIBLE. A filter that hides an
// armed exact-target disarms it, so the global counter truthfully drops to "0
// overwrite" and confirm can never silently overwrite a prompt the user can no
// longer see. CLI --overwrite refreshes (Armed) are an authorized bulk opt-in, so
// they survive while hidden; only ad-hoc arms are scoped to visibility. This is
// refilter's choke point because it is the only place the visible set ever shrinks
// (NewModel starts with an empty query, so everything is visible).
func (m Model) refilter() Model {
	m.visible = m.computeVisible()
	visible := make(map[string]bool, len(m.visible))
	for _, it := range m.visible {
		visible[it.ID] = true
	}
	for _, it := range m.items {
		if !visible[it.ID] && it.Conflict == ConflictExactTarget && !it.Armed {
			m.selected[it.ID] = false
		}
	}
	m.cursor = 0
	m.scrollOffset = 0
	return m
}

// Result returns the Result captured when the Model issued tea.Quit. The
// Renderer reads this after bubbletea returns.
func (m Model) Result() Result { return m.result }

// Init satisfies tea.Model. No startup command.
func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.scrollOffset = clampScrollOffset(m.cursor, m.scrollOffset, len(m.visible), m.rowsPerFrame())
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeSearch {
		return m.updateSearchKey(msg)
	}
	return m.updateListKey(msg)
}

// updateListKey handles keys in the default checkbox list: navigation, toggle,
// select-all, confirm/cancel, and `/` to enter search.
func (m Model) updateListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Catch Ctrl+C explicitly before bubbletea's default SIGINT path so the
	// cancel Result is captured instead of surfacing as ErrProgramKilled.
	switch {
	case msg.Type == tea.KeyCtrlC:
		m.result = Result{Action: ActionCancel}
		return m, tea.Quit
	case msg.Type == tea.KeyEsc:
		// Esc backs out one level: clear an active committed filter first (so the
		// full list returns), and only cancel the picker when no filter is active.
		// This keeps Esc from silently cancelling when the user means "drop the
		// filter", and mirrors Esc's clear-the-filter meaning inside search.
		if m.query != "" {
			m.query = ""
			return m.refilter(), nil
		}
		m.result = Result{Action: ActionCancel}
		return m, tea.Quit
	case msg.Type == tea.KeyEnter:
		m.result = Result{Action: ActionConfirm, SelectedIDs: m.selectedIDs(), OverwriteIDs: m.overwriteIDs()}
		return m, tea.Quit
	case msg.Type == tea.KeyUp:
		return m.moveCursor(-1), nil
	case msg.Type == tea.KeyDown:
		return m.moveCursor(1), nil
	case msg.Type == tea.KeySpace:
		return m.toggleCurrent(), nil
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1:
		// Bubble Tea can emit a standalone space as tea.KeySpace or as a
		// tea.KeyRunes with Runes[0] == ' ', so route both to toggle.
		switch msg.Runes[0] {
		case 'k':
			return m.moveCursor(-1), nil
		case 'j':
			return m.moveCursor(1), nil
		case ' ':
			return m.toggleCurrent(), nil
		case 'a':
			return m.selectAll(), nil
		case '/':
			m.mode = modeSearch
			return m, nil
		}
	}
	return m, nil
}

// updateSearchKey handles keys while typing a filter. Every printable rune
// (including a/j/k/space) appends to the query so any text is searchable; the
// list actions are deliberately unreachable here (AUR-530 D9). Ctrl+C still
// cancels (never trap the user). Enter commits the filter and returns to the
// list with the query kept; Esc clears the query and returns to the full list —
// it does NOT cancel the picker (only Esc in modeList cancels).
func (m Model) updateSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.result = Result{Action: ActionCancel}
		return m, tea.Quit
	case tea.KeyEsc:
		m.query = ""
		m.mode = modeList
		return m.refilter(), nil
	case tea.KeyEnter:
		m.mode = modeList
		return m, nil
	case tea.KeyUp:
		return m.moveCursor(-1), nil
	case tea.KeyDown:
		return m.moveCursor(1), nil
	case tea.KeyBackspace:
		return m.popQueryRune(), nil
	case tea.KeySpace:
		// Bubble Tea may deliver a bare space as KeySpace; append it so
		// multi-word queries work.
		return m.appendQuery(" "), nil
	case tea.KeyRunes:
		return m.appendQuery(string(msg.Runes)), nil
	}
	return m, nil
}

// appendQuery adds text to the query and refilters.
func (m Model) appendQuery(s string) Model {
	m.query += s
	return m.refilter()
}

// popQueryRune removes the last rune from the query and refilters; a no-op on an
// empty query.
func (m Model) popQueryRune() Model {
	if m.query == "" {
		return m
	}
	runes := []rune(m.query)
	m.query = string(runes[:len(runes)-1])
	return m.refilter()
}

// moveCursor shifts the cursor by delta (±1) within the visible set's bounds and
// re-clamps the scroll offset so the cursor stays visible.
func (m Model) moveCursor(delta int) Model {
	next := m.cursor + delta
	if next < 0 || next >= len(m.visible) {
		return m
	}
	m.cursor = next
	m.scrollOffset = clampScrollOffset(m.cursor, m.scrollOffset, len(m.visible), m.rowsPerFrame())
	return m
}

// toggleCurrent flips the checkbox under the cursor (over the visible set). A
// cross-path duplicate is non-selectable (the importer cannot create it), so
// toggling it is a no-op.
func (m Model) toggleCurrent() Model {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return m
	}
	it := m.visible[m.cursor]
	if it.Conflict == ConflictCrossPath {
		return m
	}
	m.selected[it.ID] = !m.selected[it.ID]
	return m
}

// selectAll resets the VISIBLE rows to NewModel's initial safe default: every
// fresh item checked, every CLI-authorized refresh (Armed) kept armed, and every
// other conflict at skip-by-default (ad-hoc per-row overwrite arms cleared,
// cross-path unselected). Scoping the reset to the visible set means `a` "applies
// the safe default to the rows I'm looking at" — off-filter fresh selections are
// left untouched (so a filter→select→filter→select batch builds up a union), and
// with no filter (visible == all) it is byte-identical to the unfiltered
// select-all.
//
// `a` does not need to reach hidden rows to stay safe: refilter already guarantees
// no ad-hoc overwrite is armed while hidden (so there is no off-filter destructive
// arm for `a` to miss), and hidden fresh selections are non-destructive.
func (m Model) selectAll() Model {
	for _, it := range m.visible {
		m.selected[it.ID] = it.Conflict == ConflictNone || it.Armed
	}
	return m
}

// selectedIDs returns the checked item ids in item order — the write set the
// writer replays, so the confirmed set matches the rows the user saw.
func (m Model) selectedIDs() []string {
	ids := make([]string, 0, len(m.items))
	for _, it := range m.items {
		if m.selected[it.ID] {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

// overwriteIDs returns the checked exact-target ids in item order: the per-item
// overwrite set (a subset of selectedIDs) the caller arms on the replayed write.
func (m Model) overwriteIDs() []string {
	ids := make([]string, 0, len(m.items))
	for _, it := range m.items {
		if it.Conflict == ConflictExactTarget && m.selected[it.ID] {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

// blockedCount is how many rows are cross-path duplicates the importer cannot
// create (the footer's K) — shown for context but never written.
func (m Model) blockedCount() int {
	n := 0
	for _, it := range m.items {
		if it.Conflict == ConflictCrossPath {
			n++
		}
	}
	return n
}

// rowsPerFrame returns how many row lines fit in the viewport. Returns 0
// pre-WindowSizeMsg (height == 0) or when chrome exceeds the window; View
// treats 0 as "render all rows" so headless tests still see everything.
// (Copied from internal/tui; see package doc.) The "chrome exceeds the window"
// case (a 1-2 line terminal renders the whole list rather than clamping) is the
// board's intentional, documented behavior, deliberately preserved here for
// parity; the alt-screen picker always receives the real terminal height, so it
// only arises in a pathological terminal.
func (m Model) rowsPerFrame() int {
	rpf := m.height - m.headerLines() - footerLines
	if rpf <= 0 {
		return 0
	}
	return rpf
}

// headerLines is the rendered height of the title header, counted so
// rowsPerFrame reserves exactly the lines View prepends. (Copied from
// internal/tui's banner accounting; see package doc.)
func (m Model) headerLines() int {
	return lipgloss.Height(m.renderHeader())
}

// viewWidth is the effective render width: the terminal width once known, or an
// 80-column default before the first WindowSizeMsg. (Copied from internal/tui.)
func (m Model) viewWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

// clampScrollOffset returns the scrollOffset that keeps cursor visible in the
// window [offset, offset+rpf) and prevents overscroll past the list tail.
// rpf <= 0 (pre-WindowSizeMsg) or an empty row set collapse to offset 0.
// (Copied verbatim from internal/tui; see package doc.)
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

// visibleRowRange returns [start, end) of the visible (filtered) items that fit
// in the viewport. Pre-WindowSizeMsg (rpf == 0) or when every row fits, returns
// the full range. (Copied from internal/tui.)
func (m Model) visibleRowRange() (int, int) {
	rows := len(m.visible)
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

// View renders header + visible rows + footer. Structure mirrors the board:
// header has no trailing newline, each row carries its own newline, and the
// footer (its own lines joined by, but not ending in, a newline) closes without
// one — so total lines == headerLines + rowsPerFrame + footerLines.
func (m Model) View() string {
	width := m.viewWidth()
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	idWidth := maxIDWidth(m.visible)
	start, end := m.visibleRowRange()
	for i := start; i < end; i++ {
		line := renderRow(m.visible[i], m.selected[m.visible[i].ID], idWidth, width)
		if i == m.cursor {
			line = selectedStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString(m.footer())
	return sb.String()
}

// renderHeader is the one title line. In modeList it reports the global
// selection count, plus an explicit "filtered" badge when a committed filter is
// still narrowing the list (so a list with hidden rows is never indistinguishable
// from the full list); in modeSearch it becomes the live `/<query>` input line.
// Both render as exactly one line (no newline, width-truncated) so rowsPerFrame's
// chrome accounting is identical in either mode.
func (m Model) renderHeader() string {
	if m.mode == modeSearch {
		return headerStyle.Render(truncateToWidth("/"+m.query, m.viewWidth()))
	}
	header := fmt.Sprintf("Select snippets to import (%d/%d)", len(m.selectedIDs()), len(m.items))
	if m.query != "" {
		header += fmt.Sprintf(" — filtered %q, %d shown", m.query, len(m.visible))
	}
	return headerStyle.Render(truncateToWidth(header, m.viewWidth()))
}

// footer renders two lines: a faint counter (selected / armed-overwrite /
// blocked) and a confirm + key-hint line. The counter always reports GLOBAL
// selection (every item, not the filtered view), so the confirm count reflects
// the true write set regardless of an active filter. The hint line is
// mode-specific. Each line is plain-text-truncated to width before styling
// (mirrors renderHeader) so the no-wrap, one-line-per-row viewport math holds;
// the two are joined by — but do not end in — a newline.
func (m Model) footer() string {
	w := m.viewWidth()
	n := len(m.selectedIDs())
	counter := fmt.Sprintf("%d selected · %d overwrite · %d blocked", n, len(m.overwriteIDs()), m.blockedCount())
	var hint string
	switch {
	case m.mode == modeSearch:
		hint = fmt.Sprintf("%d matches · enter filter · esc clear · type to search", len(m.visible))
	case m.query != "":
		// A committed filter is active: surface `/` to refine it and that esc now
		// clears the filter rather than cancelling.
		hint = fmt.Sprintf("write %d prompts? enter confirm · space toggle · a all · / edit · esc clear filter", n)
	default:
		hint = fmt.Sprintf("write %d prompts? enter confirm · space toggle · a all · / search · esc cancel", n)
	}
	return headerStyle.Render(truncateToWidth(counter, w)) + "\n" + truncateToWidth(hint, w)
}

var (
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	headerStyle   = lipgloss.NewStyle().Faint(true)
)

// renderRow formats one row: "<glyph> id  label". Both the id and label columns
// are width-bounded so the row never exceeds the terminal width: an over-long id
// is truncated (not wrapped, which would push the row onto a second physical line
// and desync the one-line-per-row viewport math). The id column wins the available
// space over the label, since the id is what identifies the row. A cross-path row
// shows the conflicting path in place of the phrase (the actionable reason it is
// blocked); other rows show the sanitized snippet phrase.
func renderRow(item Item, checked bool, idWidth, width int) string {
	const boxCol = 3 // "[x]"
	const padding = 2
	maxID := width - boxCol - padding*2
	if maxID < 0 {
		maxID = 0
	}
	if idWidth > maxID {
		idWidth = maxID
	}
	labelCol := width - boxCol - idWidth - padding*2
	if labelCol < 0 {
		labelCol = 0
	}
	id := padRight(truncateToWidth(item.ID, idWidth), idWidth)
	label := sanitizeLabel(item.Title)
	if item.Conflict == ConflictCrossPath {
		label = "also at " + sanitizeLabel(item.Blocker)
	}
	label = truncateToWidth(label, labelCol)
	// Cap the composed line too: the fixed glyph + column gaps are 7 cells, so a
	// terminal narrower than that would otherwise wrap the row and desync the
	// one-line-per-row viewport math. Truncating keeps the invariant at any width.
	return truncateToWidth(fmt.Sprintf("%s  %s  %s", glyphFor(item, checked), id, label), width)
}

// glyphFor returns the 3-cell status box for a row: cross-path is always [!]
// (non-selectable); an exact-target is [=] until armed, then [x] (it will be
// overwritten); a fresh row is [ ]/[x].
func glyphFor(item Item, checked bool) string {
	switch item.Conflict {
	case ConflictCrossPath:
		return "[!]"
	case ConflictExactTarget:
		if checked {
			return "[x]"
		}
		return "[=]"
	default:
		if checked {
			return "[x]"
		}
		return "[ ]"
	}
}

// sanitizeLabel makes a snippet phrase safe for a single-line row: every control
// rune — newlines and tabs (which would spill the row onto extra physical lines
// and desync the viewport) and the ESC that begins ANSI sequences (which could
// recolor or manipulate the terminal) — is replaced with a space. Wispr phrases
// are stored verbatim, so the picker must not trust them as display-ready.
func sanitizeLabel(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

func maxIDWidth(items []Item) int {
	max := 0
	for _, it := range items {
		if w := lipgloss.Width(it.ID); w > max {
			max = w
		}
	}
	return max
}

// padRight / truncateToWidth are copied from internal/tui (rune-width aware via
// lipgloss.Width so wide-rune titles align and truncate correctly).
func padRight(s string, width int) string {
	pad := width - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

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
