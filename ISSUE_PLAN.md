# ISSUE_PLAN — AUR-269 End-to-end lifecycle testscripts

## Goal

Add testscripts that exercise the new daemon lifecycle through the
real CLI, real socket behavior, and an isolated tmux harness. Each
script cleans up its own daemon (no orphans).

## Files

New testscripts in `cmd/tprompt/testdata/script/`:

- `daemon_start_stop_roundtrip.txtar` — `daemon start` spawns a
  detached daemon, `daemon status` reports it running, `daemon stop`
  cleans up. Asserts daemon log has exactly one `outcome=started`
  entry.
- `daemon_start_idempotent_when_running.txtar` — with a daemon
  already up via `daemon run`, calling `daemon start` prints "already
  running" and does NOT spawn a second listener. Daemon log still
  has one `outcome=started`.
- `daemon_run_collision_with_existing.txtar` — with one `daemon run`
  active, a second `daemon run` exits non-zero with daemon/IPC
  classification. Stderr mentions `already running` or `run lock
  held` (the run-lock primitive's wording).
- `tui_auto_start_cold_start.txtar` — TUI invocation with no daemon
  triggers auto-start through the launcher; the daemon is created;
  `daemon stop` cleans up. Sets `TPROMPT_UNSAFE_SKIP_TRUST_GATE=1`
  so the macOS trust gate doesn't reject the test binary (which is
  ad-hoc-signed by `go test`). Comment in the script explains why.
- `tui_auto_start_warm_reuse.txtar` — daemon already running via
  `daemon run`; two TUI invocations succeed without spawning a
  second daemon (verified by daemon log retaining one
  `outcome=started`).

## Existing scripts unchanged

- `tui_cancel.txtar`, `tui_clipboard_happy.txtar`,
  `tui_clipboard_oversize.txtar`, `tui_pane_missing.txtar` were
  migrated to `daemon run &` in AUR-265.
- `tui_daemon_unreachable.txtar` was updated to use
  `--no-daemon-auto-start` in AUR-266.

## What we explicitly do NOT add at testscript level

- **Concurrent cold starts.** Reliable concurrency in testscript
  shell is hard. Already covered by:
  - `internal/app/lifecycle/launcher_test.go::TestLauncherConcurrentCallsExactlyOneSpawn`
    (in-process serialization).
  - `internal/daemon/lifecycle/startlock_test.go` (cross-process
    flock primitive).
- **macOS trust-gate ad-hoc rejection.** Already covered by the
  fake-runner unit tests in
  `internal/app/lifecycle/trust_darwin_test.go` and the integration
  test `trust_darwin_integration_test.go::TestDarwinAssessorIntegrationAdHocReject`
  (real `codesign`/`spctl`). Acceptance criterion explicitly says
  "Platform trust tests use fake assessment commands/results where
  possible rather than relying on the developer machine's real
  Gatekeeper state" — that's exactly what the unit tests do.
- **Debug-override short-circuit before tool invocation.** Covered by
  `TestDarwinAssessorOverrideShortCircuits` (asserts `runner.calls == 0`).
- **Explicit-bypass intent gating.** Covered by
  `TestLauncherTrustGateRejectionImplicitOnly`.
- **Cooldown gating.** Covered by
  `TestLauncherImplicitCooldownGatesNextStart` and
  `TestLauncherImplicitFailureRecordsCooldown`.

## Risks / open questions

- The `daemon run &` background pattern relies on testscript's process
  group being torn down at script exit. The launcher's `Setsid`
  detaching is on the spawn path used by `daemon start`, NOT by
  `daemon run`. So `daemon run &` processes ARE cleaned up by
  testscript automatically; `daemon start`-spawned ones are NOT, which
  is why every `daemon start` testscript ends with `daemon stop`.
- macOS unix-socket path limit (~103 bytes). All scripts use a
  short relative path (`state/daemon.sock`) under the test's `$WORK`,
  matching the existing `tui_clipboard_happy` pattern. We do NOT need
  `$SHORT_TMPDIR` for scripts that don't touch tmux.
- The launcher's executable detection uses `os.Executable()`, which
  in testscript mode returns the testscript binary that re-dispatches
  to `app.RunCLI`. Spawning it with `daemon run` args goes through
  the same code path as production. Verified manually by reading
  `cmd/tprompt/testscript_test.go::TestMain`.

## Acceptance-criteria mapping

| Criterion | Where covered |
|---|---|
| cold-start TUI auto-spawns | `tui_auto_start_cold_start.txtar` |
| second TUI reuses existing | `tui_auto_start_warm_reuse.txtar` |
| concurrent cold starts → 1 daemon | unit tests (see "do NOT add" above) |
| daemon start → daemon stop end-to-end | `daemon_start_stop_roundtrip.txtar` |
| daemon run + daemon stop | already in `tui_clipboard_happy` (`daemon run &`); `daemon stop` end is added by `daemon_run_collision_with_existing.txtar` cleanup |
| daemon run colliding with bg daemon | `daemon_run_collision_with_existing.txtar` |
| daemon start while daemon run active | `daemon_start_idempotent_when_running.txtar` |
| existing testscripts migrated | done in AUR-265 |
| tui_daemon_unreachable updated | done in AUR-266 |
| privacy coverage still passes | health gate |
| no orphan daemons | every script ends with `daemon stop` or `kill-server` |
| isolated socket paths and tmux state | use `$WORK/state/...` and `TMUX_TMPDIR=$SHORT_TMPDIR` |
| macOS ad-hoc reject coverage | unit + integration tests |
| macOS debug override bypass coverage | unit test |
| macOS explicit bypass coverage | launcher unit test |
| concurrent failed implicit starts respect cooldown | launcher unit test |
| trust tests use fake commands | unit tests use fake runner |

## Out of scope

- Documentation updates (lands in AUR-270).
- DECISIONS.md / EXPECTATIONS.md updates (AUR-270).
