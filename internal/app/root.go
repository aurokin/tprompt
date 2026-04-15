// Package app wires the CLI. It owns the cobra command tree and the default
// no-args dispatch (DECISIONS.md §29).
package app

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ErrNotImplemented is returned by command handlers that have not yet been
// wired to their subsystem. Phase 0 scaffolding returns this from every
// subcommand.
var ErrNotImplemented = errors.New("not implemented")

// stdinIsTTY reports whether stdin is a terminal. Package-level so tests can
// swap it without relying on the test runner's stdin.
var stdinIsTTY = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// NewRootCmd builds the root cobra command with all subcommands registered.
func NewRootCmd() *cobra.Command {
	tuiCmd := newTUICmd()
	root := &cobra.Command{
		Use:          "tprompt",
		Short:        "Deliver markdown prompts into tmux panes",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if os.Getenv("TMUX") != "" && stdinIsTTY() {
				return tuiCmd.RunE(tuiCmd, nil)
			}
			return cmd.Help()
		},
	}

	root.AddCommand(
		newListCmd(),
		newShowCmd(),
		newSendCmd(),
		newPasteCmd(),
		newDoctorCmd(),
		tuiCmd,
		newPickCmd(),
		newDaemonCmd(),
	)

	return root
}
