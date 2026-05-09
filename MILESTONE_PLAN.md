# Milestone Plan — AUR-183 Daemon lifecycle auto-start

Status: in progress on local branch `aur-183`. All commits land on this
branch. The milestone is delivered as one push at the end (per the loop's
instructions; this overrides the per-issue PR norm in CLAUDE.md for this
milestone). ISSUE_PLAN.md is rewritten per issue and committed alongside
that issue's implementation, so the historical record per issue is preserved.

## Goal

Make the daemon auto-start automatically the first time `tprompt` needs it,
enforce one-daemon-per-socket across every entry point, split `daemon run`
into an explicit foreground command, and preserve daemon-free guarantees for
`send`, `paste`, `doctor`, and `daemon status`.

## Issue order

Each issue is a vertical slice with tests, ordered so each builds on the
seams the previous landed.

1. **AUR-263 — Lock daemon ownership.** Foundational lifecycle primitives
   inside a new `internal/daemon/lifecycle/` package (pure file/lock
   primitives; no CLI knowledge, no `internal/app` import dependency):
   * `Paths(socket)` deriving `<socket>.lock`, `<socket>.start.lock`,
     `<socket>.identity.json`, `<socket>.start.cooldown` from the configured
     socket path.
   * `RunLock`: exclusive `syscall.Flock(LOCK_EX|LOCK_NB)` on
     `<socket>.lock`. Held by the daemon process for its lifetime.
     `O_CLOEXEC` so spawned children do not inherit it.
   * `StartLock`: `syscall.Flock(LOCK_EX)` on `<socket>.start.lock`. Held
     by the parent during a start attempt; released after readiness or
     definitive failure. Serializes concurrent cold starts. `O_CLOEXEC` so
     the spawned daemon child cannot inherit it (the child must be free of
     the start lock by the time it tries to grab the run lock).
   * `Identity`: write/read/match-and-remove of
     `<socket>.identity.json` containing `{pid, start_time, exec, socket,
     log, version}`. Removal only succeeds when the on-disk pid matches
     the caller's pid.
   * `StartResult`: typed result `{Started | AlreadyRunning | Failed}` with
     reasons distinguishable for tests and for caller error mapping.
   * `Cooldown`: file-backed marker at `<socket>.start.cooldown`. TTL ~10 s.
     Set by failed implicit starts; consulted by future implicit starts
     before doing platform trust assessment or spawn. Explicit
     `tprompt daemon start` and `tprompt daemon run` always bypass and
     clear the cooldown — explicit intent is recovery and overrides the
     gate. Cooldown error embeds the daemon log path so users can find
     diagnostics.
   * Conservative stale-socket cleanup stays in
     `internal/daemon/server.go` but is updated to consult the run lock
     and identity sidecar. The four-cell matrix is covered:

     | Socket exists | Lock held | Cleanup behavior |
     |---|---|---|
     | yes | yes | refuse (compatible/owned) |
     | yes | no | probe Status; if reachable refuse, otherwise unlink stale + remove sidecars |
     | no | yes | inspect identity; if pid alive refuse, otherwise treat as orphan and clean up |
     | no | no | no-op; safe to bind |

   * Tests cover lock acquire/release, fd-no-inherit-across-fork (assert
     `O_CLOEXEC` via a helper that exec's a small probe), identity
     match-on-remove, all four stale matrix cells, cooldown gating
     including explicit-command bypass, and concurrent start lock
     serialization.

   No user-facing CLI behavior changes in this commit. Doc strings and
   helpers are wired but unconsumed.

2. **AUR-264 + AUR-265 — Split `daemon run` and background `daemon start`.**
   Combined into a single tight sequence on this branch (per reviewer note:
   landing AUR-264 alone leaves an awkward in-between where `daemon start` is
   still foreground and `run` is no longer aliased; on this all-in-one branch
   we never publish that intermediate).
   * AUR-264 step: promote `daemon run` to its own command, drop `run` alias
     from `daemon start`. `daemon run` acquires `RunLock` *before* socket
     bind / stale cleanup. On collision (lock or socket) it returns an
     `ExitDaemon` IPC error mentioning the existing daemon. Existing
     root-command alias test inverts.
   * AUR-265 step: convert `daemon start` to a backgrounded launcher.
     Implementation lives in a new `internal/app/lifecycle/launcher.go`
     (NOT inside `internal/daemon` — keeps the import direction clean).
     The launcher:
     1. Tries `Status` first — if reachable, idempotent success
        ("already running") before any tmux adapter or log file is opened.
     2. Acquires `StartLock`. After acquisition, re-checks `Status` (the
        previous holder may have just started a daemon).
     3. Runs the macOS trust gate when `Intent == ImplicitTUI` (no-op on
        non-darwin and for explicit intents).
     4. Appends pre-spawn diagnostics to the daemon log (best-effort) —
        parent pid, intent, exec path, canonical exec, socket path,
        lifecycle paths, daemon log path, env removed/forwarded, macOS
        gate result.
     5. `exec.Command(self, "daemon", "run", "--config", ...).Setsid`,
        stdout discarded, stderr appended to daemon log.
     6. Polls Status (default 5 s readiness, configurable via
        `lifecycle.Options`) until success or timeout/process exit.
     7. Returns `Started`, `AlreadyRunning`, or `Failed{ reason, err }`.

     `internal/app/deps.go:productionStartDaemon` is removed in this same
     commit and replaced by a `Launcher` field. The launcher spawns
     `daemon run` (not `daemon start`), eliminating the recursion risk
     called out by the reviewer; an assertion test confirms the spawn argv
     is `[..., "daemon", "run", ...]`.

3. **AUR-314 — macOS implicit auto-start trust gate.** Lives in
   `internal/app/lifecycle/trust_darwin.go` / `trust_other.go`:
   * `Assess(execPath) → AssessResult{Allow|RejectAdHoc|RejectInvalid|RejectGatekeeper|Indeterminate}`.
   * Uses `codesign --verify` and `spctl --assess --type execute`. CLI
     binaries that are validly signed but rejected as "not an app bundle"
     are allowed.
   * Debug override env var: `TPROMPT_UNSAFE_SKIP_TRUST_GATE=1` (explicit
     value, not just presence). Short-circuits before invoking
     `codesign`/`spctl`. `tprompt doctor` will surface a warning when set
     (added under AUR-270 docs/decisions).
   * Explicit `daemon start` and `daemon run` bypass via the `StartIntent`
     parameter — trust gate only runs for `Intent == ImplicitTUI`.
   * No caching in this milestone (one cold start per session is the
     common case; caching tracked as a follow-up if profiling proves it).
   * Unit tests use a fake assessor injected through
     `lifecycle.Options.AssessExecutable`.

4. **AUR-266 — Default TUI auto-start.**
   * Flip `daemon_auto_start` default to true; invert config tests.
   * Tprompt's TUI preflight calls the lifecycle launcher with
     `Intent: ImplicitTUI` (the typed start-intent the reviewer asked
     for). No more `productionStartDaemon` indirection.
   * `--daemon-auto-start=false` and the config opt-out continue to
     disable implicit start. CLI flag takes precedence over config when
     explicitly set; otherwise config wins. Add a clearer
     `--no-daemon-auto-start` alias as required by the issue.
   * Implicit-start failure mapping: daemon/IPC error class, error message
     mentions the daemon log path. Recovery hint only suggests options
     accepted by the TUI command (e.g. retry with `--daemon-auto-start=false`
     and run `tprompt daemon start` manually — never suggests flags the
     TUI command does not accept).
   * Concurrent cold-start serialization comes for free from the start
     lock; tests exercise two concurrent goroutines and assert exactly one
     daemon child is spawned.
   * Bare `tprompt` (DECISIONS §30 dispatch into `tprompt tui`) inherits
     this behavior automatically since it routes through `runTUI`. AUR-269
     adds an explicit testscript for this path so it does not regress.

5. **AUR-267 — Preserve non-daemon command contracts.** Tests-only commit
   that locks in:
   * `send`, `paste`, `doctor`, and `daemon status` never invoke the
     lifecycle launcher even when `daemon_auto_start = true`.
   * `daemon status` continues to call only the read-only Status RPC and
     never mutates lifecycle sidecars.
   * `send`/`paste` never construct a daemon client.

6. **AUR-268 — `daemon stop` across all daemon modes.** Verify (and adjust
   if needed) that `stop` works against:
   * a daemon spawned by `Intent: ImplicitTUI`,
   * a daemon spawned by explicit `daemon start`,
   * a foreground `daemon run` process,
   * a daemon mid-job-injection,
   * a daemon in the start-window (start lock held, socket not yet bound).

   The first four work today via the existing Stop RPC; the fifth is the
   reviewer's "stop during start" case. Behavior: if `Status` cannot reach
   any daemon and the start lock is held, `stop` reports
   `daemon not running` (the in-flight start will either succeed and the
   user can re-run stop, or fail and there is nothing to stop). Idempotent
   when nothing is running.

7. **AUR-269 — End-to-end lifecycle testscripts.** Migrate testscripts
   that backgrounded `daemon start` to use `daemon run &` where foreground
   process control is needed (`tui_cancel`, `tui_clipboard_happy`,
   `tui_clipboard_oversize`, `tui_pane_missing`). Update
   `tui_daemon_unreachable` to use the explicit auto-start opt-out. Add
   testscripts for:
   * Cold-start auto-spawn from `tprompt tui --target-pane` and bare
     `tprompt` in tmux+tty.
   * Daemon reuse on second invocation.
   * Concurrent cold starts (two parallel commands) → one daemon.
   * `daemon start` vs `daemon run` collision (both directions).
   * `daemon start` while `daemon run` is active → idempotent
     already-running.
   * `daemon stop` against each launch state.
   * macOS trust assessment with fake `codesign`/`spctl` shims (assertion
     uses fake assessment commands per the issue).
   * Repeated failed implicit starts trigger cooldown and avoid platform
     trust assessment / spawn until cooldown expires.

8. **AUR-270 — Lifecycle docs and decisions.** Update
   `docs/commands/cli.md`, `docs/commands/daemon.md`,
   `docs/storage/config.md`, `docs/testing/harness.md`, `EXPECTATIONS.md`,
   `DECISIONS.md`, README, CLI help, and example tmux bindings so they
   match the new contract. Define "compatible daemon" once
   (`docs/commands/daemon.md`) and link from elsewhere. Add the macOS
   signing/notarization expectation, the
   `TPROMPT_UNSAFE_SKIP_TRUST_GATE` debug override, and explicit
   recovery-path guidance. Add new outcome values for lifecycle events to
   the daemon log outcome table where relevant. Add `tprompt doctor`
   warning for the trust override env var.

## Architectural seams

* **`internal/daemon/lifecycle/`** (new). Pure file/lock primitives:
  paths, run lock, start lock, identity sidecar, cooldown marker,
  start-result type. No knowledge of `internal/app`, cobra, or CLI flags.
* **`internal/app/lifecycle/`** (new). The `Launcher` (spawn semantics,
  config-path propagation, Status polling, error mapping), `StartIntent`
  enum (`ImplicitTUI | ExplicitStart | ExplicitRun`), and the macOS
  trust gate. Imports `internal/daemon/lifecycle`. CLI-side glue lives
  here so the import direction `app → daemon` is preserved.
* **`internal/app/deps.go`** plumbs the `Launcher` through `Deps` so tests
  inject a fake. `productionStartDaemon` is removed in AUR-265.

## Open contracts locked in by this plan

* Cooldown: file-backed at `<socket>.start.cooldown`, ~10 s TTL,
  bypassed and cleared by explicit `daemon start`/`daemon run`,
  consulted by `Intent: ImplicitTUI` only.
* `--daemon-auto-start` flag precedence: explicit CLI wins over config;
  unset CLI defers to config; new `--no-daemon-auto-start` alias added.
* Trust override: `TPROMPT_UNSAFE_SKIP_TRUST_GATE=1` (value-required).
  `doctor` warns when set.
* `daemon status` is strictly read-only — no sidecar cleanup, no socket
  unlink.
* `daemon run` does not get a `--foreground` flag in this milestone —
  the testscript migration in AUR-269 is the entire migration story.

## Documentation refactor (after issues land)

After AUR-270 closes, do a Progressive-Disclosure / Harness deletion-only
pass: validate every entry point in `docs/README.md` still routes to the
narrowest durable contract, ensure `docs/commands/daemon.md` is the single
owner of lifecycle invariants, drop any milestone scaffolding that does
not belong in the durable harness. No new content edits in this pass —
those happen inside AUR-270.

## Health gate per commit

Every commit runs `make check` (fmt, lint, race tests). Testscripts in
`cmd/tprompt/testdata/script` exec real `tmux`. Confirm with the user
before running broad `go test ./...` if their shell holds tmux state worth
keeping.
