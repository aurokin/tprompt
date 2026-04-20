package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/hsadler/tprompt/internal/tmux"
)

func TestJobJSONRoundTrip(t *testing.T) {
	want := Job{
		JobID:        "j-1700000000000000000-1",
		CreatedAt:    time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		SubmitterPID: 4242,
		Source:       SourcePrompt,
		PromptID:     "code-review",
		SourcePath:   "/prompts/code-review.md",
		Body:         []byte("Review this code."),
		Mode:         "paste",
		Enter:        false,
		SanitizeMode: "off",
		PaneID:       "%5",
		Origin: &tmux.OriginContext{
			Session:   "$0",
			Window:    "@1",
			ClientTTY: "/dev/pts/0",
		},
		Verification: VerificationPolicy{TimeoutMS: 5000, PollIntervalMS: 100},
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Job
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestJobJSONUsesSnakeCase(t *testing.T) {
	// Wire-protocol stability: all fields use snake_case so the on-the-wire
	// payload matches docs/architecture/data-model.md.
	j := Job{
		JobID:        "j-1",
		Source:       SourceClipboard,
		Body:         []byte("hi"),
		Mode:         "paste",
		SanitizeMode: "safe",
		PaneID:       "%1",
		Verification: VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 50},
	}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	for _, key := range []string{
		`"job_id"`, `"created_at"`, `"source"`, `"body"`, `"mode"`, `"enter"`,
		`"sanitize_mode"`, `"pane_id"`, `"verification"`,
		`"timeout_ms"`, `"poll_interval_ms"`,
	} {
		if !strings.Contains(got, key) {
			t.Errorf("expected %s in payload, got: %s", key, got)
		}
	}
	if strings.Contains(got, `"target"`) {
		t.Errorf(`payload should not contain legacy "target" object, got: %s`, got)
	}
}

func TestJobJSONOmitsPromptFieldsForClipboard(t *testing.T) {
	j := Job{
		JobID:        "j-1",
		Source:       SourceClipboard,
		Body:         []byte("hi"),
		Mode:         "paste",
		SanitizeMode: "off",
		PaneID:       "%1",
		Verification: VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 50},
	}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, `"prompt_id"`) {
		t.Errorf("prompt_id should be omitted for clipboard source, got: %s", got)
	}
	if strings.Contains(got, `"source_path"`) {
		t.Errorf("source_path should be omitted for clipboard source, got: %s", got)
	}
	if strings.Contains(got, `"origin"`) {
		t.Errorf("origin should be omitted when empty, got: %s", got)
	}
}

func TestSubmitResponseOmitsReplacedWhenEmpty(t *testing.T) {
	raw, err := json.Marshal(SubmitResponse{Accepted: true, JobID: "j-1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"replaced_job_id"`) {
		t.Errorf("replaced_job_id should be omitted when empty, got: %s", raw)
	}
}

func TestSubmitResponseIncludesReplacedWhenSet(t *testing.T) {
	raw, err := json.Marshal(SubmitResponse{
		Accepted:      true,
		JobID:         "j-2",
		ReplacedJobID: "j-1",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"replaced_job_id":"j-1"`) {
		t.Errorf("replaced_job_id should appear when set, got: %s", raw)
	}
}

func TestStatusResponseRoundTrip(t *testing.T) {
	want := StatusResponse{
		PID:         12345,
		Socket:      "/run/user/1000/tprompt/daemon.sock",
		LogPath:     "/run/user/1000/tprompt/daemon.log",
		UptimeSec:   42,
		PendingJobs: 3,
		Version:     "0.1.0",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got StatusResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestSocketUnavailableErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *SocketUnavailableError
		want string
	}{
		{
			name: "no reason",
			err:  &SocketUnavailableError{Path: "/tmp/x.sock"},
			want: "daemon socket /tmp/x.sock unavailable",
		},
		{
			name: "with reason",
			err:  &SocketUnavailableError{Path: "/tmp/x.sock", Reason: "already running"},
			want: "daemon socket /tmp/x.sock unavailable: already running",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJobJSONIncludesSubmitterPIDWhenSet(t *testing.T) {
	j := Job{
		JobID:        "j-1",
		SubmitterPID: 4242,
		Source:       SourceClipboard,
		Body:         []byte("hi"),
		Mode:         "paste",
		SanitizeMode: "safe",
		PaneID:       "%1",
		Verification: VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 50},
	}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"submitter_pid":4242`) {
		t.Fatalf("submitter_pid should appear when set, got: %s", raw)
	}
}

func TestJobJSONIncludesOriginWhenSet(t *testing.T) {
	j := Job{
		JobID:        "j-1",
		Source:       SourceClipboard,
		Body:         []byte("hi"),
		Mode:         "paste",
		SanitizeMode: "safe",
		PaneID:       "%1",
		Origin:       &tmux.OriginContext{Session: "$1", Window: "@2", ClientTTY: "/dev/pts/0"},
		Verification: VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 50},
	}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	for _, key := range []string{`"origin"`, `"session"`, `"window"`, `"client_tty"`} {
		if !strings.Contains(got, key) {
			t.Fatalf("expected %s in payload, got: %s", key, got)
		}
	}
}

func TestIPCErrorMessage(t *testing.T) {
	err := &IPCError{Path: "/tmp/x.sock", Op: "read response", Reason: "EOF"}
	want := "daemon ipc error on /tmp/x.sock: read response: EOF"
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestTimeoutErrorMessage(t *testing.T) {
	err := &TimeoutError{TimeoutMS: 5000}
	want := "verification timed out after 5000ms"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
