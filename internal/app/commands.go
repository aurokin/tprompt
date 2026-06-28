package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aurokin/tprompt/internal/clipboard"
	"github.com/aurokin/tprompt/internal/config"
	"github.com/aurokin/tprompt/internal/prompttmpl"
	"github.com/aurokin/tprompt/internal/sanitize"
	"github.com/aurokin/tprompt/internal/store"
	"github.com/aurokin/tprompt/internal/tmux"
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
				_, _ = fmt.Fprintf(deps.Stdout, "%s  %s  %s\n", summary.ID, promptScope(summary), keybindSummary(summary))
			}
			// First-run nudge for an empty store. tty-gated to stderr so stdout
			// stays the machine-readable list (empty here) and piped/non-tty runs —
			// including the golden testscripts asserting empty stderr — emit nothing.
			if len(summaries) == 0 && streamIsTTY(deps.Stderr) {
				_, _ = fmt.Fprintln(deps.Stderr, "No prompts yet — create one with: tprompt new <id>")
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
			_, _ = fmt.Fprintf(w, "Scope: %s\n", promptScope(p.Summary))
			if p.Title != "" {
				_, _ = fmt.Fprintf(w, "Title: %s\n", p.Title)
			}
			if p.Description != "" {
				_, _ = fmt.Fprintf(w, "Description: %s\n", p.Description)
			}
			if len(p.Tags) > 0 {
				_, _ = fmt.Fprintf(w, "Tags: %s\n", strings.Join(p.Tags, ", "))
			}
			_, _ = fmt.Fprintf(w, "Key: %s\n", keybindValue(p.Summary))
			if p.ShadowPath != "" {
				_, _ = fmt.Fprintf(w, "Shadowed counterpart: %s\n", p.ShadowPath)
			}
			_, _ = fmt.Fprintln(w)
			_, _ = fmt.Fprintln(w, p.Body)
			return nil
		},
	}
}

func promptScope(summary store.Summary) string {
	if summary.Scope != "" {
		return summary.Scope
	}
	return "global"
}

func activePromptPriority(cfg config.Resolved) string {
	if cfg.PromptPriority != "" {
		return cfg.PromptPriority
	}
	return "global"
}

func keybindSummary(summary store.Summary) string {
	if summary.Shadowed {
		if summary.ShadowedBy != "" {
			return "shadowed by " + summary.ShadowedBy
		}
		return "shadowed"
	}
	return "key " + keybindValue(summary)
}

func keybindValue(summary store.Summary) string {
	switch summary.KeySource {
	case store.KeySourceExplicit:
		return fmt.Sprintf("%s (explicit)", summary.Key)
	case store.KeySourceAuto:
		return fmt.Sprintf("%s (auto)", summary.Key)
	case store.KeySourceOverflow:
		return "none (overflow, not on board)"
	case store.KeySourceShadowed:
		return "none (shadowed, search only)"
	default:
		if summary.Key != "" {
			return summary.Key
		}
		return "none (not assigned to board)"
	}
}

func newSendCmd(deps Deps) *cobra.Command {
	var (
		targetPane   string
		mode         string
		pressEnter   bool
		sanitizeFlag string
	)
	cmd := &cobra.Command{
		Use:   "send <id> [template flags]",
		Short: "Deliver a prompt into a tmux pane synchronously",
		Long: `Send delivers a prompt body synchronously to a tmux pane. Delivery is
direct via the tmux adapter in this process — it does not use handoff
and is not affected by pending TUI jobs.

If --target-pane is omitted, the current tmux pane is used; outside tmux
this fails with a clear error. Delivery settings resolve in this order:
CLI flags, prompt frontmatter, config file, built-in defaults.

Prompts that declare frontmatter variables accept matching template flags after
the prompt id, for example: tprompt send review --issue AUR-123.`,
		DisableFlagParsing: true,
		RunE: func(c *cobra.Command, args []string) error {
			parsed, err := parseSendKnownArgs(args, deps.ConfigPath)
			if err != nil {
				return err
			}
			if parsed.help {
				return c.Help()
			}
			return runSend(deps, parsed.id, parsed.flags, parsed.templateArgs)
		},
	}
	cmd.Flags().StringVar(&targetPane, "target-pane", "", "tmux pane ID to deliver into")
	cmd.Flags().StringVar(&mode, "mode", "", "delivery mode: paste or type")
	cmd.Flags().BoolVar(&pressEnter, "enter", false, "press Enter after delivery")
	cmd.Flags().StringVar(&sanitizeFlag, "sanitize", "", "sanitize mode: off, safe, or strict")
	return cmd
}

type sendFlags struct {
	targetPane string
	mode       *string
	pressEnter *bool
	sanitize   *string
}

type parsedSendArgs struct {
	id           string
	flags        sendFlags
	templateArgs []string
	help         bool
}

