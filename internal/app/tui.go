package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/delivery"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
)

// Flag names for the tui command. Shared as constants so `tprompt init` can
// build the popup binding from the same flag names this command registers,
// keeping the printed binding from drifting away from the real flags.
const (
	flagTargetPane = "target-pane"
	flagClientTTY  = "client-tty"
	flagSessionID  = "session-id"
)

// tuiFlags captures the --target-pane / --client-tty / --session-id inputs.
// Deprecated daemon auto-start flags are still accepted for compatibility but
// no longer affect execution.
type tuiFlags struct {
	targetPane         string
	clientTTY          string
	sessionID          string
	daemonAutoStart    bool
	daemonAutoStartSet bool
	noDaemonAutoStart  bool
}

func newTUICmd(deps Deps) *cobra.Command {
	var f tuiFlags
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI (typically from a tmux popup)",
		Long: `Launch the interactive TUI for prompt selection and clipboard delivery.
Selections spawn a short-lived handoff worker, which waits until this
TUI process exits and then injects into the target pane. This is
distinct from 'send' and 'paste', which deliver synchronously.

Pass --target-pane so the handoff worker knows where to deliver; the target
pane must still exist when the worker injects. Typically invoked from a tmux
popup binding that passes the originating context, e.g.:

  tprompt tui --target-pane '#{pane_id}' --client-tty '#{client_tty}' \
    --session-id '#{session_id}'

Without --target-pane, the TUI falls back to direct mode and delivers to the
current pane — but only when it can confirm the pane is not a popup. If it
cannot (inside a popup, or any ambiguity) it exits with a usage error pointing
at 'tprompt init'.`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runTUI(deps, f)
		},
	}
	cmd.Flags().StringVar(&f.targetPane, flagTargetPane, "",
		"tmux pane ID to deliver into; omit to deliver to the current pane (direct mode, requires a non-popup pane)")
	cmd.Flags().StringVar(&f.clientTTY, flagClientTTY, "", "originating tmux client TTY for failure banners")
	cmd.Flags().StringVar(&f.sessionID, flagSessionID, "", "originating tmux session ID for delivery context")
	cmd.Flags().BoolVar(&f.daemonAutoStart, "daemon-auto-start", true, "deprecated; TUI no longer uses a daemon")
	cmd.Flags().BoolVar(&f.noDaemonAutoStart, "no-daemon-auto-start", false, "deprecated; TUI no longer uses a daemon")
	cmd.PreRun = func(c *cobra.Command, _ []string) {
		f.daemonAutoStartSet = c.Flags().Changed("daemon-auto-start")
	}
	return cmd
}

func runTUI(deps Deps, f tuiFlags) error {
	// Resolve the delivery target first. With --target-pane this is a pure flag
	// mapping that makes no tmux calls; without it, direct mode probes tmux to
	// confirm a non-popup origin pane. Doing this before the config/store/
	// handoff pre-flight means an unresolvable target (e.g. bare `tprompt` in a
	// popup, or `tprompt tui` outside tmux) reports the actionable
	// directModeUnavailable usage error pointing at `tprompt init`, instead of
	// being masked by an unrelated config/handoff failure — matching the
	// pre-AUR-446 required-flag timing. NewTmux is a cheap, infallible
	// construct, so this does not change error ordering for the explicit
	// --target-pane path (a broken config still surfaces first there).
	adapter, err := deps.NewTmux()
	if err != nil {
		return err
	}
	target, banner, err := resolveTUITarget(deps, adapter, f)
	if err != nil {
		return err
	}

	// Pre-flight chain: config → store → handoff client. Each step
	// short-circuits on error so the user sees the most-fundamental broken
	// layer first.
	cfg, err := deps.LoadConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}

	s, err := deps.NewStore(cfg)
	if err != nil {
		return err
	}
	summaries, err := s.List()
	if err != nil {
		return err
	}

	client, err := deps.NewTUIClient(cfg)
	if err != nil {
		return err
	}

	exists, err := adapter.PaneExists(context.Background(), target.PaneID)
	if err != nil {
		return err
	}
	if !exists {
		return &tmux.PaneMissingError{PaneID: target.PaneID}
	}

	state := buildTUIState(summaries, cfg)
	state.Banner = banner
	return renderAndDispatchTUI(deps, cfg, s, client, target, state)
}

