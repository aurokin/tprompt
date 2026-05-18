// Package lifecycle owns the parent-side daemon launcher used by
// `tprompt daemon start`. It wraps the
// foundational primitives in internal/daemon/lifecycle (run lock, start
// lock, identity sidecar, cooldown marker, structured start result) with
// CLI knowledge: spawn argv, --config propagation, daemon log path,
// pre-spawn diagnostics, and the macOS trust gate hook (AUR-314).
//
// Production callers wire a Launcher in internal/app/deps.go. Tests
// inject fakes through the StatusProber/Spawner/TrustAssessor interfaces.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	dlife "github.com/hsadler/tprompt/internal/daemon/lifecycle"
)

// processAlive returns true when sending signal 0 to pid succeeds. On
// Unix, kill(pid, 0) is the standard liveness probe — it does not
// deliver a signal but returns success/EPERM if the process exists and
// ESRCH if it does not.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// StartIntent classifies why the launcher was invoked.
type StartIntent int

const (
	// IntentExplicitStart is `tprompt daemon start` invoked directly.
	IntentExplicitStart StartIntent = iota
	// IntentExplicitRun is `tprompt daemon run`. The launcher does not
	// drive `daemon run` (it runs in foreground), but the enum value
	// exists so explicit intent can be recorded in pre-spawn diagnostics
	// and the trust gate hook can match on it.
	IntentExplicitRun
	// IntentImplicitTUI is the legacy TUI auto-start intent. The current
	// TUI path uses short-lived handoff workers instead.
	IntentImplicitTUI
)

func (i StartIntent) String() string {
	switch i {
	case IntentExplicitStart:
		return "explicit_start"
	case IntentExplicitRun:
		return "explicit_run"
	case IntentImplicitTUI:
		return "implicit_tui"
	default:
		return "unknown"
	}
}

// DefaultReadinessTimeout is the default wall-clock budget for the launcher
// to observe the daemon's Status RPC succeed after spawning the child.
const DefaultReadinessTimeout = 5 * time.Second

// DefaultPollInterval is how often the launcher re-runs Status while
// waiting for readiness.
const DefaultPollInterval = 50 * time.Millisecond

// AssessResult is the macOS trust gate's verdict on whether the launcher
// is allowed to spawn the current executable for an implicit start.
type AssessResult struct {
	Allow  bool
	Reason string // populated when Allow == false
}

// ProbeResult classifies the outcome of a Status probe. The launcher
// uses it to decide whether to spawn a fresh daemon, refuse, or treat
// the daemon as already running.
type ProbeResult int

const (
	// ProbeOK means the Status RPC succeeded — a compatible daemon owns
	// the socket.
	ProbeOK ProbeResult = iota
	// ProbeUnreachable means the dial step failed (no socket, ENOENT,
	// ECONNREFUSED). Safe to spawn a fresh daemon.
	ProbeUnreachable
	// ProbeReachableBroken means the dial succeeded but Status did not.
	// Some other process is bound to the socket but cannot answer the
	// Status RPC. The launcher refuses to start another daemon over it
	// — recovery is operator-driven (`daemon stop`, kill, etc.).
	ProbeReachableBroken
)

// StatusProber asks the daemon socket if a compatible daemon is alive.
// Implementations classify dial vs RPC failures so the launcher does
// not respawn over a reachable-but-broken daemon process.
type StatusProber interface {
	Probe(ctx context.Context) (ProbeResult, error)
}

// SpawnHandle is what Spawner.Spawn returns so the launcher can detect
// child early-exit while polling Status. PID = 0 means the spawner
// produced no checkable process (e.g., a fake in tests). When PID > 0
// the launcher sends a `kill(pid, 0)` probe each poll to detect child
// death before the readiness deadline.
type SpawnHandle struct {
	PID int
}

// Spawner detaches a child running `args` against `exec`, redirecting
// stderr to logPath. Implementations are expected to fork-exec and
// release the child so the parent can return immediately.
type Spawner interface {
	Spawn(ctx context.Context, exec string, args []string, logPath string) (SpawnHandle, error)
}

// TrustAssessor implements the macOS executable-trust preflight. It is
// invoked on the explicit-start path via runTrustGate (under
// IntentExplicitStart) and on the foreground `daemon run` path via
// commands.preflightDaemonRun (under IntentExplicitRun). The intent is
// threaded into Assess so the assessor's denial reason can be rendered
// with the right callsite-specific recovery wording (AUR-329); the
// implicit-TUI path on darwin never reaches the assessor (AUR-326).
// On non-darwin builds the production assessor is a no-op.
type TrustAssessor interface {
	Assess(exec string, intent StartIntent) AssessResult
}

