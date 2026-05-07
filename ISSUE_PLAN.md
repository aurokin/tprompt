# ISSUE_PLAN — AUR-265 Make `daemon start` backgrounded

## Goal

Convert `tprompt daemon start` into an explicit background launcher.
The launcher:

1. Probes the existing daemon socket via `Status` first. If a
   compatible daemon answers, exit 0 with a clear "already running"
   message. This check happens before opening the daemon log file or
   the tmux adapter.
2. Acquires the lifecycle start lock from AUR-263 to serialize
   concurrent cold starts.
3. Re-probes Status under the lock (the previous start-lock holder
   might have just spawned a daemon).
4. Calls a `StartIntent`-aware trust-gate hook (no-op until AUR-314
   wires the macOS assessor).
5. Appends pre-spawn diagnostics (parent pid, intent, exec, canonical
   exec, socket path, lifecycle paths, log path, env removed/forwarded,
   trust-gate result) to the daemon log on a best-effort basis.
6. Spawns `tprompt daemon run --config <path>` detached (`Setsid`),
   stdout discarded, stderr appended to the daemon log.
7. Polls `Status` until it succeeds, the readiness deadline expires
   (default 5 s), or the child exits early.
8. Returns a `lifecycle.StartResult` distinguishing
   `Started`/`AlreadyRunning`/`Failed`.

The TUI implicit auto-start path (`Deps.StartDaemon`) is replumbed
through the same launcher. The parent's spawn argv now ends in
`daemon run` (not `daemon start`), eliminating the recursion class of
bug.

## Files

New:

- `internal/app/lifecycle/launcher.go` — `Launcher`, `Options`,
  `StartIntent` enum (`IntentExplicitStart | IntentExplicitRun |
  IntentImplicitTUI`), `Start(ctx, intent)` returning
  `lifecycle.StartResult`. Pre-spawn diagnostic builder that emits a
  single logfmt line.
- `internal/app/lifecycle/launcher_test.go` — fakes for
  `StatusProber`, `Spawner`, `TrustAssessor`, and the start-lock
  probe path. Tests cover already-running short-circuit, post-lock
  re-probe, spawn → readiness, readiness timeout, child early exit,
  trust-gate rejection mapping, and concurrent cold starts (two
  goroutines call Start; one observes Started, the other
  AlreadyRunning).
- `internal/app/lifecycle/spawn.go` — production `Spawner` that
  builds the `exec.Cmd` (`Setsid`, stdout discarded, stderr appended
  to daemon log). Lives in its own file because it touches `os/exec`
  and `syscall.SysProcAttr`.

Edit:

- `internal/app/deps.go` — replace `StartDaemon`'s production
  implementation with one that calls `lifecycle.Launcher.Start` with
  `IntentImplicitTUI`. The production Spawner spawns
  `daemon run --config <path>` instead of `daemon start`.
- `internal/app/commands.go` — `daemon start` no longer calls
  `runDaemonForeground`; it builds a `Launcher` with
  `IntentExplicitStart` and prints the result. The dual-purpose
  handler `runDaemonForeground` is renamed back to `runDaemonRun` (or
  inline) since only `daemon run` calls it.
- `internal/app/tui.go` — `autoStartTUIDaemon` now consumes a
  `StartResult` from the launcher. Failure mapping to daemon/IPC error
  preserves today's error class.
- `internal/app/daemon_test.go` — start-flavored tests that injected a
  fake `runDaemon` to exercise the previous foreground-start handler
  drop the foreground assumption and cover the launcher seam through a
  fake Launcher.
- `cmd/tprompt/testdata/script/*.txtar` — testscripts that previously
  did `tprompt daemon start --config ... &` migrate to
  `tprompt daemon run --config ... &` so they keep getting a
  foreground process to manage. Affected scripts (per AUR-269 plan):
  `tui_cancel.txtar`, `tui_clipboard_happy.txtar`,
  `tui_clipboard_oversize.txtar`, `tui_pane_missing.txtar`. These
  migrations land here in AUR-265 because otherwise the existing
  testscripts break the moment `daemon start` becomes background.

## Wire shape

```go
package lifecycle  // internal/app/lifecycle

type StartIntent int
const (
    IntentExplicitStart StartIntent = iota
    IntentExplicitRun
    IntentImplicitTUI
)

type Options struct {
    SocketPath       string
    LogPath          string
    ConfigPath       string  // forwarded as --config when set
    Executable       string  // exec spawned for `daemon run`
    ReadinessTimeout time.Duration
    PollInterval     time.Duration
    Now              func() time.Time
    Status           StatusProber
    Spawner          Spawner
    TrustAssessor    TrustAssessor       // optional; no-op default
    LogPreSpawn      func(parent string) // optional logger sink
}

type StatusProber interface {
    Probe(ctx context.Context) error
}
type Spawner interface {
    Spawn(ctx context.Context, exec string, args []string, logPath string) error
}
type TrustAssessor interface {
    Assess(intent StartIntent, exec string) AssessResult
}
type AssessResult struct {
    Allow  bool
    Reason string
}

type Launcher struct{ /* opts */ }

func New(opts Options) *Launcher
func (l *Launcher) Start(ctx context.Context, intent StartIntent) dlife.StartResult
```

`dlife` is the `internal/daemon/lifecycle` package alias.

## Tests

* `Launcher_AlreadyRunningShortCircuits` — Status returns nil before
  any work; result.Outcome == AlreadyRunning, no spawn.
* `Launcher_SpawnAndPollUntilReady` — Status returns error then nil;
  Spawner invoked with `daemon run` argv.
* `Launcher_ReadinessTimeoutMapsToFailed` — Status always errs;
  Outcome == Failed, Reason == ReasonReadinessTimeout, cooldown
  recorded for ImplicitTUI, NOT recorded for explicit intents.
* `Launcher_ChildExitEarlyMapsToFailed` — Spawner reports child exited
  before the readiness deadline; Reason == ReasonChildExitedEarly.
* `Launcher_ConcurrentCallsExactlyOneStart` — two goroutines call
  Start; the second observes AlreadyRunning thanks to start-lock
  serialization.
* `Launcher_TrustGateRejection` — TrustAssessor returns Allow=false
  for ImplicitTUI; Outcome == Failed, Reason == ReasonTrustGate, no
  spawn occurs. Same TrustAssessor returning Deny for ExplicitStart
  is bypassed (gate only fires for ImplicitTUI).
* `Launcher_PreSpawnDiagnosticsLogged` — LogPreSpawn captured a
  logfmt line that includes parent pid, intent, exec, socket, log,
  trust-gate result.
* `productionStartDaemon` test in `daemon_test.go` is removed; its
  coverage moves to the launcher tests.
* CLI-level: `daemon start` against a missing daemon → spawn observed
  via fake; against an already-running daemon → "already running"
  printed and exit 0; against a non-Status-responding socket → exit
  daemon-error.

## Out of scope

* Default-on TUI auto-start (AUR-266).
* macOS trust gate implementation (AUR-314 wires the assessor).
* Adding new lifecycle testscripts (AUR-269).
* `daemon stop` behavior changes (AUR-268; current Stop RPC already
  works against any daemon owning the socket).
