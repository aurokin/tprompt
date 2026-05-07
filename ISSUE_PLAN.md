# ISSUE_PLAN — AUR-266 Default TUI auto-start

## Goal

Flip the daemon auto-start default to ON for daemon-backed TUI flows.
After AUR-265 the launcher is non-blocking and structured; after
AUR-314 it has a real darwin trust gate. AUR-266 makes the new
launcher the default user experience: bare `tprompt` (and explicit
`tprompt tui`) inside tmux+tty auto-start the daemon when needed,
without the user opting in.

## Files

Edit:

- `internal/config/config.go` — flip `Default().DaemonAutoStart` from
  `false` to `true`.
- `internal/app/tui.go`:
  - flag default flips to `true`.
  - add `--no-daemon-auto-start` boolean flag as the readable
    opt-out alias. When set, it forces `daemonAutoStart=false` for
    that invocation regardless of config and short-circuits the
    auto-start branch.
  - error if BOTH `--daemon-auto-start=true` and
    `--no-daemon-auto-start` are set on the same command (an obvious
    user mistake; we'd rather fail loud than silently pick one).
  - rewrite the `Long` help block so it documents the new default
    and the opt-out flag rather than the old opt-in framing.
- `internal/config/config_test.go`:
  - `TestDefaultConfig` flips: `Default().DaemonAutoStart` should now
    be `true`.
  - `TestNormalizeThreadsDaemonAutoStart` and similar tests stay
    semantically equivalent but the `cfg.DaemonAutoStart = true`
    seeds become redundant; we keep them as redundant-but-correct so
    a future flip back catches regression.
  - Add `TestDefaultConfigDaemonAutoStartIsOn` as a single-line
    contract check that's hard to lose under a refactor.
- `internal/app/tui_test.go`:
  - Existing tests that set `c.DaemonAutoStart = true` still pass;
    they're now redundant. Leave them as-is for clarity.
  - Add `TestTUI_DaemonAutoStartDefaultOn` — config left at default,
    no flag, asserts launcher is invoked.
  - Add `TestTUI_NoDaemonAutoStartFlagOptsOut` — `--no-daemon-auto-start`
    on its own (with default-on config) skips the launcher and
    surfaces the SocketUnavailableError directly.
  - Add `TestTUI_ConflictingAutoStartFlagsErrors` — both
    `--daemon-auto-start` and `--no-daemon-auto-start` set returns a
    cobra error.
  - Update `TestTUI_DaemonAutoStartFlagFalseOverridesConfig` if it
    relies on `--daemon-auto-start=false` being the canonical opt-out;
    this still works since the flag is still bool with a default,
    but the test should also confirm the alias works the same way.
- `internal/app/help_test.go` — add `--no-daemon-auto-start` to the
  `tui` help expectations.
- `docs/storage/config.md` — flip the documented default to `true`,
  flip the example block, explain the opt-out alias.
- `docs/commands/tui-flow.md` — drop the "opt in via flag/config"
  framing, document the default-on behavior, and the opt-out flag.
- `docs/commands/daemon.md` — same.

## Behavioral contract

Resolution order for "should this TUI invocation auto-start?":

1. If `--no-daemon-auto-start` is set → false.
2. If `--daemon-auto-start` was explicitly set on the command line →
   honor its value (so `--daemon-auto-start=false` still works).
3. Otherwise → fall back to `cfg.DaemonAutoStart`, which now defaults
   to true.

Concretely the resolver is updated to consult both flags:

```go
func (f tuiFlags) daemonAutoStartEnabled(cfg config.Resolved) bool {
    if f.noDaemonAutoStart { return false }
    if f.daemonAutoStartSet { return f.daemonAutoStart }
    return cfg.DaemonAutoStart
}
```

`runTUI` validates the conflict before reaching the resolver:

```go
if f.daemonAutoStartSet && f.noDaemonAutoStart {
    return errors.New("tui: --daemon-auto-start and --no-daemon-auto-start are mutually exclusive")
}
```

## Acceptance-criteria mapping

- "daemon_auto_start defaults to enabled" → config.go default flip.
- "Config tests that currently assert DaemonAutoStart=false are
  inverted" → config_test.go.
- "tprompt tui auto-starts a detached daemon when no daemon is
  reachable" → already works after AUR-265; the default flip turns it
  on for users who didn't set the flag/config.
- "Bare tprompt dispatching to TUI inside tmux gets the same
  behavior" → bare-dispatch already routes through runTUI (root.go
  dispatchArgs), so flipping the default propagates automatically.
- "Prompt selection and clipboard-backed TUI delivery both get the
  same auto-start behavior" → both go through runTUI; nothing extra.
- "TUI auto-start calls the shared lifecycle launcher directly" →
  already true after AUR-265.
- "The launcher receives a typed start intent" → already
  `IntentImplicitTUI`.
- "TUI implicit auto-start applies the macOS executable trust policy
  from AUR-314 before spawning on macOS" → already true after AUR-314
  wires `applife.ProductionAssessor()` into `productionNewLauncher`.
- "Existing config and explicit TUI flag behavior can disable
  auto-start for that invocation" → resolver order above.
- "--daemon-auto-start=false remains supported, and help/docs either
  explain it clearly or a clearer --no-daemon-auto-start alias is
  added" → both: keep `--daemon-auto-start=false` working, add
  `--no-daemon-auto-start` alias.
- "Auto-start treats already-running daemons as success and does not
  spawn duplicates" → already true (the launcher's probe).
- "Auto-start failures map to daemon/IPC errors, mention the daemon
  log path where relevant" → already true (`launcherFailureMessage`).
- "TUI auto-start recovery guidance only suggests options accepted by
  the TUI command path" → already true; the launcher's deny message
  now mentions `tprompt daemon start` (a separate command), not a
  TUI flag.
- "Concurrent cold TUI auto-start attempts serialize through the
  lifecycle launcher" → already covered (in-process mutex
  `daemonAutoStartMu` + cross-process start lock).
- "Tests cover [the listed scenarios]" → existing tests cover most;
  this issue adds the default-on, opt-out-alias, and conflict cases.

## Test plan

Inverted from the existing test suite:
- `TestDefaultConfig.DaemonAutoStart` flips to `true`.
- `TestTUI_DaemonAutoStartDefaultOn` (new): nothing set, launcher
  is called.
- `TestTUI_NoDaemonAutoStartFlagOptsOut` (new): `--no-daemon-auto-start`
  bypasses the launcher.
- `TestTUI_ConflictingAutoStartFlagsErrors` (new): both flags set
  errors out.
- Help test flip: tui help mentions `--no-daemon-auto-start`.

Manual smoke: confirm `tprompt tui --target-pane=...` from a
fresh-install state spawns a daemon and surfaces in `tprompt daemon
status`. (Deferred to AUR-269 testscripts; not blocking here.)

## Risks / open questions

- Default flip is a user-visible behavior change. EXPECTATIONS.md
  needs an update; that lands in AUR-270 along with the doctor warning
  for the override env var. The default flip itself is locked in
  MILESTONE_PLAN.md §3.3 (Phase 4).
- The conflict-flag error path requires checking
  `f.daemonAutoStartSet` plus `f.noDaemonAutoStart` in PreRun before
  RunE. Cobra runs PreRun after flag parsing, so the order is fine.

## Out of scope

- Doctor warning for `TPROMPT_UNSAFE_SKIP_TRUST_GATE` (AUR-270).
- DECISIONS.md entry recording the default flip (AUR-270).
- Testscript end-to-end coverage (AUR-269).
