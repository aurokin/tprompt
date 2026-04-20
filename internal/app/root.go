// Package app wires the CLI. It owns the cobra command tree and the default
// no-args dispatch (DECISIONS.md §29).
package app

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ErrNotImplemented is returned by command handlers that have not yet been
// wired to their subsystem.
var ErrNotImplemented = errors.New("not implemented")

// stdinIsTTY reports whether stdin is a terminal. Package-level so tests can
// swap it without relying on the test runner's stdin.
var stdinIsTTY = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// NewRootCmd builds the root cobra command with all subcommands registered.
func NewRootCmd(deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           "tprompt",
		Short:         "Deliver markdown prompts into tmux panes",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Bare `tprompt` outside tmux+tty: help is the MVP behavior.
			// Bare dispatch to `tui` in tmux+tty is handled by dispatchArgs in
			// RunCLI (see DECISIONS.md §29) so cobra flag parsing applies to
			// `tui`'s required --target-pane.
			return cmd.Help()
		},
	}

	var configPath string
	deps.ConfigPath = &configPath
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config file")

	root.AddCommand(
		newListCmd(deps),
		newShowCmd(deps),
		newSendCmd(deps),
		newPasteCmd(deps),
		newDoctorCmd(deps),
		newTUICmd(deps),
		newPickCmd(deps),
		newDaemonCmd(deps),
	)

	return root
}

// RunCLI is the top-level entry point called from main. It builds the command
// tree, executes, and returns the process exit code.
func RunCLI(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	deps := ProductionDeps(stdout, stderr, stdin)
	cmd := NewRootCmd(deps)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(dispatchArgs(cmd, args, deps.Env, stdinIsTTY))

	err := cmd.Execute()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "tprompt - error: %s\n", err.Error())
		return ExitCode(err)
	}
	return ExitOK
}

// dispatchArgs implements the DECISIONS.md §29 default-subcommand rule: when
// stdin is a tty and $TMUX is set and the user has not named a subcommand, the
// invocation is rewritten to run `tui`. This happens before cobra parses flags
// so `tui`'s required --target-pane validation fires normally.
func dispatchArgs(root *cobra.Command, args []string, env func(string) string, stdinTTY func() bool) []string {
	if env("TMUX") == "" || !stdinTTY() {
		return args
	}
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "help" {
			return args
		}
	}
	matched, _, err := root.Find(args)
	if err != nil || matched != root {
		return args
	}
	return append([]string{"tui"}, args...)
}
