package handoff

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/aurokin/tprompt/internal/config"
	"github.com/aurokin/tprompt/internal/delivery"
)

// Client accepts TUI jobs by writing them to disk and spawning a short-lived
// worker process that outlives the popup-hosted TUI.
type Client struct {
	cfg        config.Resolved
	executable string
	configPath string
}

// NewClient returns a handoff client that spawns the current executable.
func NewClient(cfg config.Resolved, executable, configPath string) *Client {
	return &Client{cfg: cfg, executable: executable, configPath: configPath}
}

// Submit implements the delivery.Client Submit method used by the TUI submitter.
func (c *Client) Submit(req delivery.SubmitRequest) (delivery.SubmitResponse, error) {
	if err := ValidateConfig(c.cfg); err != nil {
		return delivery.SubmitResponse{}, &delivery.IPCError{Op: "validate handoff config", Reason: err.Error(), Err: err}
	}
	if c.executable == "" {
		return delivery.SubmitResponse{}, &delivery.IPCError{Op: "handoff", Reason: "empty executable path"}
	}
	job := req.Job
	if job.JobID == "" {
		job.JobID = nextJobID()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	if err := delivery.ValidateJob(job); err != nil {
		return delivery.SubmitResponse{}, &delivery.IPCError{Op: "validate handoff job", Reason: err.Error(), Err: err}
	}
	path, err := c.writeJob(job)
	if err != nil {
		return delivery.SubmitResponse{}, &delivery.IPCError{Op: "write handoff job", Reason: err.Error(), Err: err}
	}
	if err := c.spawn(path); err != nil {
		_ = os.Remove(path)
		return delivery.SubmitResponse{}, &delivery.IPCError{Op: "spawn handoff", Reason: err.Error(), Err: err}
	}
	return delivery.SubmitResponse{Accepted: true, JobID: job.JobID}, nil
}

func (c *Client) writeJob(job delivery.Job) (string, error) {
	dir := JobsDir(c.cfg)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".job-*.json")
	if err != nil {
		return "", fmt.Errorf("create temp job: %w", err)
	}
	tmpPath := tmp.Name()
	encErr := json.NewEncoder(tmp).Encode(job)
	closeErr := tmp.Close()
	if encErr != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("encode job: %w", encErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close job: %w", closeErr)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("chmod job: %w", err)
	}
	finalPath := filepath.Join(dir, job.JobID+".json")
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("install job: %w", err)
	}
	return finalPath, nil
}

func (c *Client) spawn(jobPath string) error {
	args := []string{"handoff", "--job", jobPath}
	if c.configPath != "" {
		args = append(args, "--config", c.configPath)
	}
	cmd := exec.Command(c.executable, args...) //nolint:gosec // executable is resolved from os.Executable; job path is an internal temp file.
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	if c.cfg.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(c.cfg.LogPath), 0o700); err != nil {
			return fmt.Errorf("mkdir log dir: %w", err)
		}
		f, err := os.OpenFile(c.cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open log: %w", err)
		}
		defer func() { _ = f.Close() }()
		cmd.Stderr = f
	} else {
		cmd.Stderr = io.Discard
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// ValidateConfig checks the config fields required by the TUI handoff path.
func ValidateConfig(cfg config.Resolved) error {
	if cfg.LogPath == "" {
		return &config.ValidationError{Field: "log_path", Message: "must be set"}
	}
	if cfg.MaxPasteBytes <= 0 {
		return &config.ValidationError{
			Field:   "max_paste_bytes",
			Message: fmt.Sprintf("must be positive, got %d", cfg.MaxPasteBytes),
		}
	}
	return nil
}

func nextJobID() string {
	return fmt.Sprintf("h-%d-%d", time.Now().UnixNano(), os.Getpid())
}

// JobsDir returns the per-user directory for transient handoff payloads.
func JobsDir(cfg config.Resolved) string {
	if cfg.LogPath != "" {
		return filepath.Join(filepath.Dir(cfg.LogPath), "jobs")
	}
	return filepath.Join(os.TempDir(), "tprompt", "jobs")
}
