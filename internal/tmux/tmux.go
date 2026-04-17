// Package tmux owns all tmux command construction, target inspection, and
// delivery. See docs/tmux/delivery.md and docs/tmux/verification.md.
package tmux

// TargetContext identifies a tmux pane plus (when known) the client whose
// context the delivery should be scoped to.
type TargetContext struct {
	Session   string
	Window    string
	PaneID    string
	ClientTTY string
}

// Adapter is the interface defined in docs/implementation/interfaces.md.
type Adapter interface {
	CurrentContext() (TargetContext, error)
	PaneExists(paneID string) (bool, error)
	IsTargetSelected(target TargetContext) (bool, error)
	CapturePaneTail(paneID string, lines int) (string, error)
	Paste(target TargetContext, body string, pressEnter bool) error
	Type(target TargetContext, body string, pressEnter bool) error
	DisplayMessage(clientTTY, message string) error
}
