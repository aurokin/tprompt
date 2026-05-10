package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	applife "github.com/hsadler/tprompt/internal/app/lifecycle"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	dlife "github.com/hsadler/tprompt/internal/daemon/lifecycle"
	"github.com/hsadler/tprompt/internal/envutil"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
)

// tuiFlags captures the --target-pane / --client-tty / --session-id /
// --daemon-auto-start / --no-daemon-auto-start inputs.
//
// daemonAutoStartSet records whether --daemon-auto-start was named on
// the command line (cobra's Flag.Changed semantics). noDaemonAutoStart
// is the readable opt-out alias; setting it forces the auto-start
// branch off regardless of config.
type tuiFlags struct {
	targetPane         string
	clientTTY          string
	sessionID          string
	daemonAutoStart    bool
	daemonAutoStartSet bool
	noDaemonAutoStart  bool
}

// daemonAutoStartMu serializes the in-process auto-start window so two
// concurrent runTUI calls in the same process do not both invoke the
// launcher. Cross-process serialization is handled by the lifecycle
// start lock inside the launcher itself.
var daemonAutoStartMu sync.Mutex

// noAutoStartEnv is the "hard opt-out" environment variable users can
// set to disable TUI implicit auto-start across every entry point
// (tmux popups, scripts, mise hooks) without retrofitting flags. Only
// the known-positive values accepted by envutil.Truthy disable auto-
// start; unrecognized values leave it active so a typo cannot silently
// disable it (AUR-328).
const noAutoStartEnv = "TPROMPT_NO_AUTO_START"

func newTUICmd(deps Deps) *cobra.Command {
	var f tuiFlags
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI (typically from a tmux popup)",
		Long: `Launch the interactive TUI for prompt selection and clipboard delivery.
Selections are submitted to the local daemon, which injects into the
target pane after the TUI process exits (daemon-backed deferred
delivery). This is distinct from 'send' and 'paste', which deliver
synchronously without the daemon.

--target-pane is required so the daemon knows where to deliver. The
target pane must still exist when the daemon injects. Typically invoked
from a tmux popup binding that passes the originating context, e.g.:

  tprompt tui --target-pane '#{pane_id}' --client-tty '#{client_tty}' \
    --session-id '#{session_id}'

On Linux and other non-macOS platforms, daemon auto-start is on by
default: when the TUI runs and the daemon is unreachable, tprompt
spawns a background daemon and waits for readiness. To turn it off
for one invocation, pass --no-daemon-auto-start (or
--daemon-auto-start=false). To turn it off permanently, set
'daemon_auto_start = false' in config, or set the
TPROMPT_NO_AUTO_START environment variable to a truthy value
(1, true, yes, on; case-insensitive).

On macOS, implicit TUI auto-start is hardcoded off (AUR-326). When
the daemon is unreachable, the TUI refuses with a recovery hint
pointing at 'tprompt daemon start' (background) or 'tprompt daemon
run' (foreground). Neither config nor flag re-enables the implicit
path.`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runTUI(deps, f)
		},
	}
	cmd.Flags().StringVar(&f.targetPane, "target-pane", "", "tmux pane ID to deliver into (required)")
	cmd.Flags().StringVar(&f.clientTTY, "client-tty", "", "originating tmux client TTY for failure banners")
	cmd.Flags().StringVar(&f.sessionID, "session-id", "", "originating tmux session ID for delivery context")
	cmd.Flags().BoolVar(&f.daemonAutoStart, "daemon-auto-start", true, "auto-start the daemon for this TUI run if unreachable (default true)")
	cmd.Flags().BoolVar(&f.noDaemonAutoStart, "no-daemon-auto-start", false, "skip auto-starting the daemon for this TUI run (alias for --daemon-auto-start=false)")
	if err := cmd.MarkFlagRequired("target-pane"); err != nil {
		panic(fmt.Sprintf("tui: mark --target-pane required: %v", err))
	}
	cmd.PreRun = func(c *cobra.Command, _ []string) {
		f.daemonAutoStartSet = c.Flags().Changed("daemon-auto-start")
	}
	return cmd
}

