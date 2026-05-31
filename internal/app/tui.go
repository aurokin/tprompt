package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/delivery"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
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

--target-pane is required so the handoff worker knows where to deliver.
The target pane must still exist when the worker injects. Typically invoked
from a tmux popup binding that passes the originating context, e.g.:

  tprompt tui --target-pane '#{pane_id}' --client-tty '#{client_tty}' \
    --session-id '#{session_id}'`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runTUI(deps, f)
		},
	}
	cmd.Flags().StringVar(&f.targetPane, "target-pane", "", "tmux pane ID to deliver into (required)")
	cmd.Flags().StringVar(&f.clientTTY, "client-tty", "", "originating tmux client TTY for failure banners")
	cmd.Flags().StringVar(&f.sessionID, "session-id", "", "originating tmux session ID for delivery context")
	cmd.Flags().BoolVar(&f.daemonAutoStart, "daemon-auto-start", true, "deprecated; TUI no longer uses a daemon")
	cmd.Flags().BoolVar(&f.noDaemonAutoStart, "no-daemon-auto-start", false, "deprecated; TUI no longer uses a daemon")
	if err := cmd.MarkFlagRequired("target-pane"); err != nil {
		panic(fmt.Sprintf("tui: mark --target-pane required: %v", err))
	}
	cmd.PreRun = func(c *cobra.Command, _ []string) {
		f.daemonAutoStartSet = c.Flags().Changed("daemon-auto-start")
	}
	return cmd
}

func runTUI(deps Deps, f tuiFlags) error {
	// Pre-flight chain: config → store → pane. Each step short-circuits
	// on error so the user sees the most-fundamental broken layer first.
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

	adapter, err := deps.NewTmux()
	if err != nil {
		return err
	}
	target := buildTUITarget(f)
	exists, err := adapter.PaneExists(context.Background(), target.PaneID)
	if err != nil {
		return err
	}
	if !exists {
		return &tmux.PaneMissingError{PaneID: target.PaneID}
	}

	state := buildTUIState(summaries, cfg)
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
