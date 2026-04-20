package tmux

import "fmt"

// EnvError reports that the process is not connected to a tmux session (or
// tmux itself isn't reachable) and no usable target could be derived.
type EnvError struct {
	Reason string
}

func (e *EnvError) Error() string {
	if e.Reason == "" {
		return "tmux environment unavailable"
	}
	return "tmux environment unavailable: " + e.Reason
}

// PaneMissingError reports that a referenced tmux pane does not exist.
type PaneMissingError struct {
	PaneID string
}

func (e *PaneMissingError) Error() string {
	return fmt.Sprintf("target pane %s does not exist", e.PaneID)
}

// DeliveryError reports a tmux command failure during delivery (load-buffer,
// paste-buffer, send-keys, capture-pane, display-message). Policy rejections
// (e.g. oversize body) use their own typed errors.
type DeliveryError struct {
	Op      string
	Target  string
	Message string
	Cause   error
}

func (e *DeliveryError) Error() string {
	if e.Target != "" {
		return fmt.Sprintf("tmux %s into %s failed: %s", e.Op, e.Target, e.Message)
	}
	return fmt.Sprintf("tmux %s failed: %s", e.Op, e.Message)
}

func (e *DeliveryError) Unwrap() error { return e.Cause }

// OversizeError reports that a prompt body exceeds the configured
// max_paste_bytes ceiling and was rejected before any tmux call.
type OversizeError struct {
	Bytes int
	Limit int64
}

func (e *OversizeError) Error() string {
	return fmt.Sprintf("prompt body exceeds max_paste_bytes (%d > %d)", e.Bytes, e.Limit)
}
