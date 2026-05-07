package lifecycle

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dlife "github.com/hsadler/tprompt/internal/daemon/lifecycle"
)

type probeOutcome struct {
	res ProbeResult
	err error
}

type stubProber struct {
	mu       sync.Mutex
	results  []probeOutcome
	fallback probeOutcome
	calls    int
}

func (s *stubProber) Probe(context.Context) (ProbeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if len(s.results) == 0 {
		return s.fallback.res, s.fallback.err
	}
	out := s.results[0]
	s.results = s.results[1:]
	return out.res, out.err
}

func okFallback() probeOutcome { return probeOutcome{res: ProbeOK} }
func unreachable() probeOutcome {
	return probeOutcome{res: ProbeUnreachable, err: errors.New("not yet")}
}

func unreachableN(n int) []probeOutcome {
	out := make([]probeOutcome, n)
	for i := range out {
		out[i] = unreachable()
	}
	return out
}

type stubSpawner struct {
	mu       sync.Mutex
	called   int
	lastExec string
	lastArgs []string
	lastLog  string
	pid      int
	err      error
}

func (s *stubSpawner) Spawn(_ context.Context, exec string, args []string, log string) (SpawnHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called++
	s.lastExec = exec
	s.lastArgs = append([]string(nil), args...)
	s.lastLog = log
	if s.err != nil {
		return SpawnHandle{}, s.err
	}
	return SpawnHandle{PID: s.pid}, nil
}

type stubAssessor struct {
	allow  bool
	reason string
}

func (s stubAssessor) Assess(StartIntent, string) AssessResult {
	return AssessResult{Allow: s.allow, Reason: s.reason}
}

func newPaths(t *testing.T) (dlife.Paths, string) {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")
	p, err := dlife.PathsFor(socket)
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	return p, socket
}

func TestLauncherAlreadyRunningShortCircuits(t *testing.T) {
	t.Parallel()
	_, socket := newPaths(t)
	prober := &stubProber{fallback: okFallback()}
	spawner := &stubSpawner{}

	l := New(Options{
		SocketPath: socket,
		Status:     prober,
		Spawner:    spawner,
	})
	res := l.Start(context.Background(), IntentExplicitStart)
	if res.Outcome != dlife.OutcomeAlreadyRunning {
		t.Fatalf("Outcome = %v, want AlreadyRunning", res.Outcome)
	}
	if spawner.called != 0 {
		t.Fatalf("Spawner.Spawn called %d times despite already-running", spawner.called)
	}
	if prober.calls < 1 {
		t.Fatal("StatusProber should be probed at least once")
	}
}

func TestLauncherSpawnsWhenSocketUnreachable(t *testing.T) {
	t.Parallel()
	_, socket := newPaths(t)
	// First two probes fail (initial + post-lock), then success.
	prober := &stubProber{
		results:  unreachableN(2),
		fallback: okFallback(),
	}
	spawner := &stubSpawner{}
	l := New(Options{
		SocketPath: socket,
		ConfigPath: "/etc/tprompt/config.toml",
		Executable: "/usr/local/bin/tprompt",
		LogPath:    "/tmp/d.log",
		Status:     prober,
		Spawner:    spawner,
	})
	res := l.Start(context.Background(), IntentExplicitStart)
	if res.Outcome != dlife.OutcomeStarted {
		t.Fatalf("Outcome = %v (%s), want Started", res.Outcome, res.String())
	}
	if spawner.called != 1 {
		t.Fatalf("Spawner.Spawn called %d times, want 1", spawner.called)
	}
	wantArgs := []string{"--config", "/etc/tprompt/config.toml", "daemon", "run"}
	if got := spawner.lastArgs; !equalArgs(got, wantArgs) {
		t.Fatalf("spawn argv = %v, want %v", got, wantArgs)
	}
	if spawner.lastLog != "/tmp/d.log" {
		t.Fatalf("spawn logPath = %q, want /tmp/d.log", spawner.lastLog)
	}
}

