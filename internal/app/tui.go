package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
)

// tuiFlags captures the --target-pane / --client-tty / --session-id inputs.
type tuiFlags struct {
	targetPane string
	clientTTY  string
	sessionID  string
}

func newTUICmd(deps Deps) *cobra.Command {
	var f tuiFlags
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI (typically from a tmux popup)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runTUI(deps, f)
		},
	}
	cmd.Flags().StringVar(&f.targetPane, "target-pane", "", "tmux pane ID to deliver into (required)")
	cmd.Flags().StringVar(&f.clientTTY, "client-tty", "", "tmux client TTY for failure banners")
	cmd.Flags().StringVar(&f.sessionID, "session-id", "", "tmux session ID for delivery context")
	if err := cmd.MarkFlagRequired("target-pane"); err != nil {
		panic(fmt.Sprintf("tui: mark --target-pane required: %v", err))
	}
	return cmd
}

func runTUI(deps Deps, f tuiFlags) error {
	// Pre-flight chain: config → store → daemon → pane. Each step short-circuits
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

	client, err := deps.NewDaemonClient(cfg)
	if err != nil {
		return err
	}
	if _, err := client.Status(); err != nil {
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

	renderer, err := deps.NewRenderer(cfg)
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
	case tui.ActionPrompt:
		sub := deps.NewSubmitter(cfg, s, client, target)
		return sub.Submit(result)
	case tui.ActionClipboard:
		// AUR-22 wires the clipboard path through Submitter.
		return nil
	default:
		return fmt.Errorf("tui: unknown renderer action %q", result.Action)
	}
}

// buildTUIState assembles the State the Renderer sees: pinned clipboard row,
// alphabetically-sorted board rows, overflow rows, and the reserved-key map.
func buildTUIState(summaries []store.Summary, cfg config.Resolved) tui.State {
	var board, overflow []tui.Row
	for _, sum := range summaries {
		row := tui.Row{
			PromptID:    sum.ID,
			Title:       sum.Title,
			Description: sum.Description,
			Tags:        sum.Tags,
		}
		if sum.Key != "" {
			row.Key = []rune(sum.Key)[0]
			board = append(board, row)
			continue
		}
		overflow = append(overflow, row)
	}
	// store.List() already returns summaries sorted by ID; the split preserves
	// that order for both slices.

	rows := make([]tui.Row, 0, len(board)+1)
	if clipKey, ok := clipboardKey(cfg.ReservedPrintable); ok {
		rows = append(rows, tui.Row{
			Key:         clipKey,
			Description: "(read on select)",
		})
	}
	rows = append(rows, board...)

	return tui.State{
		Rows:     rows,
		Overflow: overflow,
		Reserved: cfg.ReservedPrintable,
	}
}

// clipboardKey finds the reserved printable rune assigned to the clipboard
// action. Returns ok=false if the clipboard key is disabled in config.
func clipboardKey(reserved map[rune]string) (rune, bool) {
	for r, role := range reserved {
		if role == "clipboard" {
			return r, true
		}
	}
	return 0, false
}

func buildTUITarget(f tuiFlags) tmux.TargetContext {
	return tmux.TargetContext{
		PaneID:    f.targetPane,
		ClientTTY: f.clientTTY,
		Session:   f.sessionID,
	}
}
