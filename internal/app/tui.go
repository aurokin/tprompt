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

	// Build the Submitter up front so it can be injected into the Renderer.
	// The real Model invokes Submit via a tea.Cmd for ActionPrompt; the stub
	// Renderer paths used by TPROMPT_TEST_RENDERER fall back to the direct
	// sub.Submit below.
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
	case tui.ActionPrompt:
		// The real Renderer invokes Submitter inside the Model via tea.Cmd;
		// any error has already surfaced via renderer.Run above. Stub
		// renderers do not emit ActionPrompt today, so no direct submit is
		// required here.
		return nil
	case tui.ActionClipboard:
		// AUR-25 will move this into the Model alongside the P keypress; for
		// now the stub Renderer (staticClipboardRenderer) still returns
		// ActionClipboard directly and we submit on its behalf.
		return sub.Submit(result)
	default:
		return fmt.Errorf("tui: unknown renderer action %q", result.Action)
	}
}

// buildTUIState assembles the State the Renderer sees: pinned clipboard row,
// alphabetically-sorted board rows, overflow rows, and the reserved-key map.
func buildTUIState(summaries []store.Summary, cfg config.Resolved) tui.State {
	reserved := reservedKeys(cfg)
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
	if clipKey, ok := clipboardKey(reserved); ok {
		rows = append(rows, tui.Row{
			Key:         clipKey,
			Description: "(read on select)",
		})
	}
	rows = append(rows, board...)

	return tui.State{
		Rows:     rows,
		Overflow: overflow,
		Reserved: reserved,
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