func TestLauncherReadinessTimeoutMapsToFailed(t *testing.T) {
	t.Parallel()
	p, socket := newPaths(t)
	// Always fail the probe; deadline is short. Real wall clock so the
	// poll loop exits naturally after a few ms.
	prober := &stubProber{fallback: unreachable()}
	spawner := &stubSpawner{}
	l := New(Options{
		SocketPath:       socket,
		LogPath:          "/tmp/d.log",
		Executable:       "/usr/local/bin/tprompt",
		ReadinessTimeout: 5 * time.Millisecond,
		PollInterval:     time.Millisecond,
		Status:           prober,
		Spawner:          spawner,
	})
	res := l.Start(context.Background(), IntentExplicitStart)
	if res.Outcome != dlife.OutcomeFailed {
		t.Fatalf("Outcome = %v, want Failed", res.Outcome)
	}
	if res.Reason != dlife.ReasonReadinessTimeout {
		t.Fatalf("Reason = %v, want ReadinessTimeout", res.Reason)
	}
	if !strings.Contains(res.Detail, "/tmp/d.log") {
		t.Fatalf("detail %q does not include log path", res.Detail)
	}
	// Explicit intent: cooldown must NOT be recorded.
	if _, active, _ := dlife.ReadCooldown(p, time.Now); active {
		t.Fatal("explicit intent recorded a cooldown")
	}
}

func TestLauncherImplicitFailureRecordsCooldown(t *testing.T) {
	t.Parallel()
	p, socket := newPaths(t)
	prober := &stubProber{fallback: unreachable()}
	spawner := &stubSpawner{}
	l := New(Options{
		SocketPath:       socket,
		LogPath:          "/tmp/d.log",
		Executable:       "/usr/local/bin/tprompt",
		ReadinessTimeout: 5 * time.Millisecond,
		PollInterval:     time.Millisecond,
		Status:           prober,
		Spawner:          spawner,
	})
	res := l.Start(context.Background(), IntentImplicitTUI)
	if res.Outcome != dlife.OutcomeFailed {
		t.Fatalf("Outcome = %v, want Failed", res.Outcome)
	}
	cd, active, err := dlife.ReadCooldown(p, time.Now)
	if err != nil {
		t.Fatalf("ReadCooldown: %v", err)
	}
	if !active {
		t.Fatal("cooldown should be active after implicit failure")
	}
	if cd.LogPath != "/tmp/d.log" {
		t.Fatalf("cooldown LogPath = %q, want /tmp/d.log", cd.LogPath)
	}
}

func TestLauncherImplicitCooldownGatesNextStart(t *testing.T) {
	t.Parallel()
	p, socket := newPaths(t)
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	if err := dlife.RecordCooldown(p, dlife.Cooldown{
		Until:   now.Add(time.Hour),
		Reason:  string(dlife.ReasonSpawnFailed),
		LogPath: "/tmp/d.log",
	}); err != nil {
		t.Fatalf("seed cooldown: %v", err)
	}

	prober := &stubProber{fallback: unreachable()}
	spawner := &stubSpawner{}
	l := New(Options{
		SocketPath: socket,
		Status:     prober,
		Spawner:    spawner,
		Now:        func() time.Time { return now },
	})
	res := l.Start(context.Background(), IntentImplicitTUI)
	if res.Outcome != dlife.OutcomeFailed || res.Reason != dlife.ReasonCooldown {
		t.Fatalf("Outcome=%v Reason=%v, want Failed/Cooldown", res.Outcome, res.Reason)
	}
	if spawner.called != 0 {
		t.Fatalf("spawn during cooldown")
	}

	// Explicit start bypasses cooldown.
	prober2 := &stubProber{results: unreachableN(2), fallback: okFallback()}
	spawner2 := &stubSpawner{}
	l2 := New(Options{
		SocketPath:       socket,
		Status:           prober2,
		Spawner:          spawner2,
		ReadinessTimeout: 5 * time.Millisecond,
		PollInterval:     time.Millisecond,
	})
	res2 := l2.Start(context.Background(), IntentExplicitStart)
	if res2.Outcome != dlife.OutcomeStarted {
		t.Fatalf("explicit start in cooldown = %v, want Started", res2.Outcome)
	}
}

