package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// socketPath returns a short Unix socket path under t.TempDir, skipping the
// test if the resulting path exceeds the OS sun_path limit.
func socketPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "d.sock")
	limit := 108
	if runtime.GOOS == "darwin" {
		limit = 104
	}
	if len(p) >= limit {
		t.Skipf("temp dir produces socket path %d chars, exceeds %d", len(p), limit)
	}
	return p
}

func newTestServer(t *testing.T, path string, runJob JobRunner) (*Server, *Queue, *fakeAdapter) {
	t.Helper()
	if runJob == nil {
		runJob = func(context.Context, Job) bool { return true } // no-op
	}
	adapter := &fakeAdapter{}
	logger := NewLoggerWriter(&bytes.Buffer{})
	queue := NewQueue(adapter, logger, runJob)
	srv := NewServer(ServerConfig{
		SocketPath: path,
		Queue:      queue,
		Logger:     logger,
		StatusFn: func() StatusResponse {
			return StatusResponse{
				PID:         12345,
				Socket:      path,
				LogPath:     "/tmp/log",
				UptimeSec:   7,
				PendingJobs: queue.Pending(),
				Version:     "test",
			}
		},
	})
	return srv, queue, adapter
}

// startServer drives the full lifecycle through Run, which is race-clean by
// construction. Tests that only need to exercise Listen edge cases (e.g.
// stale-socket cleanup, refusal under contention) call srv.Listen directly
// and never start a Serve goroutine.
func startServer(t *testing.T, srv *Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		_, err := Run(ctx, srv, func() { close(ready) })
		runDone <- err
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatalf("server did not signal ready for %s", srv.SocketPath())
	}

	t.Cleanup(func() {
		cancel()
		select {
		case <-runDone:
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return after cancellation")
		}
	})
}

func TestServerSubmitRoundTrip(t *testing.T) {
	path := socketPath(t)
	released := make(chan struct{})
	runJob := func(ctx context.Context, job Job) bool {
		<-released
		return true
	}
	srv, queue, _ := newTestServer(t, path, runJob)
	startServer(t, srv)
	defer close(released)

	client := NewSocketClient(path)
	job := Job{
		Source:       SourcePrompt,
		Body:         []byte("hello"),
		Mode:         "paste",
		SanitizeMode: "off",
		PaneID:       "%5",
		Verification: VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 50},
	}
	resp, err := client.Submit(SubmitRequest{Job: job})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !resp.Accepted {
		t.Fatalf("response = %+v, want Accepted=true", resp)
	}
	if !strings.HasPrefix(resp.JobID, "j-") {
		t.Fatalf("JobID = %q, want server-stamped (prefix j-)", resp.JobID)
	}
	deadline := time.Now().Add(time.Second)
	for queue.Pending() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("queue.Pending stayed at 0; job not enqueued")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestServerStampsJobIDAndDiscardsClientValue(t *testing.T) {
	path := socketPath(t)
	released := make(chan struct{})
	runJob := func(ctx context.Context, job Job) bool {
		<-released
		return true
	}
	srv, _, _ := newTestServer(t, path, runJob)
	startServer(t, srv)
	defer close(released)

	client := NewSocketClient(path)
	job := Job{
		JobID:        "client-supplied-should-be-ignored",
		Source:       SourcePrompt,
		Body:         []byte("hi"),
		Mode:         "paste",
		SanitizeMode: "off",
		PaneID:       "%1",
		Verification: VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 50},
	}
	first, err := client.Submit(SubmitRequest{Job: job})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if first.JobID == job.JobID {
		t.Fatalf("server returned client-supplied JobID %q; expected stamped value", first.JobID)
	}
	if !strings.HasPrefix(first.JobID, "j-") {
		t.Fatalf("first JobID = %q, want j-<nanos>-<n>", first.JobID)
	}

	// Different pane so the second submit doesn't replace the first.
	job.PaneID = "%2"
	second, err := client.Submit(SubmitRequest{Job: job})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if second.JobID == first.JobID {
		t.Fatalf("server reused JobID across submits: %q", second.JobID)
	}
}

