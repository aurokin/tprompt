package tui

import (
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

// mode distinguishes board rendering from search. Only modeBoard is exercised
// in the tracer bullet; modeSearch is carried so AUR-26 can plug in without
// reshaping the Model.
type mode int

const (
	modeBoard mode = iota
	modeSearch
)

// Model is the single bubbletea model for the TUI.
type Model struct {
	state  State
	deps   ModelDeps
	mode   mode
	cursor int
	width  int
	height int
	result Result
}

// NewModel constructs a Model seeded with the rendered state and deps.
func NewModel(state State, deps ModelDeps) Model {
	return Model{state: state, deps: deps, mode: modeBoard}
}

// Result returns the Result captured at the moment the Model issued tea.Quit.
// The Renderer wrapper reads this after bubbletea returns.
func (m Model) Result() Result { return m.result }

// Init satisfies tea.Model. No startup command yet.
func (m Model) Init() tea.Cmd { return nil }

// Update handles inbound messages. Keypress handling forks by mode so later
// slices can add modeSearch without disturbing board handling.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case modeBoard:
			return m.updateBoard(msg)
		case modeSearch:
			return m, nil
		}
	}
	return m, nil
}

func (m Model) updateBoard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Catch Ctrl+C explicitly before bubbletea's default SIGINT path so the
	// cancel Result is captured instead of surfacing as ErrProgramKilled.
	switch {
	case msg.Type == tea.KeyCtrlC, matchesReserved(msg, m.state.Reserved.Cancel):
		m.result = Result{Action: ActionCancel}
		return m, tea.Quit
	case msg.Type == tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case msg.Type == tea.KeyDown:
		if m.cursor < len(m.state.Rows)-1 {
			m.cursor++
		}
		return m, nil
	}
	return m, nil
}

// View renders the three-column board and footer hint.
func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = 80
	}

	var sb strings.Builder
	idWidth := maxIDWidth(m.state.Rows)
	const keyCol = 3 // "[X]"
	const padding = 2
	descCol := width - keyCol - idWidth - padding*2
	if descCol < 0 {
		descCol = 0
	}

	for i, row := range m.state.Rows {
		line := renderRow(row, idWidth, descCol)
		if i == m.cursor {
			line = selectedStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString(m.footer())
	return sb.String()
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
	if m.isEmptyStore() {
		return m.emptyStoreFooter()
	}
	var parts []string
	if search := footerHint(m.state.Reserved.Search, "search"); search != "" {
		parts = append(parts, search)
	}
	if cancel := footerHint(m.state.Reserved.Cancel, "cancel"); cancel != "" {
		parts = append(parts, cancel)
	}
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
// digits and symbols render as declared.
func displayKey(r rune) string {
	if unicode.IsLetter(r) {
		return string(unicode.ToLower(r))
	}
	return string(r)
}