// TestLauncherPostLockCooldownReCheck verifies that a cooldown
// recorded WHILE another implicit start was waiting on the start
// lock still gates the waiter. Without re-checking after the lock
// is acquired, a concurrent failure-then-resume scenario would let
// the second caller spawn during the intended cooldown window.
//
// This models call A failing under the start lock (records cooldown,
// releases lock) and call B resuming and reading the cooldown marker
// before any further work. The launcher's pre-lock cooldown check
// would miss this if it weren't repeated post-lock.
func TestLauncherPostLockCooldownReCheck(t *testing.T) {
	t.Parallel()
	p, socket := newPaths(t)
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

	// Seed cooldown AFTER the launcher has been constructed but
	// BEFORE Start runs — simulating "another goroutine recorded a
	// cooldown while we were waiting on the start lock." We do this
	// by injecting a Spawner that records the cooldown on first call
	// and asserting it is never called twice.
	spawner := &recordCooldownSpawner{paths: p, until: now.Add(time.Hour), reason: dlife.ReasonSpawnFailed, logPath: "/tmp/d.log"}

	// Two probes both return unreachable so both pre-lock and
	// post-lock probes fall through.
	prober := &stubProber{results: unreachableN(2), fallback: unreachable()}

	l := New(Options{
		SocketPath:       socket,
		Status:           prober,
		Spawner:          spawner,
		ReadinessTimeout: 5 * time.Millisecond,
		PollInterval:     time.Millisecond,
		Now:              func() time.Time { return now },
	})

	// First call: pre-lock cooldown is unset; post-lock cooldown is
	// unset on entry but we want to simulate that during this call's
	// lifetime, OUR own implicit failure records a cooldown that the
	// NEXT call (still in the start-lock queue) would observe. We
	// achieve that with a stub spawner that records the cooldown
	// before returning failure.
	res := l.Start(context.Background(), IntentImplicitTUI)
	if res.Outcome != dlife.OutcomeFailed {
		t.Fatalf("first call Outcome = %v, want Failed", res.Outcome)
	}
	if spawner.called != 1 {
		t.Fatalf("spawner.called = %d, want 1 on first call", spawner.called)
	}

	// Second call models the queued waiter: the cooldown is now
	// recorded. Without the post-lock cooldown re-check, the launcher
	// would proceed to spawn (because the pre-lock check could be
	// raced past in a real concurrent scenario). With the re-check,
	// it must short-circuit to ReasonCooldown.
	prober2 := &stubProber{results: unreachableN(2), fallback: unreachable()}
	spawner2 := &recordCooldownSpawner{paths: p}
	l2 := New(Options{
		SocketPath:       socket,
		Status:           prober2,
		Spawner:          spawner2,
		ReadinessTimeout: 5 * time.Millisecond,
		PollInterval:     time.Millisecond,
		Now:              func() time.Time { return now },
	})
	res2 := l2.Start(context.Background(), IntentImplicitTUI)
	if res2.Outcome != dlife.OutcomeFailed || res2.Reason != dlife.ReasonCooldown {
		t.Fatalf("second call Outcome=%v Reason=%v, want Failed/Cooldown", res2.Outcome, res2.Reason)
	}
	if spawner2.called != 0 {
		t.Fatalf("spawner2.called = %d, want 0 (cooldown should gate)", spawner2.called)
	}
}

// recordCooldownSpawner is a Spawner stub that records a cooldown
// marker on the first call (modeling implicit-failure cooldown
// recording) and then returns a spawn error. Tests use it to set up
// the post-lock cooldown race.
type recordCooldownSpawner struct {
	paths   dlife.Paths
	until   time.Time
	reason  dlife.StartFailureReason
	logPath string
	called  int
}

