package app

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/tmux"
)

func newListCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available prompts",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
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
			for _, summary := range summaries {
				_, _ = fmt.Fprintln(deps.Stdout, summary.ID)
			}
			return nil
		},
	}
}

func newShowCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print the body of a prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig(*deps.ConfigPath)
			if err != nil {
				return err
			}
			s, err := deps.NewStore(cfg)
			if err != nil {
				return err
			}
			p, err := s.Resolve(args[0])
			if err != nil {
				return err
			}
			w := deps.Stdout
			_, _ = fmt.Fprintf(w, "ID: %s\n", p.ID)
			_, _ = fmt.Fprintf(w, "Source: %s\n", p.Path)
			if p.Title != "" {
				_, _ = fmt.Fprintf(w, "Title: %s\n", p.Title)
			}
			if p.Description != "" {
				_, _ = fmt.Fprintf(w, "Description: %s\n", p.Description)
			}
			if len(p.Tags) > 0 {
				_, _ = fmt.Fprintf(w, "Tags: %s\n", strings.Join(p.Tags, ", "))
			}
			if p.Key != "" {
				_, _ = fmt.Fprintf(w, "Key: %s\n", p.Key)
			}
			_, _ = fmt.Fprintln(w)
			_, _ = fmt.Fprint(w, p.Body)
			return nil
		},
	}
}

func newSendCmd(deps Deps) *cobra.Command {
	var (
		targetPane string
		mode       string
		pressEnter bool
		sanitize   string
	)
	cmd := &cobra.Command{
		Use:   "send <id>",
		Short: "Deliver a prompt into a tmux pane synchronously",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			f := sendFlags{targetPane: targetPane}
			if c.Flags().Changed("mode") {
				f.mode = &mode
			}
			if c.Flags().Changed("enter") {
				f.pressEnter = &pressEnter
			}
			if c.Flags().Changed("sanitize") {
				f.sanitize = &sanitize
			}
			return runSend(deps, args[0], f)
		},
	}
	cmd.Flags().StringVar(&targetPane, "target-pane", "", "tmux pane ID to deliver into")
	cmd.Flags().StringVar(&mode, "mode", "", "delivery mode: paste or type")
	cmd.Flags().BoolVar(&pressEnter, "enter", false, "press Enter after delivery")
	cmd.Flags().StringVar(&sanitize, "sanitize", "", "sanitize mode: off, safe, or strict (currently validated but not applied)")
	return cmd
}

type sendFlags struct {
	targetPane string
	mode       *string
	pressEnter *bool
	sanitize   *string
}

func runSend(deps Deps, id string, f sendFlags) error {
	cfg, err := deps.LoadConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}
	s, err := deps.NewStore(cfg)
	if err != nil {
		return err
	}
	prompt, err := s.Resolve(id)
	if err != nil {
		return err
	}

	fm := config.FrontmatterDefaults{
		Mode:  prompt.Defaults.Mode,
		Enter: prompt.Defaults.Enter,
	}
	delivery, err := config.ResolveDelivery(cfg, fm, config.DeliveryFlags{
		Mode:     f.mode,
		Enter:    f.pressEnter,
		Sanitize: f.sanitize,
	})
	if err != nil {
		return err
	}

	body := prompt.Body
	if cfg.MaxPasteBytes > 0 && int64(len(body)) > cfg.MaxPasteBytes {
		return &tmux.OversizeError{Bytes: len(body), Limit: cfg.MaxPasteBytes}
	}

	adapter, err := deps.NewTmux()
	if err != nil {
		return err
	}

	target, err := resolveSendTarget(f.targetPane, adapter, deps.Env)
	if err != nil {
		return err
	}
	// CurrentContext() returns our own pane, so existence is implicit — only
	// verify a user-supplied --target-pane.
	if f.targetPane != "" {
		exists, err := adapter.PaneExists(target.PaneID)
		if err != nil {
			return err
		}
		if !exists {
			return &tmux.PaneMissingError{PaneID: target.PaneID}
		}
	}

	switch delivery.Mode {
	case "paste":
		return adapter.Paste(target, body, delivery.Enter)
	case "type":
		return adapter.Type(target, body, delivery.Enter)
	default:
		return fmt.Errorf("internal error: unresolved delivery mode %q", delivery.Mode)
	}
}

func resolveSendTarget(flagValue string, adapter tmux.Adapter, env func(string) string) (tmux.TargetContext, error) {
	if flagValue != "" {
		return tmux.TargetContext{PaneID: flagValue}, nil
	}
	if env("TMUX") == "" {
		return tmux.TargetContext{}, &tmux.EnvError{Reason: "not running inside tmux and no --target-pane supplied"}
	}
	return adapter.CurrentContext()
}

func newPasteCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "paste",
		Short: "Deliver the host clipboard into a tmux pane synchronously",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
				return err
			}
			return ErrNotImplemented
		},
	}
}

func newDoctorCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose configuration, prompt store, and environment issues",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runDoctor(deps)
		},
	}
}

func newTUICmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI (typically from a tmux popup)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
				return err
			}
			return ErrNotImplemented
		},
	}
}

func newPickCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "pick",
		Short: "Select a prompt via an external picker (picker_command)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
				return err
			}
			return ErrNotImplemented
		},
	}
}

func newDaemonCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the deferred-delivery daemon",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:     "start",
			Aliases: []string{"run"},
			Short:   "Start the daemon in the foreground",
			Args:    cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
					return err
				}
				return ErrNotImplemented
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Report daemon status",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				if _, err := deps.LoadConfig(*deps.ConfigPath); err != nil {
					return err
				}
				return ErrNotImplemented
			},
		},
	)
	return cmd
}
