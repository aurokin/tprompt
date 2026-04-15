// Package picker wraps an external picker command for `tprompt pick`
// (DECISIONS.md §15 — not used by the TUI flow).
package picker

// Picker is the interface defined in docs/implementation/interfaces.md.
type Picker interface {
	Select(ids []string) (selectedID string, cancelled bool, err error)
}
