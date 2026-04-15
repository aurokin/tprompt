// Package daemon implements the deferred-delivery IPC server and job queue
// with replace-same-target semantics (DECISIONS.md §26).
package daemon

// Job is a deferred delivery request submitted by the TUI process. The daemon
// holds the content in memory until the target pane becomes active again.
type Job struct {
	Source       string // "prompt" or "clipboard"
	PromptID     string // set when Source == "prompt"
	Body         []byte // prompt body or captured clipboard bytes
	Target       string // encoded TargetContext
	Mode         string // "paste" or "type"
	Enter        bool
	SanitizeMode string // "off" | "safe" | "strict"
}

// SubmitResult is returned synchronously from the daemon after a Submit call.
type SubmitResult struct {
	Accepted bool
	Replaced bool
	Reason   string
}

// Client is the TUI's handle to the daemon socket.
type Client interface {
	Submit(job Job) (SubmitResult, error)
}

// Server is the daemon lifecycle.
type Server interface {
	Start() error
	Stop() error
}
