// Package app wires the CLI. It owns the cobra command tree and the default
// no-args dispatch (DECISIONS.md §30).
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
		Use: "tprompt",
		// Cobra registers both --version and the -v shorthand from this field
		// (InitDefaultVersionFlag binds -v because it is free at root). Both
		// forms are exercised by cmd/tprompt/testdata/script/version_flag.txtar.
		Version: appVersion,
		Short:   "Deliver markdown prompts into tmux panes",
		Long: `tprompt delivers markdown prompts into tmux panes. Pick a workflow:

  new <id>    Scaffold a new prompt file globally or with --project.
  send <id>   Direct synchronous delivery of a prompt by ID.
  paste       Direct synchronous delivery of the host clipboard.
  pick        Print a prompt ID chosen via an external picker (no delivery).
  tui         Interactive TUI; selections spawn a short-lived handoff worker
              after the TUI exits.

Inside tmux with a tty, bare 'tprompt' dispatches to 'tprompt tui'. Outside
tmux (or without a tty), bare 'tprompt' prints this help.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Bare `tprompt` outside tmux+tty: help is the MVP behavior.
			// Bare dispatch to `tui` in tmux+tty is handled by dispatchArgs in
			// RunCLI (see DECISIONS.md §30) so cobra flag parsing applies to
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
		newHandoffCmd(deps),
		newPickCmd(deps),
		newNewCmd(deps),
		newInitCmd(deps),
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

// dispatchArgs implements the DECISIONS.md §30 default-subcommand rule: when
// stdin is a tty and $TMUX is set and the user has not named a subcommand, the
// invocation is rewritten to run `tui`. This happens before cobra parses flags
// so `tui`'s required --target-pane validation fires normally.
func dispatchArgs(root *cobra.Command, args []string, env func(string) string, stdinTTY func() bool) []string {
	if env("TMUX") == "" || !stdinTTY() {
		return args
	}
	// Preserve root help/version output for those explicit flags. Matching on
	// the literal string "help" is unsafe — it can appear as a flag value such
	// as `--config help` — so rely on Find for the help-subcommand case. The
	// version flags must short-circuit too: otherwise a bare `tprompt
	// --version` inside tmux+tty would be rewritten to `tui --version`, which
	// `tui` rejects as an unknown flag. `-v` is the shorthand cobra binds for
	// --version (InitDefaultVersionFlag, since -v is otherwise free at root —
	// verified by version_flag.txtar). We match the canonical bare forms only,
	// mirroring the --help/-h handling above; degenerate spellings such as
	// `--version=true` are intentionally not special-cased.
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "--version" || a == "-v" {
			return args
		}
	}
	matched, _, err := root.Find(args)
	if err != nil || matched != root {
		return args
	}
	return append([]string{"tui"}, args...)
}
