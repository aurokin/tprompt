// Package importtui implements the interactive picker for `tprompt import
// wispr -i`: a checkbox list of fresh (importable) snippets the user toggles
// before the writer runs. It is a deliberate sibling to internal/tui — neither
// imports the other — because the import picker selects ids to write, a
// different contract from the board's Submitter/clipboard/handoff domain
// (DECISIONS AUR-528 D1). The small pure viewport helpers (clampScrollOffset,
// rowsPerFrame, headerLines, visibleRowRange) are COPIED from internal/tui
// rather than shared: they are <60 LoC with no flow coupling, and exporting
// them now would couple two renderers that are still diverging. A later slice
// (AUR-530) may promote a shared core.
//
// Unlike tui.NewRenderer, this renderer runs with tea.WithAltScreen
// (DECISIONS AUR-528 D9): the board renders inside an ephemeral tmux popup, but
// the import picker runs inline in the user's terminal, so the alt-screen keeps
// the picker off the scrollback and leaves stdout carrying exactly the
// created-path lines once Program.Run returns.
package importtui

import "io"

// Item is one selectable fresh snippet row: its disambiguated prompt id (the id
// the writer will use) and the snippet phrase shown as the title.
type Item struct {
	ID    string
	Title string
}

// Action is the user's picker outcome.
type Action int

const (
	// ActionConfirm means the user accepted the (possibly edited) selection.
	ActionConfirm Action = iota
	// ActionCancel means the user aborted; the caller writes nothing and exits 0.
	ActionCancel
)

// State is what the renderer shows: the fresh items to choose from.
type State struct {
	Items []Item
}

// Result is what Run returns after the picker closes. SelectedIDs is populated
// (in item order) only when Action == ActionConfirm.
type Result struct {
	Action      Action
	SelectedIDs []string
}

// Renderer drives one interactive selection session.
type Renderer interface {
	Run(state State) (Result, error)
}

// ProgramIO carries the streams Bubble Tea should use instead of process-global
// stdin/stdout defaults. Mirrors tui.ProgramIO.
type ProgramIO struct {
	Input  io.Reader
	Output io.Writer
}