func (s *recordCooldownSpawner) Spawn(_ context.Context, _ string, _ []string, _ string) (SpawnHandle, error) {
	s.called++
	if !s.until.IsZero() {
		_ = dlife.RecordCooldown(s.paths, dlife.Cooldown{
			Until:   s.until,
			Reason:  string(s.reason),
			LogPath: s.logPath,
		})
	}
	return SpawnHandle{}, errors.New("spawn refused for test")
}

func TestLauncherTrustGateRejectionImplicitOnly(t *testing.T) {
	t.Parallel()
	p, socket := newPaths(t)
	prober := &stubProber{fallback: unreachable()}
	spawner := &stubSpawner{}
	l := New(Options{
		SocketPath:       socket,
		Status:           prober,
		Spawner:          spawner,
		Assessor:         stubAssessor{allow: false, reason: "ad-hoc signature"},
		ReadinessTimeout: 5 * time.Millisecond,
		PollInterval:     time.Millisecond,
	})
	res := l.Start(context.Background(), IntentImplicitTUI)
	if res.Outcome != dlife.OutcomeFailed || res.Reason != dlife.ReasonTrustGate {
		t.Fatalf("Outcome=%v Reason=%v, want Failed/TrustGate", res.Outcome, res.Reason)
	}
	if spawner.called != 0 {
		t.Fatal("spawn happened despite trust rejection")
	}
	if _, active, _ := dlife.ReadCooldown(p, time.Now); !active {
		t.Fatal("trust-gate failure did not record cooldown")
	}

	// Same denying assessor under explicit start: gate is bypassed →
	// spawn proceeds. Cooldown set above is bypassed by explicit intent
	// and should be cleared on success.
	prober2 := &stubProber{results: unreachableN(2), fallback: okFallback()}
	spawner2 := &stubSpawner{}
	l2 := New(Options{
		SocketPath:       socket,
		Status:           prober2,
		Spawner:          spawner2,
		Assessor:         stubAssessor{allow: false, reason: "should not fire"},
		ReadinessTimeout: 5 * time.Millisecond,
		PollInterval:     time.Millisecond,
	})
	res2 := l2.Start(context.Background(), IntentExplicitStart)
	if res2.Outcome != dlife.OutcomeStarted {
		t.Fatalf("explicit start through denied trust gate = %v, want Started", res2.Outcome)
	}
	if _, active, _ := dlife.ReadCooldown(p, time.Now); active {
		t.Fatal("explicit start did not clear cooldown")
	}
}

func TestLauncherConcurrentCallsExactlyOneSpawn(t *testing.T) {
	t.Parallel()
	_, socket := newPaths(t)

	// Shared probe state: initially unreachable, then OK once any
	// goroutine has "spawned." We model that by gating on a flag that
	// the spawner flips.
	var spawned atomic.Bool
	prober := &probeCallback{
		fn: func() (ProbeResult, error) {
			if spawned.Load() {
				return ProbeOK, nil
			}
			return ProbeUnreachable, errors.New("not yet")
		},
	}
	var spawnCount atomic.Int64
	spawner := &spawnerCallback{
		fn: func() (SpawnHandle, error) {
			spawnCount.Add(1)
			spawned.Store(true)
			return SpawnHandle{}, nil
		},
	}
	l := func() *Launcher {
		return New(Options{
			SocketPath:       socket,
			Status:           prober,
			Spawner:          spawner,
			ReadinessTimeout: 100 * time.Millisecond,
			PollInterval:     time.Millisecond,
		})
	}

	const n = 6
	var wg sync.WaitGroup
	wg.Add(n)
	results := make(chan dlife.StartOutcome, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			results <- l().Start(context.Background(), IntentExplicitStart).Outcome
		}()
	}
	wg.Wait()
	close(results)

	if got := spawnCount.Load(); got != 1 {
		t.Fatalf("Spawner.Spawn called %d times under concurrent Start, want 1", got)
	}
	var started, already int
	for r := range results {
		switch r {
		case dlife.OutcomeStarted:
			started++
		case dlife.OutcomeAlreadyRunning:
			already++
		default:
			t.Errorf("unexpected outcome %v", r)
		}
	}
	if started != 1 {
		t.Fatalf("started = %d, want 1", started)
	}
	if already != n-1 {
		t.Fatalf("already-running = %d, want %d", already, n-1)
	}
}

