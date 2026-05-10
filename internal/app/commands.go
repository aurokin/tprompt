package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	applife "github.com/hsadler/tprompt/internal/app/lifecycle"
	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	dlife "github.com/hsadler/tprompt/internal/daemon/lifecycle"
	"github.com/hsadler/tprompt/internal/sanitize"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
)

// appVersion is reported by `tprompt daemon status`. The release workflow
// overrides this via `-ldflags -X github.com/hsadler/tprompt/internal/app.appVersion=$(cat VERSION)`.
// Keep the default in sync with VERSION so dev builds and tests assert the
// same string the next tagged release will report.
var appVersion = "0.1.0"

var runDaemon = daemon.Run

const daemonStopTimeout = 2 * time.Second

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
		Use:   "send <id>",
		Short: "Deliver a prompt into a tmux pane synchronously",
		Long: `Send delivers a prompt body synchronously to a tmux pane. Delivery is
direct via the tmux adapter in this process — it does not use the daemon
and is not affected by pending TUI jobs.

If --target-pane is omitted, the current tmux pane is used; outside tmux
this fails with a clear error. Delivery settings resolve in this order:
CLI flags, prompt frontmatter, config file, built-in defaults.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			f := sendFlags{targetPane: targetPane}
			if c.Flags().Changed("mode") {
				f.mode = &mode
			}
			if c.Flags().Changed("enter") {
				f.pressEnter = &pressEnter
			}
			if c.Flags().Changed("sanitize") {
				f.sanitize = &sanitizeFlag
			}
			return runSend(deps, args[0], f)
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
tmux pane. Same-host only: the clipboard reader, daemon, and tmux pane
all run on the same machine.

If --target-pane is omitted, the current tmux pane is used. Like 'send',
this command does not use the daemon and delivers directly. Flag set
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

func newDaemonCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the deferred-delivery daemon",
		Long: `Manage the local tprompt daemon, which performs deferred delivery of
TUI-selected prompts after the TUI process exits. Lifecycle subcommands:

  start    Start the daemon in the background (idempotent if already
           running).
  run      Run the daemon in the foreground listening on the socket.
  status   Read-only status check; does not start the daemon implicitly.
  stop     Request graceful shutdown over the local IPC socket.

'tprompt send' and 'tprompt paste' do not use the daemon.`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start the daemon in the background (idempotent if already running)",
			Long: `Start the daemon in the background. Spawns a detached 'daemon run'
process if no compatible daemon is already running, redirects child
stderr to the configured daemon log, polls the daemon Status RPC until
readiness, then returns. If a compatible daemon is already running,
prints "already running" and exits successfully — making this command
idempotent for shell scripts and tmux popup bindings. Failures map to
daemon/IPC errors and mention the daemon log path where relevant.`,
			Args: cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				return runDaemonStartBackground(c.Context(), deps)
			},
		},
		&cobra.Command{
			Use:   "run",
			Short: "Run the daemon in the foreground",
			Long: `Run the daemon in the foreground. The daemon listens on the configured
socket and processes deferred delivery jobs submitted by 'tprompt tui'.
'daemon run' acquires the lifecycle run lock before binding the socket
and refuses to start when a compatible daemon already owns it. Send
SIGINT or SIGTERM (or run 'tprompt daemon stop' from another shell) to
request graceful shutdown.`,
			Args: cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				return runDaemonForeground(c.Context(), deps)
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Report daemon status",
			Long: `Print the running daemon's pid, socket, log path, uptime, version, and
pending job count. This is a read-only check — it does not start the
daemon if it is not running.`,
			Args: cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				return runDaemonStatus(deps)
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Request graceful daemon shutdown",
			Long: `Request graceful shutdown of the running daemon over the local IPC
