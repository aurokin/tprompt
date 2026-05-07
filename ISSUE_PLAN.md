# ISSUE_PLAN — AUR-263 Lock daemon ownership

## Goal

Land the lifecycle ownership primitives that later issues compose:

* deterministic sidecar paths next to the configured socket,
* an exclusive run lock the daemon process holds for life,
* a start lock that serializes parent-side cold starts,
* a JSON identity sidecar removable only by the matching daemon,
* a typed start-result that downstream callers map to exit codes,
* a short-lived implicit-start failure cooldown,
* a conservative stale-socket cleanup contract that knows about the
  run lock and identity sidecar.

User-facing CLI behavior does NOT change in this commit. Hooks are wired
inside `internal/daemon/server.go` so the run lock is acquired on
`Listen` and the identity sidecar is written/removed across daemon
lifetime, but the CLI surface is byte-for-byte identical.

## Files

New:

- `internal/daemon/lifecycle/paths.go`
- `internal/daemon/lifecycle/paths_test.go`
- `internal/daemon/lifecycle/runlock.go`
- `internal/daemon/lifecycle/runlock_test.go`
- `internal/daemon/lifecycle/startlock.go`
- `internal/daemon/lifecycle/startlock_test.go`
- `internal/daemon/lifecycle/identity.go`
- `internal/daemon/lifecycle/identity_test.go`
- `internal/daemon/lifecycle/cooldown.go`
- `internal/daemon/lifecycle/cooldown_test.go`
- `internal/daemon/lifecycle/result.go`
- `internal/daemon/lifecycle/result_test.go`
- `internal/daemon/lifecycle/cloexec_test.go` — proves locks have
  `O_CLOEXEC` set (helper exec's `/bin/sh -c true` with the lock fd
  duplicated to the child, asserts the child cannot open the same lock
  if `O_CLOEXEC` is honored). Skipped on non-unix.

Edit:

- `internal/daemon/server.go` — `Listen` acquires the run lock FIRST,
  then runs (now-widened) stale-socket cleanup, then binds the socket.
  If `net.Listen` fails after lock acquisition, the run lock is
  released. The run lock is held until `Close`. Identity write/remove
  are wired through new `ServerConfig.IdentityFn` so the caller decides
  pid/exec/version (lifecycle stays free of `app`-only knowledge).
  Stale-socket cleanup widens to enumerate the four cells:

  | Socket | Run lock | Behavior on Listen |
  |---|---|---|
  | absent | free | bind cleanly |
  | present | free | probe Status; reachable → refuse; else stale → remove socket + identity + cooldown, then bind |
  | absent | held | refuse (mid-startup, or orphan socket cleanup that crashed) |
  | present | held | refuse (compatible/owned) |

  A fifth degenerate case — socket path exists and is not a socket — is
  unchanged from current code: refuse with `SocketUnavailableError`.
- `internal/daemon/daemon.go` — no taxonomy changes;
  `SocketUnavailableError` reasons widen to mention "lock held" /
  "stale identity" where applicable.

Wired in this slice (`internal/app/commands.go`):

- `runDaemonStart` populates `ServerConfig.IdentityFn` with the daemon's
  pid, start time, executable, socket, log path, and version. The
  launcher itself is deferred to AUR-265.

Deferred to AUR-265:

- The shared lifecycle launcher (start lock, spawn, readiness polling,
  cooldown integration). AUR-263 ships only the primitives.

## Wire shape

```go
package lifecycle

type Paths struct {
    Socket        string
    RunLock       string // <socket>.lock
    StartLock     string // <socket>.start.lock
    Identity      string // <socket>.identity.json
    CooldownMark  string // <socket>.start.cooldown   (renamed to avoid type-vs-field name clash)
}

// PathsFor canonicalizes the socket path via filepath.Abs + filepath.Clean
// so two callers using equivalent relative/absolute forms derive the same
// sidecar paths. Symlinks are not resolved (the socket parent may not exist
// yet); callers must agree on a canonical form upstream.
func PathsFor(socket string) (Paths, error)

// Errors are package-level sentinels so callers can errors.Is on them.
var ErrLockHeld         = errors.New("lifecycle: run lock held by another process")
var ErrIdentityNotOwned = errors.New("lifecycle: identity sidecar not owned by caller")

type RunLock struct{ /* fd, path */ }
func AcquireRunLock(p Paths) (*RunLock, error) // O_CLOEXEC, LOCK_EX|LOCK_NB → ErrLockHeld
func (l *RunLock) Release() error

// IsRunLockHeld probes the run lock with LOCK_NB and immediately releases on
// success. Returns true if some other process currently holds it. Used by
// stale-socket cleanup to inspect ownership without taking the lock.
func IsRunLockHeld(p Paths) (bool, error)

type StartLock struct{ /* fd, path */ }
func AcquireStartLock(p Paths) (*StartLock, error) // O_CLOEXEC, LOCK_EX (blocking)
func (l *StartLock) Release() error

type Identity struct {
    PID       int       `json:"pid"`
    StartTime time.Time `json:"start_time"` // RFC3339 UTC; uniquely tags the daemon to defend against PID reuse
    Exec      string    `json:"exec"`       // canonical executable path (resolved)
    Socket    string    `json:"socket"`
    Log       string    `json:"log"`
    Version   string    `json:"version"`
}
// WriteIdentity writes atomically (tmp file + rename) at 0o600.
func WriteIdentity(p Paths, id Identity) error
func ReadIdentity(p Paths) (Identity, error)
// RemoveIdentityIfOwned removes the sidecar only when the on-disk
// (pid, start_time) match the caller. Returns ErrIdentityNotOwned on
// mismatch (so the caller knows it did NOT clean up). Missing file → nil.
func RemoveIdentityIfOwned(p Paths, owner Identity) error

type Cooldown struct {
    Until   time.Time `json:"until"`
    Reason  string    `json:"reason"`
    LogPath string    `json:"log_path,omitempty"`
}
func RecordCooldown(p Paths, c Cooldown) error
// ReadCooldown returns active=true only when the cooldown is present AND
// has not expired against now(). Caller compares by time, not the bool name.
func ReadCooldown(p Paths, now func() time.Time) (cd Cooldown, active bool, err error)
func ClearCooldown(p Paths) error

type StartOutcome int
const (
    Started        StartOutcome = iota
    AlreadyRunning
    Failed
)

type StartFailureReason string
const (
    ReasonNone               StartFailureReason = ""
    ReasonTrustGate          StartFailureReason = "trust_gate"
    ReasonSpawnFailed        StartFailureReason = "spawn_failed"
    ReasonReadinessTimeout   StartFailureReason = "readiness_timeout"
    ReasonChildExitedEarly   StartFailureReason = "child_exited_early"
    ReasonStaleSocketRefused StartFailureReason = "stale_socket_refused"
    ReasonOther              StartFailureReason = "other"
)

type StartResult struct {
    Outcome StartOutcome
    Reason  StartFailureReason // populated when Outcome == Failed
    Detail  string             // human-readable diagnostic
}
```

