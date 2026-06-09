// Package importtui implements the interactive picker for `tprompt import
// wispr -i`: a checkbox list of fresh (importable) snippets the user toggles
// before the writer runs. It is a deliberate sibling to internal/tui — neither
// imports the other — because the import picker selects ids to write, a
// different contract from the board's Submitter/clipboard/handoff domain
// (DECISIONS AUR-528 D1). The fuzzy search core IS shared: both this picker and
// the board adapt their row to internal/searchindex (AUR-530), a dependency-free
// package that keeps importtui free of store/clipboard/config. The small pure
// viewport/width helpers live in internal/tuilayout, so shared row-window and
// truncation invariants stay in one place without importing internal/tui.
//
// Unlike tui.NewRenderer, this renderer runs with tea.WithAltScreen
// (DECISIONS AUR-528 D9): the board renders inside an ephemeral tmux popup, but
// the import picker runs inline in the user's terminal, so the alt-screen keeps
// the picker off the scrollback and leaves stdout carrying exactly the
// created-path lines once Program.Run returns.
package importtui

import "io"

// ConflictKind classifies an import row against the prompt store, so the picker
// can render the right glyph and decide whether the row is selectable. It mirrors
// the importable / exists / cross-path plan statuses the caller computes.
type ConflictKind int

const (
	// ConflictNone is a fresh, importable row: pre-checked, toggle to import.
	// It is the zero value so an Item with no Conflict set behaves as before.
	ConflictNone ConflictKind = iota
	// ConflictExactTarget is a prompt whose id already exists at the exact target:
	// skip-by-default (§34), checking it arms a per-item overwrite (refresh).
	ConflictExactTarget
	// ConflictCrossPath is a same-id prompt at ANOTHER path in scope: a §4/§18
	// duplicate the importer cannot create. Shown but never selectable.
	ConflictCrossPath
)

// Item is one selectable import row: its disambiguated prompt id (the id the
// writer will use), the snippet phrase shown as the title, and its conflict
// classification. Blocker holds the conflicting path for a ConflictCrossPath row
// (empty otherwise) so the picker can show why the row is blocked.
//
// Armed pre-checks an otherwise skip-by-default ConflictExactTarget row: it marks a
// refresh the caller has already authorized (the CLI --overwrite flag), so the row
// starts armed and the glyph/counter report the overwrite truthfully instead of
// presenting it as a fresh create.
//
// Tags is the row's provenance tags (e.g. the wispr source tag plus "starred"),
// added to the fuzzy search corpus alongside ID and Title so `/`-search can match
// on them. It is never rendered.
type Item struct {
	ID       string
	Title    string
	Conflict ConflictKind
	Blocker  string
	Armed    bool
	Tags     []string
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

// Result is what Run returns after the picker closes, populated (in item order)
// only when Action == ActionConfirm. SelectedIDs is every checked id — the write
// set (fresh creates plus armed exact-target overwrites). OverwriteIDs is the
// subset of SelectedIDs that are ConflictExactTarget rows the user armed: the
// per-item overwrite set the caller folds into the replayed write.
type Result struct {
	Action       Action
	SelectedIDs  []string
	OverwriteIDs []string
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