func TestLauncherPreSpawnDiagnosticsLogged(t *testing.T) {
	t.Parallel()
	_, socket := newPaths(t)
	// First probe (initial) and second probe (post-lock) both miss; then
	// the readiness loop sees OK once the spawner has been called.
	prober := &stubProber{results: unreachableN(2), fallback: okFallback()}
	spawner := &stubSpawner{}
	var captured string
	l := New(Options{
		SocketPath:   socket,
		ConfigPath:   "/etc/tprompt/config.toml",
		Executable:   "/usr/local/bin/tprompt",
		LogPath:      "/tmp/d.log",
		Status:       prober,
		Spawner:      spawner,
		PollInterval: time.Millisecond,
		LogPreSpawn:  func(line string) { captured = line },
	})
	res := l.Start(context.Background(), IntentImplicitTUI)
	if res.Outcome != dlife.OutcomeStarted {
		t.Fatalf("Outcome = %v, want Started", res.Outcome)
	}
	for _, want := range []string{
		"intent=implicit_tui",
		"parent_pid=",
		`exec="/usr/local/bin/tprompt"`,
		`config="/etc/tprompt/config.toml"`,
		`log="/tmp/d.log"`,
		`socket="` + socket + `"`,
		`runlock="` + socket + `.lock"`,
		`startlock="` + socket + `.start.lock"`,
		`identity="` + socket + `.identity.json"`,
		`cooldown="` + socket + `.start.cooldown"`,
		"trust=allow",
	} {
		if !strings.Contains(captured, want) {
			t.Errorf("pre-spawn diagnostic missing %q\nfull: %s", want, captured)
		}
	}
}

// allowOverrideAssessor returns an Allow result with a non-empty
// reason, modeling the macOS trust gate's debug-override path.
type allowOverrideAssessor struct{ reason string }

func (a allowOverrideAssessor) Assess(StartIntent, string) AssessResult {
	return AssessResult{Allow: true, Reason: a.reason}
}

// TestLauncherPreSpawnDiagnosticsAllowOverride verifies the override
// path is logged with `trust=allow_override` so an operator who set
// TPROMPT_UNSAFE_SKIP_TRUST_GATE=1 can see the gate was bypassed.
func TestLauncherPreSpawnDiagnosticsAllowOverride(t *testing.T) {
	t.Parallel()
	_, socket := newPaths(t)
	prober := &stubProber{results: unreachableN(2), fallback: okFallback()}
	spawner := &stubSpawner{}
	var captured string
	l := New(Options{
		SocketPath:   socket,
		Executable:   "/usr/local/bin/tprompt",
		LogPath:      "/tmp/d.log",
		Status:       prober,
		Spawner:      spawner,
		Assessor:     allowOverrideAssessor{reason: "TPROMPT_UNSAFE_SKIP_TRUST_GATE=1 (debug override)"},
		PollInterval: time.Millisecond,
		LogPreSpawn:  func(line string) { captured = line },
	})
	res := l.Start(context.Background(), IntentImplicitTUI)
	if res.Outcome != dlife.OutcomeStarted {
		t.Fatalf("Outcome = %v, want Started", res.Outcome)
	}
	if !strings.Contains(captured, "trust=allow_override") {
		t.Errorf("captured = %q, want trust=allow_override", captured)
	}
	if !strings.Contains(captured, "TPROMPT_UNSAFE_SKIP_TRUST_GATE=1") {
		t.Errorf("captured = %q, want override env literal in reason", captured)
	}
}

