package app

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// binaryName is the command name used in the printed tmux bindings. It is the
// literal "tprompt" — the canonical on-PATH command (DECISIONS.md §30,
// examples/tmux-bindings.md) — deliberately not os.Args[0]/os.Executable(): a
// tmux config persists, so the binding must reference the stable PATH command,
// not a `go run` temp path or a version-specific install path.
const binaryName = "tprompt"

// defaultPopupKey is the tmux key bound to the popup when --key is not given.
const defaultPopupKey = "P"

func newInitCmd(deps Deps) *cobra.Command {
	var (
		more    bool
		snippet bool
		key     string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Print tmux setup instructions for the popup workflow",
		Long: `Init prints the tmux configuration needed to launch tprompt's popup
workflow. It never edits your tmux config or touches tmux — it only prints,
so you stay in control of your own files.

By default it prints the popup binding plus the two steps to install it. Use
--more for extra bindings and options, or --snippet to print just the raw
binding line (for example, to append to a config file).`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runInit(deps, initOptions{more: more, snippet: snippet, key: key})
		},
	}
	cmd.Flags().BoolVar(&more, "more", false, "print the full setup menu: extra bindings, customization, and troubleshooting")
	cmd.Flags().BoolVar(&snippet, "snippet", false, "print only the raw tmux binding line(s), e.g. to append to a config file")
	cmd.Flags().StringVar(&key, "key", defaultPopupKey, "tmux key to bind the popup to")
	// MarkFlagsMutuallyExclusive is a void cobra API (returns nothing), so
	// there is no error to check here.
	cmd.MarkFlagsMutuallyExclusive("more", "snippet")
	return cmd
}

type initOptions struct {
	more    bool
	snippet bool
	key     string
}

// runInit prints the canonical, config-less tmux bindings (DECISIONS.md §30).
// init never loads config and the printed binding deliberately omits --config:
// the binding must be portable across panes/machines, and embedding an arbitrary
// path across tmux's and /bin/sh's nested quoting is fragile. A user with a
// non-default config adds --config to the printed line themselves.
func runInit(deps Deps, opts initOptions) error {
	if err := validatePopupKey(opts.key); err != nil {
		return err
	}
	w := deps.Stdout
	switch {
	case opts.snippet:
		writeInitSnippet(w, opts.key)
	case opts.more:
		writeInitMore(w, opts.key)
	default:
		writeInitDefault(w, opts.key)
	}
	return nil
}

// InvalidKeyError reports a `tprompt init --key` value that is not a single
// ASCII letter or digit. It maps to exit code 2 (usage) via ExitCode. A
// dedicated type — rather than config.ValidationError — keeps the message free
// of the "config:" prefix, which would misattribute a CLI flag error to the
// config file.
type InvalidKeyError struct {
	Key string
}

func (e *InvalidKeyError) Error() string {
	return fmt.Sprintf("invalid --key %q: must be a single letter or digit", e.Key)
}

// validatePopupKey keeps the binding key to a single ASCII letter or digit.
// Surfaced as a usage error (exit 2) via ExitCode.
func validatePopupKey(key string) error {
	runes := []rune(key)
	if len(runes) != 1 || !isSafeBindKey(runes[0]) {
		return &InvalidKeyError{Key: key}
	}
	return nil
}

// isSafeBindKey restricts --key to ASCII letters and digits. Other printable
// runes interpolate into `bind-key <key> ...` as something tmux misparses —
// '#' starts a comment, a space splits the command, quotes break the wrapper —
// so the generated binding would silently fail for the user.
func isSafeBindKey(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// popupBinding builds the canonical tmux popup binding (DECISIONS.md §30). The
// flag names come from the same constants the `tui` command registers, so the
// printed binding can never drift from the binary's real flags.
func popupBinding(key string) string {
	return fmt.Sprintf(
		`bind-key %s display-popup -E "%s tui --%s '#{pane_id}' --%s '#{client_tty}' --%s '#{session_id}'"`,
		key, binaryName, flagTargetPane, flagClientTTY, flagSessionID,
	)
}

func pasteBinding(key string) string {
	return fmt.Sprintf(`bind-key %s run-shell "%s paste --%s '#{pane_id}'"`, key, binaryName, flagTargetPane)
}

// sendBinding emits the direct-send binding with flags before the id and a "--"
// end-of-flags marker, so a prompt id that starts with "-" (validateNewID allows
// it) is parsed as the positional id rather than a flag. The id is single-quoted
// because ids may contain spaces or other shell metacharacters — validateNewID
// only bars path separators, leading dots, the .md suffix, and non-printables —
// and tmux runs the run-shell string via /bin/sh. The id here is the fixed
// PROMPT_ID placeholder the user replaces; keeping a substituted id shell-safe
// (e.g. avoiding embedded quotes) is then the user's responsibility.
func sendBinding(key, id string) string {
	return fmt.Sprintf(`bind-key %s run-shell "%s send --%s '#{pane_id}' -- '%s'"`, key, binaryName, flagTargetPane, id)
}

func writeInitSnippet(w io.Writer, key string) {
	_, _ = fmt.Fprintln(w, popupBinding(key))
}

func writeInitDefault(w io.Writer, key string) {
	_, _ = fmt.Fprintf(w, `# tprompt tmux setup

tprompt's main workflow opens the TUI in a tmux popup, then injects your
selection into the pane you started from. Wire it up in two steps:

1. Add this line to your tmux config (~/.tmux.conf or ~/.config/tmux/tmux.conf):

       %s

2. Reload tmux so the binding takes effect — source the file you edited, e.g.:

       tmux source-file ~/.tmux.conf

Then press your tmux prefix followed by %q to open the prompt popup.

For more bindings and options:  %s init --more
To print just the binding:      %s init --snippet
`, popupBinding(key), key, binaryName, binaryName)
}

func writeInitMore(w io.Writer, key string) {
	_, _ = fmt.Fprintf(w, `# tprompt tmux setup (full menu)

## 1. Popup workflow (recommended)

Add to your tmux config (~/.tmux.conf or ~/.config/tmux/tmux.conf), then reload
that file with "tmux source-file" (use whichever path you edited):

       %s

The -E flag is required: it tears the popup down when the TUI exits, which is
what lets the handoff worker inject into your original pane. Without -E,
delivery silently breaks.

## 2. Direct bindings (no popup, no TUI)

These deliver synchronously to the current pane. Replace PROMPT_ID with one of
your prompt ids (run "%s list" to see them):

       %s
       %s

Append --enter to the paste binding to deliver and submit in one keypress.

## 3. Customize the key

Pass --key to bind the popup to a different key (default %q):

       %s init --key g

## 4. Verify your setup

       %s doctor

Note: this command only prints — it never edits your tmux config.
`,
		popupBinding(key),
		binaryName,
		pasteBinding("V"),
		sendBinding("R", "PROMPT_ID"),
		defaultPopupKey,
		binaryName,
		binaryName,
	)
}