func parseSendKnownArgs(args []string, configPath *string) (parsedSendArgs, error) {
	var parsed parsedSendArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if parsed.id == "" {
				i++
				if i >= len(args) {
					return parsedSendArgs{}, fmt.Errorf("accepts 1 arg(s), received 0")
				}
				parsed.id = args[i]
				continue
			}
			continue
		}
		if arg == "-h" {
			if parsed.id == "" {
				parsed.help = true
				return parsed, nil
			}
			parsed.templateArgs = append(parsed.templateArgs, arg)
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name, value, hasValue := splitLongFlag(arg)
			switch name {
			case "help":
				if parsed.id == "" {
					parsed.help = true
					return parsed, nil
				}
				parsed.templateArgs = append(parsed.templateArgs, arg)
			case "config":
				got, next, err := flagValue(args, i, name, value, hasValue)
				if err != nil {
					return parsedSendArgs{}, err
				}
				*configPath = got
				i = next
			case "target-pane":
				got, next, err := flagValue(args, i, name, value, hasValue)
				if err != nil {
					return parsedSendArgs{}, err
				}
				parsed.flags.targetPane = got
				i = next
			case "mode":
				got, next, err := flagValue(args, i, name, value, hasValue)
				if err != nil {
					return parsedSendArgs{}, err
				}
				parsed.flags.mode = &got
				i = next
			case "sanitize":
				got, next, err := flagValue(args, i, name, value, hasValue)
				if err != nil {
					return parsedSendArgs{}, err
				}
				parsed.flags.sanitize = &got
				i = next
			case "enter":
				got, err := boolFlagValue(name, value, hasValue)
				if err != nil {
					return parsedSendArgs{}, err
				}
				parsed.flags.pressEnter = &got
			default:
				if parsed.id == "" {
					return parsedSendArgs{}, fmt.Errorf("unknown flag: --%s", name)
				}
				parsed.templateArgs = append(parsed.templateArgs, arg)
			}
			continue
		}
		if parsed.id == "" {
			parsed.id = arg
			continue
		}
		parsed.templateArgs = append(parsed.templateArgs, arg)
	}
	if parsed.help {
		return parsed, nil
	}
	if parsed.id == "" {
		return parsedSendArgs{}, fmt.Errorf("accepts 1 arg(s), received 0")
	}
	return parsed, nil
}

func splitLongFlag(arg string) (name, value string, hasValue bool) {
	trimmed := strings.TrimPrefix(arg, "--")
	if before, after, ok := strings.Cut(trimmed, "="); ok {
		return before, after, true
	}
	return trimmed, "", false
}

func flagValue(args []string, idx int, name, value string, hasValue bool) (string, int, error) {
	if hasValue {
		return value, idx, nil
	}
	if idx+1 >= len(args) {
		return "", idx, fmt.Errorf("flag needs an argument: --%s", name)
	}
	return args[idx+1], idx + 1, nil
}

func boolFlagValue(name, value string, hasValue bool) (bool, error) {
	if !hasValue {
		return true, nil
	}
	switch value {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid argument %q for \"--%s\" flag: must be true or false", value, name)
	}
}

func runSend(deps Deps, id string, f sendFlags, templateArgs []string) error {
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
	templateValues, err := parseTemplateFlags(prompt.Variables, templateArgs)
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

	body, err := prompttmpl.Render(prompt.Body, prompt.Variables, templateValues)
	if err != nil {
		return err
	}
	if cfg.MaxPasteBytes > 0 && int64(len(body)) > cfg.MaxPasteBytes {
		return &tmux.OversizeError{Bytes: len(body), Limit: cfg.MaxPasteBytes}
	}

	cleaned, err := sanitize.New(sanitize.Mode(delivery.Sanitize)).Process([]byte(body))
	if err != nil {
		return err
	}
	body = string(cleaned)

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
		exists, err := adapter.PaneExists(context.Background(), target.PaneID)
		if err != nil {
			return err
		}
		if !exists {
			return &tmux.PaneMissingError{PaneID: target.PaneID}
		}
	}

	switch delivery.Mode {
	case "paste":
		return adapter.Paste(context.Background(), target, body, delivery.Enter)
	case "type":
		return adapter.Type(context.Background(), target, body, delivery.Enter)
	default:
		return fmt.Errorf("internal error: unresolved delivery mode %q", delivery.Mode)
	}
}

