package handoff

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/tmux"
)

// RunJob loads a handoff job file, runs the existing deferred-delivery
// executor once, then removes the job file. Delivery failures are logged and
// surfaced by the executor; RunJob only reports setup/read failures.
func RunJob(ctx context.Context, cfg config.Resolved, adapter tmux.Adapter, jobPath string) error {
	job, err := readJob(jobPath)
	if err != nil {
		return err
	}
	logger, err := daemon.NewLogger(cfg.LogPath)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Close() }()

	executor := daemon.NewExecutor(adapter, logger, cfg.MaxPasteBytes)
	executor.EnablePostInjectionVerification(cfg.PostInjectionVerification)
	executor.Run(ctx, job)
	_ = os.Remove(jobPath)
	return nil
}

func readJob(path string) (daemon.Job, error) {
	f, err := os.Open(path) //nolint:gosec // path is provided by tprompt's own handoff client.
	if err != nil {
		return daemon.Job{}, fmt.Errorf("open handoff job: %w", err)
	}
	defer func() { _ = f.Close() }()

	var job daemon.Job
	if err := json.NewDecoder(f).Decode(&job); err != nil {
		return daemon.Job{}, fmt.Errorf("decode handoff job: %w", err)
	}
	if err := daemon.ValidateJob(job); err != nil {
		return daemon.Job{}, err
	}
	return job, nil
}