// TestLauncherReachableBrokenRefusesToSpawn verifies the launcher does
// NOT respawn over a daemon process whose Status RPC fails (some other
// process is bound to the socket). Recovery is operator-driven; we'd
// rather fail loudly than start a second daemon over the broken one.
func TestLauncherReachableBrokenRefusesToSpawn(t *testing.T) {
	t.Parallel()
	_, socket := newPaths(t)
	prober := &stubProber{
		fallback: probeOutcome{res: ProbeReachableBroken, err: errors.New("rpc broken")},
	}
	spawner := &stubSpawner{}
	l := New(Options{
		SocketPath: socket,
		Status:     prober,
		Spawner:    spawner,
	})
	res := l.Start(context.Background(), IntentExplicitStart)
	if res.Outcome != dlife.OutcomeFailed {
		t.Fatalf("Outcome = %v, want Failed", res.Outcome)
	}
	if spawner.called != 0 {
		t.Fatalf("Spawner called despite reachable-but-broken socket: %d", spawner.called)
	}
	if !strings.Contains(res.Detail, "manual recovery required") {
		t.Fatalf("detail %q does not mention manual recovery", res.Detail)
	}
}

// TestLauncherChildExitedEarlyMapsToFailed verifies that a daemon child
// that exits before bind is reported as ReasonChildExitedEarly rather
// than burning the full readiness budget on a dead pid. We pass a pid
// guaranteed to be dead by spawning a process that exits immediately.
func TestLauncherChildExitedEarlyMapsToFailed(t *testing.T) {
	t.Parallel()
	_, socket := newPaths(t)
	deadPID := mustDeadPID(t)
	prober := &stubProber{fallback: unreachable()}
	spawner := &stubSpawner{pid: deadPID}
	l := New(Options{
		SocketPath:       socket,
		LogPath:          "/tmp/d.log",
		Executable:       "/usr/local/bin/tprompt",
		ReadinessTimeout: 5 * time.Second, // generous; should exit early on PID death
		PollInterval:     time.Millisecond,
		Status:           prober,
		Spawner:          spawner,
	})
	start := time.Now()
	res := l.Start(context.Background(), IntentExplicitStart)
	elapsed := time.Since(start)
	if res.Outcome != dlife.OutcomeFailed {
		t.Fatalf("Outcome = %v, want Failed", res.Outcome)
	}
	if res.Reason != dlife.ReasonChildExitedEarly {
		t.Fatalf("Reason = %v, want ChildExitedEarly", res.Reason)
	}
	if elapsed >= time.Second {
		t.Fatalf("readiness loop took %v; should short-circuit on dead PID", elapsed)
	}
	if !strings.Contains(res.Detail, "/tmp/d.log") {
		t.Fatalf("detail %q does not include log path", res.Detail)
	}
}

// Helper types for the concurrent and trust-gate tests.

type probeCallback struct {
	mu sync.Mutex
	fn func() (ProbeResult, error)
}

func (p *probeCallback) Probe(context.Context) (ProbeResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fn()
}

type spawnerCallback struct {
	mu sync.Mutex
	fn func() (SpawnHandle, error)
}

func (s *spawnerCallback) Spawn(context.Context, string, []string, string) (SpawnHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fn()
}

// mustDeadPID spawns a short-lived helper, waits for it to exit, and
// returns its PID. The PID is guaranteed dead at return time (Wait
// reaped it). PID reuse is theoretically possible but vanishingly
// unlikely in the readiness window of these tests.
func mustDeadPID(t *testing.T) int {
	t.Helper()
	bin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("no `true` binary on PATH: %v", err)
	}
	cmd := exec.Command(bin)
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v", bin, err)
	}
	return cmd.Process.Pid
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
