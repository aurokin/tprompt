// Package delivery contains the local, verified tmux delivery contract used by
// TUI handoff workers.
package delivery

import (
	"errors"
	"fmt"
	"time"

	"github.com/hsadler/tprompt/internal/tmux"
)

// Source values for Job.Source.
const (
	SourcePrompt    = "prompt"
	SourceClipboard = "clipboard"
)

// Job is a deferred delivery request submitted by the TUI process. Body bytes
// are transient and must never be logged.
type Job struct {
	JobID        string              `json:"job_id"`
	CreatedAt    time.Time           `json:"created_at"`
	SubmitterPID int                 `json:"submitter_pid,omitempty"`
	Source       string              `json:"source"`
	PromptID     string              `json:"prompt_id,omitempty"`
	SourcePath   string              `json:"source_path,omitempty"`
	Body         []byte              `json:"body"`
	Mode         string              `json:"mode"`
	Enter        bool                `json:"enter"`
	SanitizeMode string              `json:"sanitize_mode"`
	PaneID       string              `json:"pane_id"`
	Origin       *tmux.OriginContext `json:"origin,omitempty"`
	Verification VerificationPolicy  `json:"verification"`
}

func (j Job) deliveryTarget() tmux.TargetContext {
	return tmux.TargetContext{PaneID: j.PaneID}
}

func (j Job) verificationTarget() tmux.TargetContext {
	return j.originContext().WithPane(j.PaneID)
}

func (j Job) messageTarget() tmux.MessageTarget {
	return j.originContext().MessageTarget(j.PaneID)
}

func (j Job) originContext() tmux.OriginContext {
	if j.Origin == nil {
		return tmux.OriginContext{}
	}
	return *j.Origin
}

// VerificationPolicy controls how long delivery waits for the target pane to
// become active before injecting.
type VerificationPolicy struct {
	TimeoutMS      int `json:"timeout_ms"`
	PollIntervalMS int `json:"poll_interval_ms"`
}

// SubmitRequest is the local handoff payload for enqueuing a Job.
type SubmitRequest struct {
	Job Job `json:"job"`
}

// SubmitResponse acknowledges local handoff acceptance.
type SubmitResponse struct {
	Accepted      bool   `json:"accepted"`
	JobID         string `json:"job_id"`
	ReplacedJobID string `json:"replaced_job_id,omitempty"`
}

// Client is the TUI-facing handle for submitting local delivery work.
type Client interface {
	Submit(req SubmitRequest) (SubmitResponse, error)
}

// UnavailableError reports that the local delivery path cannot accept work.
type UnavailableError struct {
	Path   string
	Reason string
}

func (e *UnavailableError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("delivery path %s unavailable", e.Path)
	}
	return fmt.Sprintf("delivery path %s unavailable: %s", e.Path, e.Reason)
}

// IPCError reports a failure while handing off work to another local process.
type IPCError struct {
	Path   string
	Op     string
	Reason string
	Err    error
}

func (e *IPCError) Error() string {
	base := "delivery ipc error"
	if e.Path != "" {
		base = fmt.Sprintf("delivery ipc error on %s", e.Path)
	}
	if e.Op != "" {
		base = fmt.Sprintf("%s: %s", base, e.Op)
	}
	if e.Reason == "" {
		return base
	}
	return fmt.Sprintf("%s: %s", base, e.Reason)
}

func (e *IPCError) Unwrap() error { return e.Err }

// IsUnavailable reports whether err is a delivery-unavailable error.
func IsUnavailable(err error) bool {
	var unavailable *UnavailableError
	return errors.As(err, &unavailable)
}

// TimeoutError reports that verification did not complete within the policy
// timeout.
type TimeoutError struct {
	TimeoutMS int
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("verification timed out after %dms", e.TimeoutMS)
}

// InvalidPolicyError reports that a VerificationPolicy field held a non-
// positive value.
type InvalidPolicyError struct {
	Field string
	Value int
}

func (e *InvalidPolicyError) Error() string {
	return fmt.Sprintf("verification policy: %s=%d must be > 0", e.Field, e.Value)
}