func parseTemplateFlags(vars []prompttmpl.Variable, args []string) (map[string]string, error) {
	values := prompttmpl.Defaults(vars)
	allowed := make(map[string]struct{}, len(vars))
	for _, v := range vars {
		allowed[v.Name] = struct{}{}
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			return nil, fmt.Errorf("unexpected template argument %q", arg)
		}
		name, value, hasValue := splitLongFlag(arg)
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("unknown flag: --%s", name)
		}
		if !hasValue {
			var err error
			value, i, err = flagValue(args, i, name, value, hasValue)
			if err != nil {
				return nil, err
			}
		}
		values[name] = value
	}
	if err := prompttmpl.ValidateValues(vars, values); err != nil {
		return nil, err
	}
	return values, nil
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
	var (
		targetPane   string
		mode         string
		pressEnter   bool
		sanitizeFlag string
	)
	cmd := &cobra.Command{
		Use:   "paste",
		Short: "Deliver the host clipboard into a tmux pane synchronously",
		Long: `Paste reads the host clipboard once and delivers it synchronously to a
tmux pane. Same-host only: the clipboard reader and tmux pane run on the
same machine.

If --target-pane is omitted, the current tmux pane is used. Like 'send',
this command does not use handoff and delivers directly. Flag set
mirrors 'send' for consistency.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			f := pasteFlags{targetPane: targetPane}
			if c.Flags().Changed("mode") {
				f.mode = &mode
			}
			if c.Flags().Changed("enter") {
				f.pressEnter = &pressEnter
			}
			if c.Flags().Changed("sanitize") {
				f.sanitize = &sanitizeFlag
			}
			return runPaste(deps, f)
		},
	}
	cmd.Flags().StringVar(&targetPane, "target-pane", "", "tmux pane ID to deliver into")
	cmd.Flags().StringVar(&mode, "mode", "", "delivery mode: paste or type")
	cmd.Flags().BoolVar(&pressEnter, "enter", false, "press Enter after delivery")
	cmd.Flags().StringVar(&sanitizeFlag, "sanitize", "", "sanitize mode: off, safe, or strict")
	return cmd
}

type pasteFlags struct {
	targetPane string
	mode       *string
	pressEnter *bool
	sanitize   *string
}

func runPaste(deps Deps, f pasteFlags) error {
	cfg, err := deps.LoadPasteConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}

	delivery, err := config.ResolveDelivery(cfg, config.FrontmatterDefaults{}, config.DeliveryFlags{
		Mode:     f.mode,
		Enter:    f.pressEnter,
		Sanitize: f.sanitize,
	})
	if err != nil {
		return err
	}

	adapter, target, err := resolvePasteTarget(deps, f.targetPane)
	if err != nil {
		return err
	}

	reader, err := deps.NewClip(cfg)
	if err != nil {
		return err
	}
	body, err := reader.Read()
	if err != nil {
		return err
	}
	if err := clipboard.Validate(body, cfg.MaxPasteBytes); err != nil {
		return err
	}
	cleaned, err := sanitize.New(sanitize.Mode(delivery.Sanitize)).Process(body)
	if err != nil {
		return err
	}

	adapter, err = ensurePasteAdapterAndTarget(deps, adapter, f.targetPane, target)
	if err != nil {
		return err
	}

	switch delivery.Mode {
	case "paste":
		return adapter.Paste(context.Background(), target, string(cleaned), delivery.Enter)
	case "type":
		return adapter.Type(context.Background(), target, string(cleaned), delivery.Enter)
	default:
		return fmt.Errorf("internal error: unresolved delivery mode %q", delivery.Mode)
	}
}

func resolvePasteTarget(deps Deps, targetPane string) (tmux.Adapter, tmux.TargetContext, error) {
	if targetPane != "" {
		return nil, tmux.TargetContext{PaneID: targetPane}, nil
	}
	adapter, err := deps.NewTmux()
	if err != nil {
		return nil, tmux.TargetContext{}, err
	}
	target, err := resolveSendTarget(targetPane, adapter, deps.Env)
	if err != nil {
		return nil, tmux.TargetContext{}, err
	}
	return adapter, target, nil
}

func ensurePasteAdapterAndTarget(
	deps Deps,
	adapter tmux.Adapter,
	targetPane string,
	target tmux.TargetContext,
) (tmux.Adapter, error) {
	if adapter == nil {
		var err error
		adapter, err = deps.NewTmux()
		if err != nil {
			return nil, err
		}
	}
	if targetPane == "" {
		return adapter, nil
	}
	exists, err := adapter.PaneExists(context.Background(), target.PaneID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, &tmux.PaneMissingError{PaneID: target.PaneID}
	}
	return adapter, nil
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

func newPickCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "pick",
		Short: "Select a prompt via an external picker (picker_command)",
		Long: `Pick runs the configured external picker (picker_command, default 'fzf')
over the available prompt IDs and prints the selected ID to stdout. It
does not deliver the prompt — pipe the ID into 'tprompt send' or use it
in shell composition. Cancellation exits 0 with no output.

For interactive selection that delivers, use 'tprompt tui' instead.`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runPick(deps)
		},
	}
}

func runPick(deps Deps) error {
	cfg, err := deps.LoadConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}
	if len(cfg.PickerArgv) == 0 {
		return &config.ValidationError{Field: "picker_command", Message: "must be set for pick"}
	}

	s, err := deps.NewStore(cfg)
	if err != nil {
		return err
	}
	summaries, err := s.List()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		ids = append(ids, summary.ID)
	}

	p, err := deps.NewPicker(cfg)
	if err != nil {
		return err
	}
	selected, cancelled, err := p.Select(ids)
	if err != nil {
		return err
	}
	if cancelled {
		return nil
	}
	_, _ = fmt.Fprintln(deps.Stdout, selected)
	return nil
}