func TestNextJobIDFormatAndMonotonic(t *testing.T) {
	// nextJobID is in-memory; no Listen needed, so any path works.
	srv, _, _ := newTestServer(t, socketPath(t), nil)

	a := srv.nextJobID()
	b := srv.nextJobID()
	for _, id := range []string{a, b} {
		parts := strings.Split(id, "-")
		if len(parts) != 3 || parts[0] != "j" {
			t.Fatalf("JobID %q does not match j-<nanos>-<n>", id)
		}
	}
	if a == b {
		t.Fatalf("nextJobID returned duplicates: %q", a)
	}
}

func TestServerStatusRoundTrip(t *testing.T) {
	path := socketPath(t)
	srv, _, _ := newTestServer(t, path, nil)
	startServer(t, srv)

	client := NewSocketClient(path)
	got, err := client.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	want := StatusResponse{
		PID:         12345,
		Socket:      path,
		LogPath:     "/tmp/log",
		UptimeSec:   7,
		PendingJobs: 0,
		Version:     "test",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("status mismatch (-want +got):\n%s", diff)
	}
}

func TestServerStopRoundTripCancelsRun(t *testing.T) {
	path := socketPath(t)
	adapter := &fakeAdapter{}
	logger := NewLoggerWriter(&bytes.Buffer{})
	queue := NewQueue(adapter, logger, func(context.Context, Job) bool { return true })

	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(ServerConfig{
		SocketPath: path,
		Queue:      queue,
		Logger:     logger,
		StatusFn:   func() StatusResponse { return StatusResponse{} },
		ShutdownFn: cancel,
	})

	runDone := make(chan struct {
		result RunResult
		err    error
	}, 1)
	ready := make(chan struct{})
	go func() {
		result, err := Run(ctx, srv, func() { close(ready) })
		runDone <- struct {
			result RunResult
			err    error
		}{result: result, err: err}
	}()

	select {
	case <-ready:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("server did not signal ready")
	}

	resp, err := NewSocketClient(path).Stop()
	if err != nil {
		cancel()
		t.Fatalf("Stop: %v", err)
	}
	if !resp.Accepted {
		cancel()
		t.Fatalf("Stop response = %+v, want Accepted=true", resp)
	}

	select {
	case done := <-runDone:
		if done.err != nil {
			t.Fatalf("Run returned error after stop: %v", done.err)
		}
		if done.result.ExitReason != RunExitContextCanceled {
			t.Fatalf("ExitReason = %q, want %q", done.result.ExitReason, RunExitContextCanceled)
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("Run did not return after stop")
	}
}

func TestListenStaleSocketIsRemoved(t *testing.T) {
	path := socketPath(t)

	// First daemon binds, then exits ungracefully (simulate by Closing the
	// listener but leaving the file in place).
	first := net.UnixAddr{Name: path, Net: "unix"}
	l, err := net.ListenUnix("unix", &first)
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}
	l.SetUnlinkOnClose(false)
	if err := l.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected stale socket file to remain: %v", err)
	}

	srv, _, _ := newTestServer(t, path, nil)
	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen on stale socket: %v", err)
	}
	defer func() { _ = srv.Close() }()
}

func TestListenRefusesWhenLiveDaemonHoldsSocket(t *testing.T) {
	path := socketPath(t)
	first, _, _ := newTestServer(t, path, nil)
	startServer(t, first)

	second, _, _ := newTestServer(t, path, nil)
	err := second.Listen()
	var sue *SocketUnavailableError
	if !errors.As(err, &sue) {
		t.Fatalf("want SocketUnavailableError, got %T: %v", err, err)
	}
	if !strings.Contains(sue.Reason, "already running") {
		t.Fatalf("Reason = %q, want 'already running'", sue.Reason)
	}
}