// renderAndDispatchTUI builds the Submitter+Renderer, runs the
// Renderer, and translates its terminal action into a runTUI return
// value.
//
// The Submitter is built up front so it can be injected into the
// Renderer. The real Model invokes Submit via a tea.Cmd for
// ActionPrompt and ActionClipboard alike; the stub clipboard Renderer
// (used by TPROMPT_TEST_RENDERER) also calls Submit itself, so runTUI
// never re-submits here regardless of which Renderer ran. Clipboard-
// reader construction is deferred to the production branch inside
// ProductionDeps.NewRenderer so stub-renderer testscripts don't
// regress on hosts without a clipboard tool.
func renderAndDispatchTUI(deps Deps, cfg config.Resolved, s store.Store, client delivery.Client, target tmux.TargetContext, state tui.State) error {
	sub := deps.NewSubmitter(cfg, s, client, target)
	renderer, err := deps.NewRenderer(cfg, s, sub)
	if err != nil {
		return err
	}
	result, err := renderer.Run(state)
	if err != nil {
		return err
	}
	switch result.Action {
	case tui.ActionCancel:
		return nil
	case tui.ActionPrompt, tui.ActionClipboard:
		// Submit was performed inside the Renderer (real Model via tea.Cmd,
		// or the staticClipboardRenderer stub). Any error already surfaced
		// via renderer.Run above, so nothing to do here.
		return nil
	default:
		return fmt.Errorf("tui: unknown renderer action %q", result.Action)
	}
}

// buildTUIState assembles the State the Renderer sees: pinned clipboard row,
// board rows (explicit keybinds first, then auto-assigned, each alphabetical
// by ID), overflow rows, and the reserved-key map.
func buildTUIState(summaries []store.Summary, cfg config.Resolved) tui.State {
	reserved := reservedKeys(cfg)
	var explicit, auto, overflow []tui.Row
	for _, sum := range summaries {
		row := tui.Row{
			PromptID:    sum.ID,
			Scope:       promptScope(sum),
			Title:       sum.Title,
			Description: sum.Description,
			Tags:        sum.Tags,
			Shadowed:    sum.Shadowed,
		}
		if sum.Key != "" && !sum.Shadowed {
			row.Key = []rune(sum.Key)[0]
			if sum.KeySource == store.KeySourceAuto {
				auto = append(auto, row)
			} else {
				explicit = append(explicit, row)
			}
			continue
		}
		overflow = append(overflow, row)
	}
	// store.List() already returns summaries sorted by ID; the split preserves
	// that order within each group. Explicitly keybound prompts render above
	// auto-assigned ones so user-chosen shortcuts stay at the top of the board.

	rows := make([]tui.Row, 0, len(explicit)+len(auto)+1)
	if clipKey, ok := clipboardKey(reserved); ok {
		rows = append(rows, tui.Row{
			Key:         clipKey,
			Description: "(read on select)",
		})
	}
	rows = append(rows, explicit...)
	rows = append(rows, auto...)

	return tui.State{
		Rows:               rows,
		Overflow:           overflow,
		Reserved:           reserved,
		ClipboardAvailable: true,
	}
}

func reservedKeys(cfg config.Resolved) tui.ReservedKeys {
	return tui.ReservedKeys{
		Clipboard: reservedBinding("clipboard", cfg),
		Search:    reservedBinding("search", cfg),
		Cancel:    reservedBinding("cancel", cfg),
		Select:    reservedBinding("select", cfg),
	}
}

