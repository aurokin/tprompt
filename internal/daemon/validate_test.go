package daemon

import (
	"strings"
	"testing"
)

func validJobForValidator() Job {
	return Job{
		Source:       SourcePrompt,
		Body:         []byte("hi"),
		Mode:         "paste",
		SanitizeMode: "off",
		PaneID:       "%1",
		Verification: VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 50},
	}
}

func TestValidateJobAcceptsValidJob(t *testing.T) {
	if err := validateJob(validJobForValidator()); err != nil {
		t.Fatalf("validateJob rejected a valid job: %v", err)
	}
}

func TestValidateJobRejectsBadFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Job)
		wantMsg string
	}{
		{"missing pane id", func(j *Job) { j.PaneID = "" }, "pane_id is required"},
		{"empty source", func(j *Job) { j.Source = "" }, "source must be prompt or clipboard"},
		{"unknown source", func(j *Job) { j.Source = "garbage" }, "source must be prompt or clipboard"},
		{"empty mode", func(j *Job) { j.Mode = "" }, "mode must be paste or type"},
		{"unknown mode", func(j *Job) { j.Mode = "spray" }, "mode must be paste or type"},
		{"empty sanitize mode", func(j *Job) { j.SanitizeMode = "" }, "sanitize_mode must be off, safe, or strict"},
		{"unknown sanitize mode", func(j *Job) { j.SanitizeMode = "aggressive" }, "sanitize_mode must be off, safe, or strict"},
		{"zero timeout", func(j *Job) { j.Verification.TimeoutMS = 0 }, "timeout_ms must be > 0"},
		{"negative timeout", func(j *Job) { j.Verification.TimeoutMS = -1 }, "timeout_ms must be > 0"},
		{"zero interval", func(j *Job) { j.Verification.PollIntervalMS = 0 }, "poll_interval_ms must be > 0"},
		{"negative interval", func(j *Job) { j.Verification.PollIntervalMS = -5 }, "poll_interval_ms must be > 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := validJobForValidator()
			tc.mutate(&j)
			err := validateJob(j)
			if err == nil {
				t.Fatalf("validateJob accepted invalid job")
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}
