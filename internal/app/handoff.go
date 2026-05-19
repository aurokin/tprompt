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
			if jobPath == "" {
				return fmt.Errorf("handoff: --job is required")
			}
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
	return cmd
}
