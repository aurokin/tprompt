package app

import "github.com/spf13/cobra"

func newListCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available prompts",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
				return err
			}
			return ErrNotImplemented
		},
	}
}

func newShowCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print the body of a prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
				return err
			}
			return ErrNotImplemented
		},
	}
}

func newSendCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "send <id>",
		Short: "Deliver a prompt into a tmux pane synchronously",
		Args:  cobra.ExactArgs(1),
		RunE: func(*cobra.Command, []string) error {
			if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
				return err
			}
			return ErrNotImplemented
		},
	}
}

func newPasteCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "paste",
		Short: "Deliver the host clipboard into a tmux pane synchronously",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
				return err
			}
			return ErrNotImplemented
		},
	}
}

func newDoctorCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose configuration, prompt store, and environment issues",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
				return err
			}
			return ErrNotImplemented
		},
	}
}

func newTUICmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI (typically from a tmux popup)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
				return err
			}
			return ErrNotImplemented
		},
	}
}

func newPickCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "pick",
		Short: "Select a prompt via an external picker (picker_command)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
				return err
			}
			return ErrNotImplemented
		},
	}
}

func newDaemonCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the deferred-delivery daemon",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:     "start",
			Aliases: []string{"run"},
			Short:   "Start the daemon in the foreground",
			Args:    cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
					return err
				}
				return ErrNotImplemented
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Report daemon status",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
					return err
				}
				return ErrNotImplemented
			},
		},
	)
	return cmd
}