func TestListenAmbiguousSocketProbePreservesPath(t *testing.T) {
	path := socketPath(t)
	addr := net.UnixAddr{Name: path, Net: "unix"}
	l, err := net.ListenUnix("unix", &addr)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	l.SetUnlinkOnClose(false)
	if err := l.Close(); err != nil {
		t.Fatalf("close unix listener: %v", err)
	}

	srv, _, _ := newTestServer(t, path, nil)
	srv.dial = func(string, string, time.Duration) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "unix", Err: syscall.EACCES}
	}

	err = srv.Listen()
	var sue *SocketUnavailableError
	if !errors.As(err, &sue) {
		t.Fatalf("want SocketUnavailableError, got %T: %v", err, err)
	}
	if !strings.Contains(sue.Reason, syscall.EACCES.Error()) {
		t.Fatalf("Reason = %q, want %q", sue.Reason, syscall.EACCES.Error())
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected ambiguous probe to preserve socket path: %v", statErr)
	}
}

func TestListenRejectsNonSocketFileAtPath(t *testing.T) {
	path := socketPath(t)
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	srv, _, _ := newTestServer(t, path, nil)
	err := srv.Listen()
	var sue *SocketUnavailableError
	if !errors.As(err, &sue) {
		t.Fatalf("want SocketUnavailableError, got %T: %v", err, err)
	}
	if !strings.Contains(sue.Reason, "not a socket") {
		t.Fatalf("Reason = %q, want 'not a socket'", sue.Reason)
	}
}

func TestSocketHasRestrictedPermissions(t *testing.T) {
	path := socketPath(t)
	srv, _, _ := newTestServer(t, path, nil)
	startServer(t, srv)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perms = %o, want 0600", info.Mode().Perm())
	}
}

func TestClientDialFailureReturnsSocketUnavailable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-daemon.sock")
	client := NewSocketClient(missing)
	_, err := client.Status()
	var sue *SocketUnavailableError
	if !errors.As(err, &sue) {
		t.Fatalf("want SocketUnavailableError, got %T: %v", err, err)
	}
	if sue.Path != missing {
		t.Fatalf("Path = %q, want %q", sue.Path, missing)
	}
}

func TestServerRejectsUnknownRequestKind(t *testing.T) {
	path := socketPath(t)
	srv, _, _ := newTestServer(t, path, nil)
	startServer(t, srv)

	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	if err := json.NewEncoder(conn).Encode(map[string]string{"kind": "bogus"}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp wireResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp.Error, "unknown request kind") {
		t.Fatalf("expected unknown-kind error, got: %+v", resp)
	}
}

func TestServerLogsIPCErrorOnDecodeFailure(t *testing.T) {
	path := socketPath(t)
	adapter := &fakeAdapter{}
	var logBuf bytes.Buffer
	logger := NewLoggerWriter(&logBuf)
	queue := NewQueue(adapter, logger, func(context.Context, Job) bool { return true })
	srv := NewServer(ServerConfig{
		SocketPath: path,
		Queue:      queue,
		Logger:     logger,
		StatusFn:   func() StatusResponse { return StatusResponse{} },
	})
	startServer(t, srv)

	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Send malformed JSON.
	if _, err := conn.Write([]byte("this is not json\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	var resp wireResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp.Error, "bad request") {
		t.Fatalf("expected bad-request error, got: %+v", resp)
	}
	if !strings.Contains(logBuf.String(), "outcome="+OutcomeIPCError) {
		t.Fatalf("expected ipc_error log entry, got: %q", logBuf.String())
	}
}

func TestServerRejectsSubmitMissingPayload(t *testing.T) {
	path := socketPath(t)
	srv, _, _ := newTestServer(t, path, nil)
	startServer(t, srv)

	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	if err := json.NewEncoder(conn).Encode(map[string]string{"kind": "submit"}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp wireResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp.Error, "missing payload") {
		t.Fatalf("expected missing-payload error, got: %+v", resp)
	}
}

func TestServerRejectsInvalidJobAtSubmit(t *testing.T) {
	path := socketPath(t)
	adapter := &fakeAdapter{}
	var logBuf bytes.Buffer
	logger := NewLoggerWriter(&logBuf)
	queue := NewQueue(adapter, logger, func(context.Context, Job) bool { return true })
	srv := NewServer(ServerConfig{
		SocketPath: path,
		Queue:      queue,
		Logger:     logger,
		StatusFn:   func() StatusResponse { return StatusResponse{} },
	})
	startServer(t, srv)

	client := NewSocketClient(path)
	valid := Job{
		Source:       SourcePrompt,
		Body:         []byte("hi"),
		Mode:         "paste",
		SanitizeMode: "off",
		PaneID:       "%1",
		Verification: VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 50},
	}
	tests := []struct {
		name    string
		mutate  func(*Job)
		wantMsg string
	}{
		{"missing pane id", func(j *Job) { j.PaneID = "" }, "pane_id"},
		{"unknown source", func(j *Job) { j.Source = "garbage" }, "source must be prompt or clipboard"},
		{"unknown mode", func(j *Job) { j.Mode = "spray" }, "mode must be paste or type"},
		{"empty sanitize", func(j *Job) { j.SanitizeMode = "" }, "sanitize_mode must be"},
		{"zero timeout", func(j *Job) { j.Verification.TimeoutMS = 0 }, "timeout_ms must be > 0"},
		{"zero interval", func(j *Job) { j.Verification.PollIntervalMS = 0 }, "poll_interval_ms must be > 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := valid
			tc.mutate(&j)
			_, err := client.Submit(SubmitRequest{Job: j})
			if err == nil {
				t.Fatalf("Submit accepted invalid job")
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantMsg)
			}
			if queue.Pending() != 0 {
				t.Fatalf("Pending = %d, want 0 (invalid job should not be enqueued)", queue.Pending())
			}
		})
	}

	if !strings.Contains(logBuf.String(), "outcome="+OutcomeIPCError) {
		t.Fatalf("expected ipc_error log entries for invalid submits, got: %q", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "reject submit:") {
		t.Fatalf("expected 'reject submit:' in log, got: %q", logBuf.String())
	}
}