The `StartLock`/`RunLock` files are opened via `os.OpenFile(...,
O_CREATE|O_RDWR|O_CLOEXEC, 0o600)` and locked via `syscall.Flock` (advisory).
Only `O_CLOEXEC` is needed; `flock` is associated with the open file
description, not the fd, so post-`exec` the kernel releases the OFD reference
when the inherited fd is closed by `O_CLOEXEC`. The plan does not mix
`F_SETLK` with `flock`.

Local-FS assumption: lifecycle sidecars must live on a local filesystem
(advisory locks over NFS are ill-defined). The default socket path
`~/.local/state/tprompt/daemon.sock` satisfies this; documented in AUR-270.

## Tests

* `paths_test.go` — relative + absolute socket paths derive the same
  sidecar paths via canonicalization; weird suffixes (already-suffixed
  `.sock`, no extension) are stable.
* `runlock_test.go` — second acquire returns `errors.Is(err, ErrLockHeld)`;
  release+reacquire works; `IsRunLockHeld` reflects state and never
  takes the lock for longer than the probe; concurrent acquires from N
  goroutines elect exactly one winner.
* `startlock_test.go` — concurrent acquires from N goroutines serialize
  through the lock with FIFO-ish behavior (no specific order asserted,
  only mutual exclusion); release+reacquire works.
* `cloexec_test.go` — primary assertion: after open, `fcntl(F_GETFD)`
  reports `FD_CLOEXEC`. Secondary assertion (skip on macOS missing
  `flock(1)`): exec a small child via `os.StartProcess` without passing
  the lock fd as an `ExtraFile`; the child opens the same lockfile
  fresh and acquires `LOCK_EX|LOCK_NB`. The parent still holds the OFD,
  so the child's acquire MUST fail (validating the parent's lock survives
  the exec rather than `O_CLOEXEC` per se — `O_CLOEXEC` is asserted
  directly via `F_GETFD`).
* `identity_test.go` — write/read roundtrip preserves all fields and
  uses RFC3339 UTC for `StartTime`; `RemoveIdentityIfOwned` returns
  `ErrIdentityNotOwned` when pid mismatches OR when start_time
  mismatches (PID-reuse defense); missing file is nil; effective
  permission bits are 0600 under a known umask; write is atomic
  (assertion: a parallel reader either sees the old content or the new
  full content, never a truncated partial).
* `cooldown_test.go` — record + read within TTL returns active=true;
  expired cooldown reads as active=false; clear is idempotent;
  cooldown JSON file is 0600.
* `result_test.go` — String() and zero-value invariants for
  StartOutcome / StartFailureReason / StartResult.
* `internal/daemon/server_test.go` — extend with:
  - Listen acquires the run lock before stale-socket cleanup; if stale
    cleanup fails, the run lock is released before returning.
  - All four matrix cells (each as a named subtest).
  - Listen failure after lock acquisition releases the lock so a retry
    succeeds.
  - Close removes the identity sidecar only when the on-disk identity
    matches the daemon's own.
  - Logging assertion: lifecycle events do not log prompt body or
    clipboard content (existing executor invariant guards real jobs;
    the new lifecycle event logs need their own assertion).

## Risks / decisions

* `syscall.Flock` is portable across Linux + macOS; Windows is out of
  scope for the project. We do not need `golang.org/x/sys/unix`.
* `O_CLOEXEC` portability: `os.OpenFile` honors `O_CLOEXEC` on Linux
  and macOS via `syscall.O_CLOEXEC`. No build tags needed.
* Identity sidecar permissions: 0600 is enforceable on macOS/Linux.
  Tests assert `Stat().Mode().Perm() == 0o600`.
* Cooldown TTL: 10 s, hard-coded for now; documented in AUR-270.
* The cooldown file is JSON so future fields (`attempts`, `last_error`)
  can be added without breaking older readers.

## Out of scope (deferred to AUR-264/265/266)

* Splitting `daemon run` from `daemon start`.
* Backgrounding `daemon start`.
* Default-on TUI auto-start.
* macOS trust gate.
* User-visible recovery messages.