// noopAssessor allows everything. Used when no TrustAssessor is
// supplied (Linux production, unit tests that don't model trust).
type noopAssessor struct{}

func (noopAssessor) Assess(string, StartIntent) AssessResult {
	return AssessResult{Allow: true}
}

// Options wires a Launcher with everything it needs.
type Options struct {
	SocketPath       string
	LogPath          string
	ConfigPath       string // forwarded as --config when set
	Executable       string
	ReadinessTimeout time.Duration
	PollInterval     time.Duration
	Now              func() time.Time
	Status           StatusProber
	Spawner          Spawner
	Assessor         TrustAssessor
	// LogPreSpawn receives a single-line logfmt diagnostic before the
	// detached child is spawned. Optional; production wires this to the
	// daemon log appender.
	LogPreSpawn func(line string)
}

// Launcher owns the start-lock-protected, cooldown-aware spawn flow.
// Invoke via Start. Reusable across calls.
type Launcher struct {
	opts Options
}

// New constructs a Launcher with sensible defaults filled in.
func New(opts Options) *Launcher {
	if opts.ReadinessTimeout <= 0 {
		opts.ReadinessTimeout = DefaultReadinessTimeout
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = DefaultPollInterval
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Assessor == nil {
		opts.Assessor = noopAssessor{}
	}
	return &Launcher{opts: opts}
}

// Start runs the lifecycle launcher under the start lock. It returns a
// structured StartResult; CLI commands map that to user-visible output and
// exit codes.
func (l *Launcher) Start(ctx context.Context, intent StartIntent) dlife.StartResult {
	paths, err := dlife.PathsFor(l.opts.SocketPath)
	if err != nil {
		return failed(dlife.ReasonConfig, "lifecycle paths: "+err.Error())
	}

	if res, decided := l.classifyProbe(ctx); decided {
		// A successful probe means a compatible daemon owns the
		// socket. Clear any stale cooldown marker so the legacy implicit
		// start path is not gated by an expired-window cooldown left over
		// from before this success.
		// Mirrors the post-lock OutcomeAlreadyRunning branch below.
		if res.Outcome == dlife.OutcomeAlreadyRunning {
			_ = dlife.ClearCooldown(paths)
		}
		return res
	}

	// Platform policy: macOS hardcodes implicit auto-start off (AUR-326).
	// The check sits AFTER the probe so a daemon that another process
	// already started is reported as OutcomeAlreadyRunning rather than
	// being refused. It sits BEFORE the cooldown / start lock / spawn
	// path so disabled intents never touch the cooldown marker.
	if res, gated := l.applyImplicitPolicy(intent, paths); gated {
		return res
	}

	if res, gated := l.checkCooldown(paths, intent); gated {
		return res
	}

	startLock, err := dlife.AcquireStartLock(paths)
	if err != nil {
		return failed(dlife.ReasonSpawnFailed, "acquire start lock: "+err.Error())
	}
	defer func() { _ = startLock.Release() }()

	if res, decided := l.classifyProbe(ctx); decided {
		if res.Outcome == dlife.OutcomeAlreadyRunning {
			_ = dlife.ClearCooldown(paths)
		}
		return res
	}

	// Re-check the cooldown marker AFTER acquiring the start lock.
	// Without this, a concurrent implicit caller that was waiting on
	// the start lock would bypass a cooldown that the previous holder
	// just recorded (after its own trust-gate or spawn failure). The
	// post-lock probe above handles the "already running" race; this
	// handles the "just failed and recorded a cooldown" race.
	if res, gated := l.checkCooldown(paths, intent); gated {
		return res
	}

	assessment, gateRes, gated := l.runTrustGate(intent)
	if gated {
		l.recordImplicitFailure(paths, intent, gateRes)
		return gateRes
	}

	l.logPreSpawn(intent, paths, assessment)

	handle, err := l.opts.Spawner.Spawn(ctx, l.opts.Executable, buildSpawnArgs(l.opts.ConfigPath), l.opts.LogPath)
	if err != nil {
		res := failed(dlife.ReasonSpawnFailed, err.Error())
		l.recordImplicitFailure(paths, intent, res)
		return res
	}

	res := l.waitForReadiness(ctx, handle)
	if res.Outcome == dlife.OutcomeStarted {
		_ = dlife.ClearCooldown(paths)
		return res
	}
	l.recordImplicitFailure(paths, intent, res)
	return res
}

// classifyProbe runs the StatusProber and returns a final StartResult
// (and decided=true) when the result is conclusive: either the daemon is
// already running, or the socket is reachable but the Status RPC failed.
// ProbeUnreachable is the only "keep going" classification.
func (l *Launcher) classifyProbe(ctx context.Context) (dlife.StartResult, bool) {
	res, _ := l.probe(ctx)
	switch res {
	case ProbeOK:
		return dlife.StartResult{Outcome: dlife.OutcomeAlreadyRunning}, true
	case ProbeReachableBroken:
		return failed(dlife.ReasonOther,
			"socket bound by an unresponsive process; manual recovery required (try `tprompt daemon stop` or kill the existing process)"), true
	default:
		return dlife.StartResult{}, false
	}
}

// checkCooldown returns a Failed result when an implicit caller is
// gated by an active cooldown. Explicit intents always bypass.
func (l *Launcher) checkCooldown(paths dlife.Paths, intent StartIntent) (dlife.StartResult, bool) {
	if intent != IntentImplicitTUI {
		return dlife.StartResult{}, false
	}
	cd, active, err := dlife.ReadCooldown(paths, l.opts.Now)
	if err != nil || !active {
		return dlife.StartResult{}, false
	}
	return failed(dlife.ReasonCooldown,
		fmt.Sprintf("implicit auto-start in cooldown until %s (reason=%s, log=%s)",
			cd.Until.Format(time.RFC3339), cd.Reason, cd.LogPath)), true
}

// applyImplicitPolicy short-circuits the launcher when the platform
// policy disables this StartIntent. On darwin, IntentImplicitTUI is
// always disabled (AUR-326). The policy fires before the cooldown
// check and the start lock so a disabled intent never records a
// cooldown, never runs the trust assessor, and never spawns. A
// dedicated diagnostic line is still emitted so operators can see
// the refusal in the daemon log.
func (l *Launcher) applyImplicitPolicy(intent StartIntent, paths dlife.Paths) (dlife.StartResult, bool) {
	disabled, reason := MacOSImplicitAutoStartDisabled(intent)
	if !disabled {
		return dlife.StartResult{}, false
	}
	l.logImplicitDisabled(intent, paths, reason)
	return failed(dlife.ReasonPolicyDisabled, reason), true
}

// runTrustGate applies the macOS executable trust assessment on the
// explicit-start path (AUR-327). The implicit-TUI path on darwin is
// already short-circuited by applyImplicitPolicy before this runs;
// `daemon run` foreground does its own preflight in the command
// handler since the launcher does not drive `daemon run`. Other
// intents bypass.
func (l *Launcher) runTrustGate(intent StartIntent) (AssessResult, dlife.StartResult, bool) {
	if intent != IntentExplicitStart {
		return AssessResult{Allow: true}, dlife.StartResult{}, false
	}
	res := l.opts.Assessor.Assess(l.opts.Executable, intent)
	if res.Allow {
		return res, dlife.StartResult{}, false
	}
	return res, failed(dlife.ReasonTrustGate, res.Reason), true
}

// waitForReadiness polls Status until it succeeds, the deadline elapses,
// or ctx is canceled. When the SpawnHandle carries a checkable PID, the
// loop also probes the child via kill(pid, 0) so a crash before bind is
// reported as ReasonChildExitedEarly instead of burning the full
// readiness budget on a dead pid. ProbeReachableBroken at any iteration
// short-circuits with the same incompatible-daemon message Start uses.
func (l *Launcher) waitForReadiness(ctx context.Context, handle SpawnHandle) dlife.StartResult {
	deadline := l.opts.Now().Add(l.opts.ReadinessTimeout)
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return failed(dlife.ReasonOther, "context canceled: "+err.Error())
		}
		switch res, err := l.probe(ctx); res {
		case ProbeOK:
			return dlife.StartResult{Outcome: dlife.OutcomeStarted}
		case ProbeReachableBroken:
			return failed(dlife.ReasonOther,
				"socket bound by an unresponsive process; manual recovery required")
		default:
			lastErr = err
		}
		if handle.PID > 0 && !processAlive(handle.PID) {
			detail := fmt.Sprintf("daemon child (pid=%d) exited before bind", handle.PID)
			if l.opts.LogPath != "" {
				detail += fmt.Sprintf(" (see %s)", l.opts.LogPath)
			}
			return failed(dlife.ReasonChildExitedEarly, detail)
		}
		if !l.opts.Now().Before(deadline) {
			break
		}
		time.Sleep(l.opts.PollInterval)
	}
	detail := fmt.Sprintf("readiness wait %s exceeded", l.opts.ReadinessTimeout)
	if lastErr != nil {
		detail = fmt.Sprintf("%s; last status err: %v", detail, lastErr)
	}
	if l.opts.LogPath != "" {
		detail += fmt.Sprintf(" (see %s)", l.opts.LogPath)
	}
	return failed(dlife.ReasonReadinessTimeout, detail)
}