func TestRunCancelsAcceptedJobsOnContextCancel(t *testing.T) {
	path := socketPath(t)
	workerCanceled := make(chan struct{})
	srv, queue, _ := newTestServer(t, path, func(ctx context.Context, _ Job) bool {
		<-ctx.Done()
		close(workerCanceled)
		return false
	})

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct {
		result RunResult
		err    error
	}, 1)
	ready := make(chan struct{})
	go func() {
		result, err := Run(ctx, srv, func() { close(ready) })
		runDone <- struct {
			result RunResult
			err    error
		}{result: result, err: err}
	}()

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("server did not signal ready")
	}

	// Submit a long-running job.
	client := NewSocketClient(path)
	if _, err := client.Submit(SubmitRequest{Job: Job{
		JobID:        "j-1",
		Source:       SourcePrompt,
		Body:         []byte("hi"),
		Mode:         "paste",
		SanitizeMode: "off",
		PaneID:       "%5",
		Verification: VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 50},
	}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	cancel()

	select {
	case done := <-runDone:
		if done.err != nil {
			t.Fatalf("Run returned error on shutdown: %v", done.err)
		}
		if !done.result.Started {
			t.Fatal("RunResult.Started = false, want true")
		}
		if done.result.ExitReason != RunExitContextCanceled {
			t.Fatalf("RunResult.ExitReason = %q, want %q", done.result.ExitReason, RunExitContextCanceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	select {
	case <-workerCanceled:
	default:
		t.Fatal("accepted job was not canceled before shutdown returned")
	}
	if queue.Pending() != 0 {
		t.Fatalf("Pending after shutdown = %d, want 0", queue.Pending())
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket should be unlinked after shutdown, stat err = %v", err)
	}
}

func TestRunReturnsListenError(t *testing.T) {
	path := socketPath(t)
	live, _, _ := newTestServer(t, path, nil)
	startServer(t, live)

	contender, _, _ := newTestServer(t, path, nil)
	var readyCalled bool
	_, err := Run(context.Background(), contender, func() { readyCalled = true })
	var sue *SocketUnavailableError
	if !errors.As(err, &sue) {
		t.Fatalf("want SocketUnavailableError from Run, got %T: %v", err, err)
	}
	if readyCalled {
		t.Fatal("onReady must not fire when Listen fails")
	}
}
