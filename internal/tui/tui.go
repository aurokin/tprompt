// Package tui implements the built-in Bubble Tea interface: keybind board,
// pinned clipboard row, /-search (DECISIONS.md §15, §19).
package tui

import "strings"

// Action is the user's selection result.
type Action string

const (
	ActionPrompt    Action = "prompt"
	ActionClipboard Action = "clipboard"
	ActionCancel    Action = "cancel"
)

// Result is what Run returns after the TUI closes.
type Result struct {
	Action   Action
	PromptID string // populated when Action == ActionPrompt
	Scope    string // optional; disambiguates shadowed cross-tier prompts.
	// ClipboardBody is captured by the Renderer at the moment of intent so the
	// handoff worker never re-reads the clipboard. Populated when Action == ActionClipboard.
	ClipboardBody []byte
}

// State holds everything the TUI needs to render without importing store or
// keybind packages directly.
type State struct {
	Rows               []Row
	Overflow           []Row
	Reserved           ReservedKeys
	ClipboardAvailable bool
}

// ReservedBinding is a resolved reserved-key role: either a printable rune, a
// symbolic key name, or disabled.
type ReservedBinding struct {
	Printable rune
	Symbolic  string
	Disabled  bool
}

// ReservedKeys groups the TUI's reserved-key roles so matching and footer
// rendering can share one source of truth.
type ReservedKeys struct {
	Clipboard ReservedBinding
	Search    ReservedBinding
	Cancel    ReservedBinding
	Select    ReservedBinding
}

// Row is a single keybind-board entry.
type Row struct {
	Key         rune
	PromptID    string
	Scope       string
	Title       string
	Description string
	Tags        []string
	Shadowed    bool
}

// DisplayName returns the text shown in the board's name (id) column. Prompt
// rows show their id; the pinned clipboard row — identified by an empty
// PromptID, the same sentinel used everywhere else — shows the fixed
// "clipboard" label so its purpose is legible on the board and in search.
func (r Row) DisplayName() string {
	if r.PromptID == "" {
		return "clipboard"
	}
	return r.PromptID
}

// DisplayDescription returns the text shown in the board's description column.
func (r Row) DisplayDescription() string {
	if r.Description != "" {
		return r.Description
	}
	if r.Title != "" {
		return r.Title
	}
	return ""
}

// SearchText returns the text corpus indexed by /-search.
func (r Row) SearchText() string {
	parts := make([]string, 0, 3+len(r.Tags))
	for _, part := range []string{r.PromptID, r.Title, r.Description} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	parts = append(parts, r.Tags...)
	return strings.Join(parts, " ")
}

// Renderer is the interface defined in docs/implementation/interfaces.md.
type Renderer interface {
	Run(state State) (Result, error)
}