func (l *Launcher) probe(ctx context.Context) (ProbeResult, error) {
	if l.opts.Status == nil {
		return ProbeUnreachable, errors.New("lifecycle launcher: nil StatusProber")
	}
	return l.opts.Status.Probe(ctx)
}

func (l *Launcher) recordImplicitFailure(paths dlife.Paths, intent StartIntent, res dlife.StartResult) {
	if intent != IntentImplicitTUI {
		return
	}
	cd := dlife.Cooldown{
		Until:   l.opts.Now().Add(dlife.DefaultCooldownTTL),
		Reason:  string(res.Reason),
		LogPath: l.opts.LogPath,
	}
	_ = dlife.RecordCooldown(paths, cd)
}

// writeLifecyclePrelude emits the shared `outcome=... parent_pid=...
// intent=... exec=... socket=... runlock=... startlock=... identity=...
// cooldown=... log=... [config=...]` head used by every lifecycle
// diagnostic. Per-outcome suffixes are appended by the caller.
func (l *Launcher) writeLifecyclePrelude(b *strings.Builder, outcome string, intent StartIntent, paths dlife.Paths) {
	fmt.Fprintf(b, "outcome=%s parent_pid=%d intent=%s exec=%q socket=%q runlock=%q startlock=%q identity=%q cooldown=%q log=%q",
		outcome, os.Getpid(), intent, l.opts.Executable, l.opts.SocketPath,
		paths.RunLock, paths.StartLock, paths.Identity, paths.CooldownMark, l.opts.LogPath)
	if l.opts.ConfigPath != "" {
		fmt.Fprintf(b, " config=%q", l.opts.ConfigPath)
	}
}

