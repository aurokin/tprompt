package handoff

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/tmux"
)

type fakeAdapter struct {
	pastedBody string
	pastedPane string
	enter      bool
	displays   []string
}

func (f *fakeAdapter) CurrentContext() (tmux.TargetContext, error) { return tmux.TargetContext{}, nil }
func (f *fakeAdapter) PaneExists(context.Context, string) (bool, error) {
	return true, nil
}
func (f *fakeAdapter) IsTargetSelected(context.Context, tmux.TargetContext) (bool, error) {
	return true, nil
}
func (f *fakeAdapter) CapturePaneTail(context.Context, string, int) (string, error) { return "", nil }
func (f *fakeAdapter) Paste(_ context.Context, target tmux.TargetContext, body string, enter bool) error {
	f.pastedPane = target.PaneID
	f.pastedBody = body
	f.enter = enter
	return nil
}
func (f *fakeAdapter) Type(context.Context, tmux.TargetContext, string, bool) error { return nil }
func (f *fakeAdapter) DisplayMessage(_ tmux.MessageTarget, msg string) error {
	f.displays = append(f.displays, msg)
	return nil
}

func TestRunJobDeliversAndRemovesJobFile(t *testing.T) {
	dir := t.TempDir()
	jobPath := filepath.Join(dir, "job.json")
	job := daemon.Job{
		JobID:        "h-1",
		Source:       daemon.SourcePrompt,
		PromptID:     "demo",
		Body:         []byte("hello"),
		Mode:         "paste",
		Enter:        true,
		SanitizeMode: "safe",
		PaneID:       "%9",
		Verification: daemon.VerificationPolicy{TimeoutMS: 100, PollIntervalMS: 10},
	}
	writeJobFile(t, jobPath, job)

	adapter := &fakeAdapter{}
	cfg := config.Resolved{LogPath: filepath.Join(dir, "handoff.log"), MaxPasteBytes: 1024}
	if err := RunJob(context.Background(), cfg, adapter, jobPath); err != nil {
		t.Fatalf("RunJob: %v", err)
	}
	if adapter.pastedPane != "%9" || adapter.pastedBody != "hello" || !adapter.enter {
		t.Fatalf("paste = pane %q body %q enter %v", adapter.pastedPane, adapter.pastedBody, adapter.enter)
	}
	if _, err := os.Stat(jobPath); !os.IsNotExist(err) {
		t.Fatalf("job file still exists or stat failed: %v", err)
	}
}

func TestRunJobRejectsMalformedJobBeforeDelivery(t *testing.T) {
	dir := t.TempDir()
	jobPath := filepath.Join(dir, "job.json")
	writeJobFile(t, jobPath, daemon.Job{JobID: "bad"})

	adapter := &fakeAdapter{}
	cfg := config.Resolved{LogPath: filepath.Join(dir, "handoff.log"), MaxPasteBytes: 1024}
	if err := RunJob(context.Background(), cfg, adapter, jobPath); err == nil {
		t.Fatal("RunJob err = nil, want validation error")
	}
	if adapter.pastedBody != "" {
		t.Fatalf("unexpected delivery body %q", adapter.pastedBody)
	}
}

func TestClientRejectsMalformedJobBeforeSpawn(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Resolved{LogPath: filepath.Join(dir, "handoff.log")}
	client := NewClient(cfg, "/bin/false", "")

	_, err := client.Submit(daemon.SubmitRequest{Job: daemon.Job{
		Source:       daemon.SourcePrompt,
		Body:         []byte("hello"),
		Mode:         "paste",
		SanitizeMode: "safe",
		PaneID:       "%9",
		Verification: daemon.VerificationPolicy{TimeoutMS: 0, PollIntervalMS: 10},
	}})
	var ipc *daemon.IPCError
	if !errors.As(err, &ipc) {
		t.Fatalf("Submit err = %T: %v, want IPCError", err, err)
	}
	if ipc.Op != "validate handoff job" {
		t.Fatalf("IPCError.Op = %q, want validate handoff job", ipc.Op)
	}
	entries, err := os.ReadDir(JobsDir(cfg))
	if err == nil && len(entries) != 0 {
		t.Fatalf("job files were written despite validation failure: %v", entries)
	}
}

func writeJobFile(t *testing.T, path string, job daemon.Job) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := json.NewEncoder(f).Encode(job); err != nil {
		t.Fatalf("encode job: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close job: %v", err)
	}
}