func reservedBinding(role string, cfg config.Resolved) tui.ReservedBinding {
	if symbolic, ok := cfg.ReservedSymbolic[role]; ok {
		return tui.ReservedBinding{Symbolic: symbolic}
	}
	for r, gotRole := range cfg.ReservedPrintable {
		if gotRole == role {
			return tui.ReservedBinding{Printable: r}
		}
	}
	return tui.ReservedBinding{Disabled: true}
}

// clipboardKey finds the reserved printable rune assigned to the clipboard
// action. Returns ok=false if the clipboard key is disabled or symbolic in
// config; the current board row format only supports printable clipboard keys.
func clipboardKey(reserved tui.ReservedKeys) (rune, bool) {
	if reserved.Clipboard.Disabled || reserved.Clipboard.Printable == 0 {
		return 0, false
	}
	return reserved.Clipboard.Printable, true
}

func buildTUITarget(f tuiFlags) tmux.TargetContext {
	return tmux.TargetContext{
		PaneID:    f.targetPane,
		ClientTTY: f.clientTTY,
		Session:   f.sessionID,
	}
}

// directModeBanner nudges the user toward the optimal popup flow when the TUI
// runs in direct mode (delivering straight to the current pane).
const directModeBanner = "No popup detected — delivering to the current pane. Set up the popup binding for the full flow: tprompt init"

// directModeUnavailableError is returned when the TUI runs without
// --target-pane and direct mode cannot be confirmed safe: $TMUX_PANE is unset,
// not present in `tmux list-panes -a` (e.g. a popup or nested/control-mode
// pane), or the query failed. It maps to ExitUsage. The message is
// self-contained (points at `tprompt init`) because, unlike a cobra
// required-flag error, it does not get RunCLI's `--help` pointer line.
type directModeUnavailableError struct{}

func (directModeUnavailableError) Error() string {
	return "no target pane: run 'tprompt init' to set up the tmux popup binding, or pass --target-pane <pane-id>"
}

// paneLister is the narrow consumer-side view of *tmux.Exec.ListPanes that
// direct-mode resolution needs. Kept off the tmux.Adapter interface (like
// bindingLister for doctor) so the Adapter fakes need not grow a method only
// this path uses; recovered from the adapter by type assertion.
type paneLister interface {
	ListPanes(ctx context.Context) ([]string, error)
}

// resolveTUITarget picks the delivery target and any header banner for the TUI.
//
// With an explicit --target-pane (the canonical popup binding path) it maps the
// flags straight through with no banner, so that path carries zero new risk.
//
// Without one, it attempts direct mode: deliver to the current pane, but ONLY
// when it can confirm the pane is not a popup. Inside a `display-popup -E`
// popup, $TMUX_PANE is the popup's own pane, not the origin (DECISIONS §30), so
// direct mode requires $TMUX_PANE to resolve AND to appear in
// `tmux list-panes -a`. On ANY uncertainty (unset env, an adapter without the
// query, a query error, pane absent, or a failing context probe) it returns
// directModeUnavailableError (exit 2). Direct mode is opt-in-by-confidence,
// never a guess.
func resolveTUITarget(deps Deps, adapter tmux.Adapter, f tuiFlags) (tmux.TargetContext, string, error) {
	if f.targetPane != "" {
		return buildTUITarget(f), "", nil
	}
	paneID := strings.TrimSpace(deps.Env("TMUX_PANE"))
	if paneID == "" {
		return tmux.TargetContext{}, "", directModeUnavailableError{}
	}
	lister, ok := adapter.(paneLister)
	if !ok {
		return tmux.TargetContext{}, "", directModeUnavailableError{}
	}
	panes, err := lister.ListPanes(context.Background())
	if err != nil || !slices.Contains(panes, paneID) {
		return tmux.TargetContext{}, "", directModeUnavailableError{}
	}
	cur, err := adapter.CurrentContext()
	if err != nil {
		return tmux.TargetContext{}, "", directModeUnavailableError{}
	}
	// Trust $TMUX_PANE as the origin pane and take session/window/client-tty
	// from the current context. Outside a popup these agree; pinning PaneID
	// makes the intent explicit.
	cur.PaneID = paneID
	return cur, directModeBanner, nil
}
