// Package daemon implements the deferred-delivery IPC server and job queue
// with replace-same-target semantics (DECISIONS.md §26).
//
// MVP wire protocol: line-delimited JSON, one request and one response per
// connection, then close. Submit is fire-and-ack — SubmitResponse is returned
// before verification or delivery occurs, so the TUI can exit immediately.
// Verification + delivery happen asynchronously in a per-job goroutine on the
// server side.
package daemon

import (
	"fmt"
	"time"

	"github.com/hsadler/tprompt/internal/tmux"
)

// Source values for Job.Source.
const (
	SourcePrompt    = "prompt"
	SourceClipboard = "clipboard"
)

// Job is a deferred delivery request submitted by the TUI process. The daemon
// holds the body in memory until verification passes, then runs the sanitizer
// and hands the cleaned body to the tmux adapter. Pane identity is tracked
// separately from the optional origin metadata used for verification and
// banner routing. Body bytes are transient and must never be logged.
type Job struct {
	JobID     string    `json:"job_id"`
	CreatedAt time.Time `json:"created_at"`
	// SubmitterPID is the PID of the process that submitted this job (typically
	// the popup-hosted TUI). When present, verification waits for that process
	// to exit before treating the target pane as ready, which prevents delivery
	// from racing ahead of popup teardown.
	SubmitterPID int    `json:"submitter_pid,omitempty"`
	Source       string `json:"source"`                // SourcePrompt | SourceClipboard
	PromptID     string `json:"prompt_id,omitempty"`   // set when Source == SourcePrompt
	SourcePath   string `json:"source_path,omitempty"` // set when Source == SourcePrompt
	// Body is base64-encoded on the wire by encoding/json (Go's default for
	// []byte), so the JSON payload is ~4/3 the raw body size.
	Body         []byte `json:"body"`
	Mode         string `json:"mode"` // "paste" | "type"
	Enter        bool   `json:"enter"`
	SanitizeMode string `json:"sanitize_mode"` // "off" | "safe" | "strict"
	PaneID       string `json:"pane_id"`
	// Origin carries the submitting tmux client metadata when known. It is
	// optional because tests and future callers may only know the pane.
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

// VerificationPolicy controls how long the daemon waits for the target pane
// to become the active pane again before delivering. The require_* booleans
// from docs/architecture/data-model.md are baked in for MVP — pre-injection
// pane existence and selection are always checked. Optional post-injection
// capture comparison is daemon config, not wire payload policy.
type VerificationPolicy struct {
	TimeoutMS      int `json:"timeout_ms"`
	PollIntervalMS int `json:"poll_interval_ms"`
}

// SubmitRequest is the client → server payload for enqueuing a Job.
type SubmitRequest struct {
	Job Job `json:"job"`
}

// SubmitResponse is the server's acknowledgement. Accepted is true on every
// successful enqueue; ReplacedJobID names a prior pending job for the same
// pane that is known to have been discarded before the acknowledgement is
// returned (empty when no such replacement occurred).
type SubmitResponse struct {
	Accepted      bool   `json:"accepted"`
	JobID         string `json:"job_id"`
	ReplacedJobID string `json:"replaced_job_id,omitempty"`
}

// StatusRequest asks the server to report its current state.
type StatusRequest struct{}

// StatusResponse describes the running daemon for `tprompt daemon status`.
type StatusResponse struct {
	PID         int    `json:"pid"`
	Socket      string `json:"socket"`
	LogPath     string `json:"log_path"`
	UptimeSec   int64  `json:"uptime_sec"`
	PendingJobs int    `json:"pending_jobs"`
	Version     string `json:"version"`
}

// StopRequest asks the server to begin graceful shutdown.
type StopRequest struct{}

// StopResponse acknowledges that the daemon accepted the shutdown request.
type StopResponse struct {
	Accepted bool `json:"accepted"`
}

// Client is the TUI- and CLI-facing handle to the daemon socket.
type Client interface {
	Submit(req SubmitRequest) (SubmitResponse, error)
	Status() (StatusResponse, error)
	Stop() (StopResponse, error)
}

// SocketUnavailableError reports that the daemon socket cannot be reached:
// no socket file, connection refused by a stale socket the caller could not
// clean up, or another daemon already holding the path. Maps to ExitDaemon
// (5) at the CLI boundary.
type SocketUnavailableError struct {
	Path   string
	Reason string
}

func (e *SocketUnavailableError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("daemon socket %s unavailable", e.Path)
	}
	return fmt.Sprintf("daemon socket %s unavailable: %s", e.Path, e.Reason)
}

// IPCError reports a failure on an already-established daemon socket, such as
// a broken write or an EOF while waiting for the daemon's reply. These are
// still daemon-class failures and map to ExitDaemon (5) at the CLI boundary.
type IPCError struct {
	Path   string
	Op     string
	Reason string
}

func (e *IPCError) Error() string {
	base := "daemon ipc error"
	if e.Path != "" {
		base = fmt.Sprintf("daemon ipc error on %s", e.Path)
	}
	if e.Op != "" {
		base = fmt.Sprintf("%s: %s", base, e.Op)
	}
	if e.Reason == "" {
		return base
	}
	return fmt.Sprintf("%s: %s", base, e.Reason)
}

// ShutdownTimeoutError reports that `daemon stop` was acknowledged but the
// daemon socket was still reachable after the bounded graceful wait.
type ShutdownTimeoutError struct {
	Path      string
	TimeoutMS int
}

func (e *ShutdownTimeoutError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("daemon shutdown not confirmed after %dms", e.TimeoutMS)
	}
	return fmt.Sprintf("daemon shutdown not confirmed for %s after %dms", e.Path, e.TimeoutMS)
}

// TimeoutError reports that verification did not complete within the policy
// timeout. Maps to ExitDaemon (5) at the CLI boundary; surfaced via
// `tmux display-message` and the daemon log on the server side. The job ID
// is carried by the Executor's log entry, not the error, so this struct only
// needs the timeout value.
type TimeoutError struct {
	TimeoutMS int
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("verification timed out after %dms", e.TimeoutMS)
}

// InvalidPolicyError reports that a VerificationPolicy field held a non-
// positive value. Callers build the policy from validated config, so this
// is a programmer error rather than user input, but it still surfaces as
// ExitDaemon (5) to avoid silent hangs if a future caller forgets to
// populate the fields.
type InvalidPolicyError struct {
	Field string
	Value int
}

func (e *InvalidPolicyError) Error() string {
	return fmt.Sprintf("verification policy: %s=%d must be > 0", e.Field, e.Value)
}
