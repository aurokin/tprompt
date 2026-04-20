package daemon

import "fmt"

// validateJob checks wire-submitted Job fields before enqueueing so the daemon
// rejects malformed requests synchronously on SubmitResponse instead of
// surfacing the failure later via tmux display-message. Clients get a clear
// error at submit time; the banner path is reserved for failures discovered
// during verification/delivery, when the daemon is the only actor left.
func validateJob(j Job) error {
	if j.PaneID == "" {
		return fmt.Errorf("invalid job: pane_id is required")
	}
	switch j.Source {
	case SourcePrompt, SourceClipboard:
	default:
		return fmt.Errorf("invalid job: source must be prompt or clipboard, got %q", j.Source)
	}
	switch j.Mode {
	case "paste", "type":
	default:
		return fmt.Errorf("invalid job: mode must be paste or type, got %q", j.Mode)
	}
	switch j.SanitizeMode {
	case "off", "safe", "strict":
	default:
		return fmt.Errorf("invalid job: sanitize_mode must be off, safe, or strict, got %q", j.SanitizeMode)
	}
	if j.Verification.TimeoutMS <= 0 {
		return fmt.Errorf("invalid job: verification.timeout_ms must be > 0, got %d", j.Verification.TimeoutMS)
	}
	if j.Verification.PollIntervalMS <= 0 {
		return fmt.Errorf("invalid job: verification.poll_interval_ms must be > 0, got %d", j.Verification.PollIntervalMS)
	}
	return nil
}
