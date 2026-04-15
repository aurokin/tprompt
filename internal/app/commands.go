package app

import "github.com/spf13/cobra"

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available prompts",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return ErrNotImplemented },
	}
}

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print the body of a prompt",
		Args:  cobra.ExactArgs(1),
		RunE:  func(*cobra.Command, []string) error { return ErrNotImplemented },
	}
}

func newSendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "send <id>",
		Short: "Deliver a prompt into a tmux pane synchronously",
		Args:  cobra.ExactArgs(1),
		RunE:  func(*cobra.Command, []string) error { return ErrNotImplemented },
	}
}

func newPasteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "paste",
		Short: "Deliver the host clipboard into a tmux pane synchronously",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return ErrNotImplemented },
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose configuration, prompt store, and environment issues",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return ErrNotImplemented },
	}
}

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI (typically from a tmux popup)",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return ErrNotImplemented },
	}
}

func newPickCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pick",
		Short: "Select a prompt via an external picker (picker_command)",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return ErrNotImplemented },
	}
}

func newDaemonCmd() *cobra.Command {
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
			RunE:    func(*cobra.Command, []string) error { return ErrNotImplemented },
		},
		&cobra.Command{
			Use:   "status",
			Short: "Report daemon status",
			Args:  cobra.NoArgs,
			RunE:  func(*cobra.Command, []string) error { return ErrNotImplemented },
		},
	)
	return cmd
}
