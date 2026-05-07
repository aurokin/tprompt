package lifecycle

import "testing"

func TestStartOutcomeString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		o    StartOutcome
		want string
	}{
		{OutcomeUnknown, "unknown"},
		{OutcomeStarted, "started"},
		{OutcomeAlreadyRunning, "already_running"},
		{OutcomeFailed, "failed"},
	}
	for _, c := range cases {
		if got := c.o.String(); got != c.want {
			t.Errorf("(%d).String() = %q, want %q", c.o, got, c.want)
		}
	}
}

func TestStartResultString(t *testing.T) {
	t.Parallel()
	if got := (StartResult{Outcome: OutcomeStarted}).String(); got != "started" {
		t.Errorf("started: %q", got)
	}
	if got := (StartResult{Outcome: OutcomeAlreadyRunning}).String(); got != "already_running" {
		t.Errorf("already running: %q", got)
	}
	r := StartResult{Outcome: OutcomeFailed, Reason: ReasonReadinessTimeout, Detail: "5s exceeded"}
	if got := r.String(); got == "" {
		t.Error("failed result string empty")
	}
}
