package lifecycle

import "fmt"

// StartOutcome classifies the high-level outcome of a lifecycle start
// attempt: a brand-new daemon was spawned, an already-running compatible
// daemon was reused, or the start attempt definitively failed.
type StartOutcome int

const (
	OutcomeUnknown        StartOutcome = iota
	OutcomeStarted                     // we spawned the daemon
	OutcomeAlreadyRunning              // a compatible daemon was already running
	OutcomeFailed                      // start attempt failed; see Reason+Detail
)

// String reports a stable text form for logs and tests.
func (o StartOutcome) String() string {
	switch o {
	case OutcomeStarted:
		return "started"
	case OutcomeAlreadyRunning:
		return "already_running"
	case OutcomeFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// StartFailureReason is a closed enum of categories the launcher may
// surface when StartOutcome == OutcomeFailed. Callers map these to user
// guidance and exit-code reasons. Adding values here is a wire-level
// change — keep the set small.
type StartFailureReason string

const (
	ReasonNone               StartFailureReason = ""
	ReasonTrustGate          StartFailureReason = "trust_gate"
	ReasonSpawnFailed        StartFailureReason = "spawn_failed"
	ReasonReadinessTimeout   StartFailureReason = "readiness_timeout"
	ReasonChildExitedEarly   StartFailureReason = "child_exited_early"
	ReasonStaleSocketRefused StartFailureReason = "stale_socket_refused"
	ReasonCooldown           StartFailureReason = "cooldown"
	ReasonConfig             StartFailureReason = "config"
	ReasonPolicyDisabled     StartFailureReason = "policy_disabled"
	ReasonOther              StartFailureReason = "other"
)

// StartResult records the outcome of a launcher invocation. Detail carries
// a human-readable explanation; logs and CLI errors should pull from it.
type StartResult struct {
	Outcome StartOutcome
	Reason  StartFailureReason
	Detail  string
}

// String produces a one-line summary suitable for diagnostics.
func (r StartResult) String() string {
	if r.Outcome == OutcomeFailed {
		if r.Detail == "" {
			return fmt.Sprintf("%s reason=%s", r.Outcome, r.Reason)
		}
		return fmt.Sprintf("%s reason=%s detail=%q", r.Outcome, r.Reason, r.Detail)
	}
	return r.Outcome.String()
}
