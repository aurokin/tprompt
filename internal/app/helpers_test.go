package app

import (
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/daemon"
)

func strPtr(s string) *string { return &s }

func assertPrefix(t *testing.T, line, prefix string) {
	t.Helper()
	if !strings.HasPrefix(line, prefix) {
		t.Errorf("want line to start with %q, got %q", prefix, line)
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("want %q to contain %q", s, substr)
	}
}

type fakeDaemonClient struct {
	submitFn func(daemon.SubmitRequest) (daemon.SubmitResponse, error)
	statusFn func() (daemon.StatusResponse, error)
	stopFn   func() (daemon.StopResponse, error)
}

func (f *fakeDaemonClient) Submit(req daemon.SubmitRequest) (daemon.SubmitResponse, error) {
	if f.submitFn == nil {
		return daemon.SubmitResponse{Accepted: true, JobID: "job-1"}, nil
	}
	return f.submitFn(req)
}

func (f *fakeDaemonClient) Status() (daemon.StatusResponse, error) {
	if f.statusFn == nil {
		return daemon.StatusResponse{}, nil
	}
	return f.statusFn()
}

func (f *fakeDaemonClient) Stop() (daemon.StopResponse, error) {
	if f.stopFn == nil {
		return daemon.StopResponse{Accepted: true}, nil
	}
	return f.stopFn()
}