func runTUI(deps Deps, f tuiFlags) error {
	envOptOut, err := resolveTUIAutoStartIntent(deps, f)
	if err != nil {
		return err
	}
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

	if err := ensureTUIDaemonReady(deps, cfg, client, daemonAutoStartEnabled(f, cfg, envOptOut)); err != nil {
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
func renderAndDispatchTUI(deps Deps, cfg config.Resolved, s store.Store, client daemon.Client, target tmux.TargetContext, state tui.State) error {
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

// resolveTUIAutoStartIntent validates the auto-start opt-out signals
// before any I/O so a usage error surfaces before LoadConfig runs. It
// returns the env-opt-out flag for the resolver to consume.
//
// Two checks run here:
//  1. The flag-vs-flag mutual exclusion (`--daemon-auto-start` and
//     `--no-daemon-auto-start` cannot coexist).
//  2. AUR-328: `TPROMPT_NO_AUTO_START` (truthy) conflicts with an
//     explicit `--daemon-auto-start=true`. "Explicit on" here means
//     cobra parsed a --daemon-auto-start flag (Flag.Changed) AND its
//     resolved boolean value is true; --daemon-auto-start=false is
//     consistent with the env opt-out and not flagged. The aim is to
//     never silently override the operator: a flag and an env var
//     pointing in opposite directions is always a usage error.
//
// Running both checks before LoadConfig keeps the env var a hard
// opt-out: a malformed config file cannot mask the user's intent.
func resolveTUIAutoStartIntent(deps Deps, f tuiFlags) (bool, error) {
	if f.daemonAutoStartSet && f.noDaemonAutoStart {
		return false, errors.New("tui: --daemon-auto-start and --no-daemon-auto-start are mutually exclusive")
	}
	rawEnv := readEnv(deps.Env, noAutoStartEnv)
	envOptOut := envutil.TruthyOf(rawEnv)
	if envOptOut && f.daemonAutoStartSet && f.daemonAutoStart {
		return false, fmt.Errorf("tui: --daemon-auto-start (explicit on) conflicts with %s=%s (set in environment); remove one", noAutoStartEnv, strings.TrimSpace(rawEnv))
	}
	return envOptOut, nil
}

// readEnv invokes the deps.Env seam once with a nil-safe default.
// Callers that need both the raw value (for echoing in errors) and a
// truthy decision should use this with envutil.TruthyOf so getenv is
// not invoked twice for the same key.
//
// The nil guard exists because resolveTUIAutoStartIntent calls this
// with the raw deps.Env, which production wires but tests sometimes
// leave nil. Callsites that go through doctor's envOrEmpty wrapper
// already see a non-nil function — for them the nil check is
// redundant but harmless, and keeping it here lets readEnv stay the
// single canonical seam regardless of caller.
func readEnv(getenv func(string) string, key string) string {
	if getenv == nil {
		return ""
	}
	return getenv(key)
}

// daemonAutoStartEnabled resolves the auto-start decision from the
// flag bag, the resolved config, and the TPROMPT_NO_AUTO_START env
// opt-out. Callers must have first run resolveTUIAutoStartIntent so
// the env+explicit-on conflict has already been rejected as a usage
// error; this function therefore never has to disambiguate that case.
//
// Precedence (highest to lowest):
//  1. --no-daemon-auto-start flag → off
//  2. TPROMPT_NO_AUTO_START truthy → off
//  3. --daemon-auto-start explicit (=true or =false) → flag value
//  4. config.daemon_auto_start → config default
//
// Reachable combinations after the upstream conflict gate:
//   - env=truthy + --daemon-auto-start=true   → rejected upstream; never reaches here.
//   - env=truthy + --daemon-auto-start=false  → off (rule 1 if --no- alias was used, otherwise rule 2).
//   - env=truthy alone                        → off (rule 2).
//   - env=falsy/unset + explicit flag         → flag value (rule 3).
//   - env=falsy/unset + no flag               → config default (rule 4).
func daemonAutoStartEnabled(f tuiFlags, cfg config.Resolved, envOptOut bool) bool {
	if f.noDaemonAutoStart {
		return false
	}
	if envOptOut {
		return false
	}
	if f.daemonAutoStartSet {
		return f.daemonAutoStart
	}
	return cfg.DaemonAutoStart
}

func ensureTUIDaemonReady(deps Deps, cfg config.Resolved, client daemon.Client, autoStart bool) error {
	if _, err := client.Status(); err != nil {
		var socketErr *daemon.SocketUnavailableError
		if !autoStart || !errors.As(err, &socketErr) {
			return err
		}
		return autoStartTUIDaemon(deps, cfg, client)
	}
	return nil
}

func autoStartTUIDaemon(deps Deps, cfg config.Resolved, client daemon.Client) error {
	daemonAutoStartMu.Lock()
	defer daemonAutoStartMu.Unlock()

	if _, err := client.Status(); err == nil {
		return nil
	} else {
		var socketErr *daemon.SocketUnavailableError
		if !errors.As(err, &socketErr) {
			return err
		}
	}

	// Platform policy: macOS hardcodes TUI implicit auto-start off
	// (AUR-326). Refuse here, before constructing a Launcher we know
	// would just refuse with the same reason. Keeps the cold-start
	// failure surface localized to the TUI flow and lets test seams
	// assert NewLauncher was never invoked on darwin.
	if disabled, reason := applife.MacOSImplicitAutoStartDisabled(applife.IntentImplicitTUI); disabled {
		return &daemon.IPCError{
			Path: cfg.SocketPath,
			Op:   "auto-start daemon",
			Reason: launcherFailureMessage(dlife.StartResult{
				Outcome: dlife.OutcomeFailed,
				Reason:  dlife.ReasonPolicyDisabled,
				Detail:  reason,
			}, cfg.LogPath),
		}
	}

	if err := validateDaemonStartConfig(cfg); err != nil {
		return err
	}

	launcher := deps.NewLauncher(cfg, explicitConfigPath(deps))
	res := launcher.Start(context.Background(), applife.IntentImplicitTUI)
	switch res.Outcome {
	case dlife.OutcomeStarted, dlife.OutcomeAlreadyRunning:
		return nil
	default:
		return &daemon.IPCError{
			Path:   cfg.SocketPath,
			Op:     "auto-start daemon",
			Reason: launcherFailureMessage(res, cfg.LogPath),
		}
	}
}

// launcherFailureMessage formats a StartResult for surfacing as a daemon
// IPC error. The TUI's recovery hint must only suggest options the TUI
// command path accepts, so the message embeds the daemon log path (where
// real diagnostics land) but does not invite the user to retry with a
// different flag. Explicit recovery via `tprompt daemon start` /
// `tprompt daemon run` is documented elsewhere.
func launcherFailureMessage(res dlife.StartResult, logPath string) string {
	detail := res.Detail
	if detail == "" {
		detail = string(res.Reason)
	}
	if logPath == "" {
		return detail
	}
	return fmt.Sprintf("%s (see %s)", detail, logPath)
}

func explicitConfigPath(deps Deps) string {
	if deps.ConfigPath == nil {
		return ""
	}
	return *deps.ConfigPath
}

// buildTUIState assembles the State the Renderer sees: pinned clipboard row,
// alphabetically-sorted board rows, overflow rows, and the reserved-key map.
func buildTUIState(summaries []store.Summary, cfg config.Resolved) tui.State {
	reserved := reservedKeys(cfg)
	var board, overflow []tui.Row
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
