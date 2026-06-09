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

// stdinIsTTY reports whether the process stdin is a terminal. Used by the bare
// `tprompt` → `tui` dispatch (dispatchArgs), which decides before command
// streams are threaded. Package-level so tests can swap it.
var stdinIsTTY = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// streamIsTTY reports whether a command stream (one of Deps.Stdin/Stdout/Stderr)
// is backed by a terminal. It inspects the injected stream itself rather than
// the process-global FD, so behavior keys off the command's *logical* I/O: a
// caller that redirects RunCLI's streams gets the documented no-op even with a
// tty attached to the process, and unit tests that inject a *bytes.Buffer
// deterministically get false. tty-gated, human-facing behavior — the add-a-
// body hint and the `new --edit` editor launch — uses this so it never pollutes
// piped stdout or the golden testscripts' empty-stderr assertions. Accepts any
// so it serves both io.Reader (stdin) and io.Writer (stdout/stderr); package-
// level so tests can swap it.
var streamIsTTY = func(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
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
		newImportCmd(deps),
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

	executedCmd, err := cmd.ExecuteC()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "tprompt - error: %s\n", err.Error())
		code := ExitCode(err)
		// Flag/arg parse errors print only the one-line error (SilenceUsage
		// suppresses cobra's usage block). Add a one-line --help pointer so
		// the user can recover without dumping full usage. Require BOTH the
		// usage exit code AND a cobra-usage string match: a typed error that
		// ExitCode classifies as prompt/clipboard/etc. (code != ExitUsage)
		// must not get a misleading flag-help pointer even if its message
		// coincidentally matches a broad isCobraUsageError pattern.
		if code == ExitUsage && isCobraUsageError(err) {
			path := "tprompt"
			if executedCmd != nil {
				path = executedCmd.CommandPath()
			}
			_, _ = fmt.Fprintf(stderr, "run '%s --help' for usage.\n", path)
		}
		return code
	}
	return ExitOK
}

// dispatchArgs implements the DECISIONS.md §30 default-subcommand rule: when
// process stdin is a tty, $TMUX is set, and the user has not named a subcommand,
// bare `tprompt` is rewritten to `tui` before cobra parses flags so `tui`'s
// required --target-pane validation applies. The import parent handles its own
// bare default after cobra parsing, so this function stays scoped to root
// dispatch and does not mirror pflag tokenization.
func dispatchArgs(root *cobra.Command, args []string, env func(string) string, stdinTTY func() bool) []string {
	if env("TMUX") == "" || !stdinTTY() {
		return args
	}
	matched, _, err := root.Find(args)
	if err != nil {
		return args
	}
	if matched == root {
		// Bare root → `tui`, except when the user asked for root help/version:
		// prepending `tui` would route `--help` to tui's help (wrong output) and
		// `--version` to tui (which rejects it as an unknown flag). `-v` is the
		// shorthand cobra binds for --version (InitDefaultVersionFlag, since -v is
		// otherwise free at root — verified by version_flag.txtar). We match the
		// canonical bare spellings only; degenerate forms such as `--version=true`,
		// or a `--config -h` whose `-h` is merely the config value, are
		// intentionally not distinguished.
		for _, a := range args {
			if a == "--help" || a == "-h" || a == "--version" || a == "-v" {
				return args
			}
		}
		return append([]string{"tui"}, args...)
	}
	return args
}
