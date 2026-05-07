# ISSUE_PLAN — AUR-267 Preserve non-daemon command contracts

## Goal

Lock in via tests that direct delivery (`send`, `paste`) and
diagnostics (`doctor`, `daemon status`) do not call the lifecycle
launcher even after AUR-266 made auto-start default-on. Today's
implementation already follows this contract; the issue exists to
prevent regression.

`daemon status` already has a launcher-fatal test
(`TestDaemonStatusDaemonUnreachableExitsDaemon`,
`internal/app/daemon_test.go:169`). AUR-267 adds the equivalent
guards for `send`, `paste`, and `doctor`.

## Files

Edit (test-only):

- `internal/app/send_test.go` — add `TestSend_NeverInvokesLauncher`.
- `internal/app/paste_test.go` — add `TestPaste_NeverInvokesLauncher`.
- `internal/app/doctor_test.go` — add `TestDoctor_NeverInvokesLauncher`.

Each test:
- Builds `workingDeps` with `cfg.DaemonAutoStart = true` so the
  default-on path is exercised.
- Overrides `deps.NewLauncher` with a `t.Fatal`-emitting stub.
- Runs the command. If `NewLauncher` is invoked, the test fails.

The `doctor` test additionally seeds the daemon socket path to a
deliberately-unreachable location so the diagnostic reports
"daemon unreachable" without trying to start one.

## Acceptance-criteria mapping

- "tprompt send never starts or contacts the daemon" → already true;
  send_test launcher-fatal stub locks it in.
- "tprompt paste never starts or contacts the daemon" → ditto.
- "tprompt doctor does not auto-start the daemon" → ditto.
- "tprompt daemon status does not auto-start the daemon" → already
  covered by `TestDaemonStatusDaemonUnreachableExitsDaemon`.
- "Existing direct-send and direct-paste behavior is unchanged" → no
  production code change.
- "Tests prove these commands do not invoke the lifecycle launcher" →
  the new tests.

## Out of scope

- No production code changes.
- No docs changes (the existing tui-flow / daemon docs already note
  that `send`/`paste`/`doctor` are direct-path).