// logImplicitDisabled emits the diagnostic line for a policy refusal.
// Shares the prelude with logPreSpawn; the outcome key is distinct so
// log readers can filter by event type.
func (l *Launcher) logImplicitDisabled(intent StartIntent, paths dlife.Paths, reason string) {
	if l.opts.LogPreSpawn == nil {
		return
	}
	var b strings.Builder
	l.writeLifecyclePrelude(&b, "lifecycle_implicit_disabled", intent, paths)
	fmt.Fprintf(&b, " reason=%q", reason)
	l.opts.LogPreSpawn(b.String())
}

func (l *Launcher) logPreSpawn(intent StartIntent, paths dlife.Paths, assess AssessResult) {
	if l.opts.LogPreSpawn == nil {
		return
	}
	var b strings.Builder
	l.writeLifecyclePrelude(&b, "lifecycle_pre_spawn", intent, paths)
	switch {
	case !assess.Allow:
		fmt.Fprintf(&b, " trust=denied reason=%q", assess.Reason)
	case assess.Reason != "":
		// Allow path with a non-empty reason means the assessor
		// short-circuited (e.g., debug override). Keep that visible
		// so operators can see when the gate was bypassed.
		fmt.Fprintf(&b, " trust=allow_override reason=%q", assess.Reason)
	default:
		b.WriteString(" trust=allow")
	}
	l.opts.LogPreSpawn(b.String())
}

func failed(reason dlife.StartFailureReason, detail string) dlife.StartResult {
	return dlife.StartResult{Outcome: dlife.OutcomeFailed, Reason: reason, Detail: detail}
}

// buildSpawnArgs returns the argv passed to the Spawner: the daemon
// child runs `daemon run`, optionally prefixed by `--config <path>`.
func buildSpawnArgs(configPath string) []string {
	args := make([]string, 0, 4)
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	args = append(args, "daemon", "run")
	return args
}
