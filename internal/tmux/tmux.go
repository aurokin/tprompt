// Package tmux owns all tmux command construction, target inspection, and
// delivery. See docs/tmux/delivery.md and docs/tmux/verification.md.
package tmux

import "context"

// TargetContext identifies the delivery pane plus any extra tmux context the
// daemon can use for verification and failure routing. JSON tags make the
// struct safe to transport over the daemon IPC wire (see internal/daemon).
type TargetContext struct {
	Session   string `json:"session,omitempty"`
	Window    string `json:"window,omitempty"`
	PaneID    string `json:"pane_id"`
	ClientTTY string `json:"client_tty,omitempty"`
}

// OriginContext carries the submitting tmux client metadata that can help the
// daemon verify focus return and scope failure banners. It intentionally omits
// pane identity, which is tracked separately by daemon jobs.
type OriginContext struct {
	Session   string `json:"session,omitempty"`
	Window    string `json:"window,omitempty"`
	ClientTTY string `json:"client_tty,omitempty"`
}

// MessageTarget scopes a tmux display-message banner. It is separate from
// TargetContext so callers do not accidentally reuse a delivery target as a
// notification target after the pane has disappeared.
type MessageTarget struct {
	Session   string
	Window    string
	PaneID    string
	ClientTTY string
}

// MessageTarget returns the notification scope implied by this target
// context.
func (t TargetContext) MessageTarget() MessageTarget {
	return MessageTarget{
		Session:   t.Session,
		Window:    t.Window,
		PaneID:    t.PaneID,
		ClientTTY: t.ClientTTY,
	}
}

// WithPane combines the origin metadata with a specific pane.
func (o OriginContext) WithPane(paneID string) TargetContext {
	return TargetContext{
		Session:   o.Session,
		Window:    o.Window,
		PaneID:    paneID,
		ClientTTY: o.ClientTTY,
	}
}

// MessageTarget scopes a banner for the specified pane plus this origin
// metadata.
func (o OriginContext) MessageTarget(paneID string) MessageTarget {
	return MessageTarget{
		Session:   o.Session,
		Window:    o.Window,
		PaneID:    paneID,
		ClientTTY: o.ClientTTY,
	}
}

// Adapter is the interface defined in docs/implementation/interfaces.md.
type Adapter interface {
	CurrentContext() (TargetContext, error)
	PaneExists(ctx context.Context, paneID string) (bool, error)
	IsTargetSelected(ctx context.Context, target TargetContext) (bool, error)
	CapturePaneTail(paneID string, lines int) (string, error)
	Paste(ctx context.Context, target TargetContext, body string, pressEnter bool) error
	Type(ctx context.Context, target TargetContext, body string, pressEnter bool) error
	DisplayMessage(target MessageTarget, message string) error
}
