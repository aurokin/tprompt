// Package app wires the CLI. It owns the cobra command tree and the default
// no-args dispatch (DECISIONS.md §30).
package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
	cmd.SetArgs(dispatchArgs(cmd, args, deps.Env, stdinIsTTY,
		func() bool { return streamIsTTY(stdin) && streamIsTTY(stdout) },
		defaultImportSourceName()))

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

// dispatchArgs implements the DECISIONS.md §30 default-subcommand rule and its
// §34 import analog: when stdin is a tty and $TMUX is set and the user has not
// named a subcommand, the invocation is rewritten so the default interactive
// flow runs. Bare `tprompt` → `tui`; bare `tprompt import` (the import parent,
// no source) → `import <importDefault> -i`. This happens before cobra parses
// flags so `tui`'s required --target-pane validation (and `-i`'s tty preflight)
// fire normally. importDefault is the default import source name (empty disables
// the import rewrite); it and the tty predicates are passed in rather than read
// from globals so this stays a pure function, matching the §30 shape.
//
// interactiveTTY gates the import rewrite only and mirrors the picker's own
// preflight (NewImportRenderer requires streamIsTTY on both the injected stdin
// AND stdout): it reports whether the interactive import can actually run for
// this invocation's streams. Forcing `-i` when it cannot — stdout redirected
// (`tprompt import >out.txt` in tmux), or an embedded caller injecting a non-tty
// stdin while the process stdin is a terminal — would turn §34's documented "bare
// form prints help" into an InteractiveRequiresTTYError. The §30 `tui` path takes
// no equivalent check: it has no such preflight and is a locked decision, keying
// off the process-global stdinTTY before command streams are threaded.
func dispatchArgs(root *cobra.Command, args []string, env func(string) string, stdinTTY, interactiveTTY func() bool, importDefault string) []string {
	if env("TMUX") == "" || !stdinTTY() {
		return args
	}
	matched, remaining, err := root.Find(args)
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
		// intentionally not distinguished. This guard is scoped to the root path:
		// the import rewrite below needs no equivalent because bareImportArgs reads
		// `-h`/`-v` correctly as a root flag's value, not a help request.
		for _, a := range args {
			if a == "--help" || a == "-h" || a == "--version" || a == "-v" {
				return args
			}
		}
		return append([]string{"tui"}, args...)
	}
	// Bare `tprompt import` (the import parent matched, with nothing of the user's
	// own passed to it) → the default source's interactive picker. `remaining` is
	// what cobra's Find left after locating the import command; bareImportArgs
	// accepts it only when it holds nothing but root persistent flags. So
	// `--config x import` and `import --config x` both rewrite (flag position is
	// irrelevant), while `import --dry-run` (foreign flag), `import bogus`, and
	// `import import` (positionals) all leave a disqualifying token and pass
	// through unrewritten — crucially, rewriting `--dry-run` would inject `-i` and
	// trip the interactive/dry-run conflict. An explicit `tprompt import wispr`
	// matches the wispr command (matched.Name() != the parent), so it is never
	// rewritten; outside tmux/tty the early return above leaves bare `import` to
	// print help. The interactiveTTY guard keeps a bare `import` whose streams
	// cannot host the picker on §34's help path instead of forcing `-i` into a
	// non-tty renderer.
	if importDefault != "" && interactiveTTY() && matched.Name() == "import" && matched.Parent() == root && bareImportArgs(root, remaining) {
		return appendImportSource(args, importDefault)
	}
	return args
}

// bareImportArgs reports whether `remaining` — the tokens cobra's Find left after
// locating the import parent — carries nothing of the user's own beyond root
// persistent flags (e.g. `--config` and its value). Those invocations dispatch
// to the default source's interactive picker regardless of where the root flag
// sits relative to `import`. A positional (a source name or stray arg) or a flag
// that is not a root persistent flag (e.g. the source-level `--dry-run`)
// disqualifies, leaving the invocation for cobra to route or reject. It mirrors
// pflag's own splitting (long `--name[=value]`, shorthand `-x[=value]`, and a
// value-taking flag consuming the next token) without parsing into — and thus
// mutating — the live command's bound flag variables.
func bareImportArgs(root *cobra.Command, remaining []string) bool {
	for i := 0; i < len(remaining); i++ {
		consumesValue, ok := rootFlagToken(root, remaining[i])
		if !ok {
			return false
		}
		if consumesValue {
			// A value-taking flag claims the next token. If none follows, the
			// invocation is malformed (`import --config` with no value); leave it
			// unrewritten so cobra reports the missing-value error rather than
			// binding the appended source token as the flag's value.
			if i+1 >= len(remaining) {
				return false
			}
			i++
		}
	}
	return true
}

// rootFlagToken classifies a single leftover-arg token. ok is false when the
// token is a positional or names a flag that is not a root persistent flag —
// either disqualifies "bare import". When ok, consumesValue reports whether the
// flag takes its value from the following token (`--name value` / `-x value`, as
// opposed to the self-contained `--name=value` or a value-optional flag).
func rootFlagToken(root *cobra.Command, tok string) (consumesValue, ok bool) {
	pf := root.PersistentFlags()
	switch {
	case strings.HasPrefix(tok, "--"):
		name, _, hasEq := strings.Cut(tok[2:], "=")
		f := pf.Lookup(name)
		if f == nil {
			return false, false
		}
		return f.NoOptDefVal == "" && !hasEq, true
	case strings.HasPrefix(tok, "-") && len(tok) > 1:
		name, _, hasEq := strings.Cut(tok[1:], "=")
		if name == "" {
			return false, false
		}
		f := pf.ShorthandLookup(name[:1])
		if f == nil {
			return false, false
		}
		return f.NoOptDefVal == "" && !hasEq, true
	default:
		return false, false // positional (source name, stray arg, or "-")
	}
}

// appendImportSource rewrites a bare-`import` invocation to `… import <source>
// -i` by appending the source token and `-i`. The caller guarantees (via
// bareImportArgs) that the user passed nothing of their own to import, so
// appending the source as the final command token is correct even when a root
// flag value happens to be the literal "import" (e.g. `--config import import` →
// `--config import import wispr -i`, which cobra re-parses correctly). A fresh
// slice is returned so the caller's args array is never aliased.
func appendImportSource(args []string, source string) []string {
	out := make([]string, 0, len(args)+2)
	out = append(out, args...)
	return append(out, source, "-i")
}
