package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hsadler/tprompt/internal/handoff"
)

func newHandoffCmd(deps Deps) *cobra.Command {
	var jobPath string
	cmd := &cobra.Command{
		Use:    "handoff",
		Short:  "Run a deferred TUI handoff job",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := deps.LoadHandoffConfig(*deps.ConfigPath)
			if err != nil {
				return err
			}
			adapter, err := deps.NewTmux()
			if err != nil {
				return err
			}
			return handoff.RunJob(context.Background(), cfg, adapter, jobPath)
		},
	}
	cmd.Flags().StringVar(&jobPath, "job", "", "handoff job file")
	// MarkFlagRequired only rejects a *missing* --job (→ usage exit 2), not an
	// explicitly-empty `--job=`. That gap is intentional: handoff is a hidden,
	// internal command and its sole caller (the handoff client) always spawns it
	// with a real, non-empty path (internal/handoff/client.go: args are
	// ["handoff", "--job", <temp job file>], after validating job.JobID != "").
	// An empty value is unreachable in production; were it ever passed it would
	// surface a clear readJob error rather than silently misbehave.
	if err := cmd.MarkFlagRequired("job"); err != nil {
		panic(fmt.Sprintf("handoff: mark --job required: %v", err))
	}
	return cmd
}