socket. If no daemon is reachable on the configured socket, prints
'daemon not running' and exits 0. If the daemon does not finish shutting
down within a short bounded wait, exits with a daemon/IPC error.`,
			Args: cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				return runDaemonStop(deps, daemonStopTimeout)
			},
		},
	)
	return cmd
}

// runDaemonStartBackground is the explicit `daemon start` handler. It
// dispatches to the lifecycle launcher with IntentExplicitStart so the
// trust gate is bypassed (explicit recovery commands always proceed) and
// any active cooldown is cleared on success. The launcher spawns
// `daemon run` detached, polls Status until ready, then returns. If a
// compatible daemon is already running this is an idempotent success.
func runDaemonStartBackground(parent context.Context, deps Deps) error {
	cfg, err := deps.LoadDaemonConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}
	if err := validateDaemonStartConfig(cfg); err != nil {
		return err
	}
	launcher := deps.NewLauncher(cfg, explicitConfigPath(deps))
	res := launcher.Start(parent, applife.IntentExplicitStart)
	switch res.Outcome {
	case dlife.OutcomeAlreadyRunning:
		_, _ = fmt.Fprintf(deps.Stdout, "tprompt daemon already running on %s\n", cfg.SocketPath)
		return nil
	case dlife.OutcomeStarted:
		_, _ = fmt.Fprintf(deps.Stdout, "tprompt daemon started on %s\n", cfg.SocketPath)
		return nil
	default:
		detail := res.Detail
		if detail == "" {
			detail = string(res.Reason)
		}
		return &daemon.IPCError{
			Path:   cfg.SocketPath,
			Op:     "daemon start",
			Reason: detail,
		}
	}
}

// runDaemonForeground is the shared handler for `daemon run`. It runs the
// daemon server loop in the calling process. The run-lock + identity
// sidecar from AUR-263 enforce one daemon per socket; collisions surface
// as daemon/IPC errors with the same exit-code mapping the launcher
// uses.
func runDaemonForeground(parent context.Context, deps Deps) error {
	cfg, err := deps.LoadDaemonConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}
	if err := validateDaemonStartConfig(cfg); err != nil {
		return err
	}
	if err := preflightDaemonRun(deps, cfg); err != nil {
		return err
	}
	adapter, err := deps.NewTmux()
	if err != nil {
		return err
	}
	logger, err := daemon.NewLogger(cfg.LogPath)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Close() }()

	executor := daemon.NewExecutor(adapter, logger, cfg.MaxPasteBytes)
	executor.EnablePostInjectionVerification(cfg.PostInjectionVerification)
	queue := daemon.NewQueue(adapter, logger, executor.Run)
	started := time.Now()

	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	exec, _ := os.Executable()

	server := daemon.NewServer(daemon.ServerConfig{
		SocketPath: cfg.SocketPath,
		Queue:      queue,
		Logger:     logger,
		ShutdownFn: stop,
		StatusFn: func() daemon.StatusResponse {
			return daemon.StatusResponse{
				PID:         os.Getpid(),
				Socket:      cfg.SocketPath,
				LogPath:     cfg.LogPath,
				UptimeSec:   int64(time.Since(started).Seconds()),
				PendingJobs: queue.Pending(),
				Version:     appVersion,
			}
		},
		IdentityFn: func() dlife.Identity {
			return dlife.Identity{
				PID:       os.Getpid(),
				StartTime: started,
				Exec:      exec,
				Socket:    cfg.SocketPath,
				Log:       cfg.LogPath,
				Version:   appVersion,
			}
		},
	})

	runResult, runErr := runDaemon(ctx, server, func() {
		_, _ = fmt.Fprintf(deps.Stdout, "tprompt daemon listening on %s\n", cfg.SocketPath)
		_ = logger.Log(daemon.Entry{
			Outcome: daemon.OutcomeStarted,
			Msg:     fmt.Sprintf("pid=%d socket=%s", os.Getpid(), cfg.SocketPath),
		})
		// Explicit `daemon run` is a recovery path: a healthy bind
		// here means any cooldown left over from a prior implicit
		// auto-start failure is no longer relevant. Clear it so a
		// later TUI auto-start (after this daemon is stopped) is not
		// gated by a stale window. Mirrors the launcher's
		// OutcomeStarted / OutcomeAlreadyRunning cooldown clear.
		if paths, err := dlife.PathsFor(cfg.SocketPath); err == nil {
			_ = dlife.ClearCooldown(paths)
		}
	})

	if runResult.Started && runErr == nil && runResult.ExitReason == daemon.RunExitContextCanceled {
		_ = logger.Log(daemon.Entry{Outcome: daemon.OutcomeStopped})
		_, _ = fmt.Fprintln(deps.Stdout, "tprompt daemon stopped")
	}
	return runErr
}

func runDaemonStatus(deps Deps) error {
	cfg, err := deps.LoadDaemonConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}
	if err := validateDaemonStatusConfig(cfg); err != nil {
		return err
	}
	client, err := deps.NewDaemonClient(cfg)
	if err != nil {
		return err
	}
	status, err := client.Status()
	if err != nil {
		return err
	}

	writeDaemonStatus(deps.Stdout, status)
	return nil
}

// runDaemonStop sends the Stop RPC over the daemon socket and waits
// (bounded) for the socket to disappear. The handler is mode-agnostic
// by construction: after AUR-264/265 the only daemon binary is
// `daemon run`, and both implicit (TUI auto-start) and explicit
// (`daemon start`) paths spawn `daemon run` detached. All three modes
// converge on the same Server lifecycle once the daemon is bound,
// so the same Stop RPC tears down whichever daemon owns the socket.
func runDaemonStop(deps Deps, timeout time.Duration) error {
	cfg, err := deps.LoadDaemonConfig(*deps.ConfigPath)
	if err != nil {
		return err
	}
	if err := validateDaemonStatusConfig(cfg); err != nil {
		return err
	}
	client, err := deps.NewDaemonClient(cfg)
	if err != nil {
		return err
	}
	if _, err := client.Stop(); err != nil {
		if daemon.IsDaemonGone(err) {
			_, _ = fmt.Fprintln(deps.Stdout, "daemon not running")
			return nil
		}
		return err
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := client.Status(); err != nil {
			if daemon.IsDaemonGone(err) {
				_, _ = fmt.Fprintln(deps.Stdout, "tprompt daemon stopped")
				return nil
			}
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return &daemon.ShutdownTimeoutError{Path: cfg.SocketPath, TimeoutMS: int(timeout / time.Millisecond)}
}

func writeDaemonStatus(w io.Writer, status daemon.StatusResponse) {
	_, _ = fmt.Fprintln(w, "tprompt daemon")
	_, _ = fmt.Fprintf(w, "  pid:          %d\n", status.PID)
	_, _ = fmt.Fprintf(w, "  socket:       %s\n", status.Socket)
	_, _ = fmt.Fprintf(w, "  log:          %s\n", status.LogPath)
	_, _ = fmt.Fprintf(w, "  uptime:       %s\n", formatUptime(status.UptimeSec))
	_, _ = fmt.Fprintf(w, "  version:      %s\n", status.Version)
	_, _ = fmt.Fprintf(w, "  pending jobs: %d\n", status.PendingJobs)
}

func formatUptime(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	return (time.Duration(seconds) * time.Second).String()
}

func validateDaemonStatusConfig(cfg config.Resolved) error {
	if cfg.SocketPath == "" {
		return &config.ValidationError{Field: "socket_path", Message: "must be set"}
	}
	return nil
}

func validateDaemonStartConfig(cfg config.Resolved) error {
	if err := validateDaemonStatusConfig(cfg); err != nil {
		return err
	}
	if cfg.LogPath == "" {
		return &config.ValidationError{Field: "log_path", Message: "must be set"}
	}
	if cfg.MaxPasteBytes <= 0 {
		return &config.ValidationError{
			Field:   "max_paste_bytes",
			Message: fmt.Sprintf("must be positive, got %d", cfg.MaxPasteBytes),
		}
	}
	return nil
}

// preflightDaemonRun runs the macOS executable-trust preflight before
// the foreground `daemon run` binds the socket (AUR-327). On non-darwin
// the assessor is a no-op so the call is effectively free. The daemon
// log is not yet open at this point; failures surface through stderr
// via the returned error.
//
// We fail safe in the production direction: a nil constructor falls
// back to applife.ProductionAssessor so a caller that forgets to wire
// NewTrustAssessor cannot accidentally bypass the gate on darwin. On
// non-darwin ProductionAssessor is a no-op via build-tagged
// trust_other.go.
//
// We use os.Executable() rather than the launcher's resolved path
// because `daemon run` is the entrypoint for two paths: invoked
// directly by the user (no parent launcher), and spawned as a child by
// `daemon start` (parent already preflighted, but the cost of
// re-checking is small and the alternative — a cross-process trust
// hand-off — is harder to reason about).
//
// On darwin, an os.Executable() failure must surface as a preflight
// failure: we cannot inspect what we cannot find, and the gate is
// load-bearing. On non-darwin the assessor is a no-op, so we tolerate
// resolution failure and pass an empty exec — preserving the
// pre-AUR-327 behavior for restricted /proc setups where `daemon run`
// previously worked despite os.Executable() returning an error.
func preflightDaemonRun(deps Deps, cfg config.Resolved) error {
	newAssessor := deps.NewTrustAssessor
	if newAssessor == nil {
		newAssessor = applife.ProductionAssessor
	}
	assessor := newAssessor()
	if assessor == nil {
		assessor = applife.ProductionAssessor()
	}
	exec, err := os.Executable()
	if err != nil {
		if runtime.GOOS == "darwin" {
			return &daemon.IPCError{
				Path:   cfg.SocketPath,
				Op:     "daemon run",
				Reason: "resolve executable path: " + err.Error(),
			}
		}
		exec = ""
	}
	res := assessor.Assess(exec)
	if res.Allow {
		return nil
	}
	return &daemon.IPCError{
		Path:   cfg.SocketPath,
		Op:     "daemon run",
		Reason: res.Reason,
	}
}
