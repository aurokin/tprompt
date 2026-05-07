# ISSUE_PLAN — AUR-268 daemon stop across all modes

## Goal

Verify and document that `tprompt daemon stop` works the same way
regardless of how the daemon was started: TUI auto-start, explicit
`daemon start`, or foreground `daemon run`. The implementation is
already mode-agnostic — the launcher in AUR-265 spawns `daemon run`
detached for both auto-start and `daemon start` paths, so all three
modes produce a Server bound to the same socket and tear down via
the same `Server.Close()` lifecycle. AUR-268 is mostly a verification
+ test pass.

## Files

Edit:

- `internal/app/commands.go` — small docstring on `runDaemonStop`
  pinning the mode-agnostic contract.
- `internal/app/daemon_test.go` — add
  `TestDaemonStop_ModeAgnosticAcrossAutoStartBackgroundForeground`
  that exercises the CLI handler with daemon-state matrices
  (foreground-running, background-running, autostart-spawned,
  not-running) and asserts the same handler succeeds in each.

## Existing coverage we lean on

- `TestServerStopRoundTripCancelsRun` — server side: Stop RPC cancels
  Run, idle-for-2s teardown.
- `TestListenAcquiresAndReleasesRunLock` — lifecycle artifacts (run
  lock, identity sidecar, socket file) are released on `Close`, and a
  fresh Listen on the same path then succeeds. Combined, this proves
  that after stop, a follow-up start works regardless of which mode
  spawned the prior daemon.
- `TestDaemonStopPrintsStoppedAfterSocketDisappears` — CLI happy path.
- `TestDaemonStopNoDaemonRunningPrintsClearMessage` — idempotent
  no-daemon case.
- `TestDaemonStopTreatsPeerCloseAsStopped`,
  `TestDaemonStopTreatsPeerCloseOnAckAsNotRunning` — race-condition
  flavors of "daemon already gone".
- `TestDaemonStopTimeoutMapsToExitDaemon` — bounded wait.

## Why mode-agnosticism is structural

After AUR-264/265 the only daemon binary is `daemon run`. Both
implicit (TUI launcher) and explicit (`daemon start` launcher) paths
spawn `tprompt daemon run` detached; the only differences are the
parent's argv and which env-var bypasses the trust gate. Once the
daemon is bound to the socket, all three modes converge on the
same Server lifecycle. `runDaemonStop` connects to the socket and
issues the Stop RPC; nothing in that path depends on parent
provenance.

## Acceptance-criteria mapping

- "stops a daemon created by TUI auto-start" → mode-agnostic: same
  socket, same RPC. Test asserts.
- "stops a daemon created by `daemon start`" → ditto.
- "stops a foreground `daemon run` process" → ditto.
- "remains idempotent when no daemon is running" → already covered.
- "Stop uses existing daemon stop RPC" → unchanged.
- "Tests cover ... across auto-started, background-started,
  foreground-run, and not-running" → the new parameterized test.

## Out of scope

- Production code changes beyond the docstring.
- Testscript-level coverage (lands in AUR-269).
