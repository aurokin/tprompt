package importtui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// footerLines is the fixed chrome subtracted from terminal height to compute
// rowsPerFrame: a single key-hint line. The header is one title line, counted
// dynamically via headerLines so the viewport math stays exact if it ever wraps.
const footerLines = 1

// Model is the bubbletea model for the import picker: a checkbox list of fresh
// items. selected is keyed by item id and starts fully checked (locked decision
// D8: fresh items pre-checked). cursor/scrollOffset drive the viewport with the
// same clamp math as the board (copied; see package doc).
type Model struct {
	items        []Item
	selected     map[string]bool
	cursor       int
	scrollOffset int
	width        int
	height       int
	result       Result
}

// NewModel seeds a Model from State with every item pre-checked.
func NewModel(state State) Model {
	selected := make(map[string]bool, len(state.Items))
	for _, it := range state.Items {
		selected[it.ID] = true
	}
	return Model{items: state.Items, selected: selected}
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
		m.scrollOffset = clampScrollOffset(m.cursor, m.scrollOffset, len(m.items), m.rowsPerFrame())
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Catch Ctrl+C explicitly before bubbletea's default SIGINT path so the
	// cancel Result is captured instead of surfacing as ErrProgramKilled.
	switch {
	case msg.Type == tea.KeyCtrlC, msg.Type == tea.KeyEsc:
		m.result = Result{Action: ActionCancel}
		return m, tea.Quit
	case msg.Type == tea.KeyEnter:
		m.result = Result{Action: ActionConfirm, SelectedIDs: m.selectedIDs()}
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
		}
	}
	return m, nil
}

// moveCursor shifts the cursor by delta (±1) within bounds and re-clamps the
// scroll offset so the cursor stays visible.
func (m Model) moveCursor(delta int) Model {
	next := m.cursor + delta
	if next < 0 || next >= len(m.items) {
		return m
	}
	m.cursor = next
	m.scrollOffset = clampScrollOffset(m.cursor, m.scrollOffset, len(m.items), m.rowsPerFrame())
	return m
}

// toggleCurrent flips the checkbox under the cursor.
func (m Model) toggleCurrent() Model {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return m
	}
	id := m.items[m.cursor].ID
	m.selected[id] = !m.selected[id]
	return m
}

// selectAll re-checks every item (the inverse of deselecting via Space).
func (m Model) selectAll() Model {
	for _, it := range m.items {
		m.selected[it.ID] = true
	}
	return m
}

// selectedIDs returns the checked item ids in item order — the order the writer
// replays, so the confirmed set matches the rows the user saw.
func (m Model) selectedIDs() []string {
	ids := make([]string, 0, len(m.items))
	for _, it := range m.items {
		if m.selected[it.ID] {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

// rowsPerFrame returns how many row lines fit in the viewport. Returns 0
// pre-WindowSizeMsg (height == 0) or when chrome exceeds the window; View
// treats 0 as "render all rows" so headless tests still see everything.
// (Copied from internal/tui; see package doc.)
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

// visibleRowRange returns [start, end) of items that fit in the viewport.
// Pre-WindowSizeMsg (rpf == 0) or when every row fits, returns the full range.
// (Copied from internal/tui.)
func (m Model) visibleRowRange() (int, int) {
	rows := len(m.items)
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
// footer closes without one — so total lines == headerLines + rowsPerFrame + 1.
func (m Model) View() string {
	width := m.viewWidth()
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	idWidth := maxIDWidth(m.items)
	start, end := m.visibleRowRange()
	for i := start; i < end; i++ {
		line := renderRow(m.items[i], m.selected[m.items[i].ID], idWidth, width)
		if i == m.cursor {
			line = selectedStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString(m.footer())
	return sb.String()
}

func (m Model) renderHeader() string {
	header := fmt.Sprintf("Select snippets to import (%d/%d)", len(m.selectedIDs()), len(m.items))
	return headerStyle.Render(truncateToWidth(header, m.viewWidth()))
}

func (m Model) footer() string {
	return truncateToWidth("space toggle · a all · enter import · esc cancel", m.viewWidth())
}

var (
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	headerStyle   = lipgloss.NewStyle().Faint(true)
)

// renderRow formats one checkbox row: "[x] id  title". Both the id and title
// columns are width-bounded so the row never exceeds the terminal width: an
// over-long id is truncated (not wrapped, which would push the row onto a second
// physical line and desync the one-line-per-row viewport math). The id column
// wins the available space over the title, since the id is what identifies the
// row.
func renderRow(item Item, checked bool, idWidth, width int) string {
	box := "[ ]"
	if checked {
		box = "[x]"
	}
	const boxCol = 3 // "[x]"
	const padding = 2
	maxID := width - boxCol - padding*2
	if maxID < 0 {
		maxID = 0
	}
	if idWidth > maxID {
		idWidth = maxID
	}
	titleCol := width - boxCol - idWidth - padding*2
	if titleCol < 0 {
		titleCol = 0
	}
	id := padRight(truncateToWidth(item.ID, idWidth), idWidth)
	title := truncateToWidth(item.Title, titleCol)
	// Cap the composed line too: the fixed checkbox + column gaps are 7 cells, so
	// a terminal narrower than that would otherwise wrap the row and desync the
	// one-line-per-row viewport math. Truncating keeps the invariant at any width.
	return truncateToWidth(fmt.Sprintf("%s  %s  %s", box, id, title), width)
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
