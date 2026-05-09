# ISSUE_PLAN — AUR-270 Lifecycle docs and decisions

## Goal

Lock the daemon-lifecycle behavior in DECISIONS.md and
EXPECTATIONS.md, then sweep doc surfaces for stale references and
add the lifecycle/auto-start narrative.

## Files

Edit:

- `DECISIONS.md` — add §33 Daemon lifecycle architecture (run lock,
  start lock, identity sidecar, cooldown; `daemon start` backgrounded
  + idempotent; `daemon run` foreground; TUI auto-start default-on
  via the shared launcher; macOS trust gate + override; explicit
  recovery paths).
- `EXPECTATIONS.md` — add a Daemon Lifecycle section to the user-
  visible contract (what each command does, when auto-start fires,
  what the macOS trust gate does, the recovery escape hatches).
- `README.md` — confirm the command map mentions `daemon run`
  (foreground) and the `daemon start` background semantics.
- `docs/README.md` — add an entry under "I want to change X" pointing
  to the new lifecycle doc.
- `docs/architecture/overview.md` — sweep for old daemon language.

New:

- `docs/lifecycle/auto-start.md` — narrative covering the lifecycle
  primitives (run lock, start lock, identity, cooldown), the
  StartIntent enum, the macOS trust gate and override, and the
  recovery flow (`tprompt daemon start` and
  `tprompt daemon run` always bypass; debug override for local
  recovery).

Already done in prior issues (no change needed):

- `docs/storage/config.md` — flipped in AUR-266.
- `docs/commands/tui-flow.md` — flipped in AUR-266.
- `docs/commands/daemon.md` — flipped in AUR-266.
- CLI help text for `daemon start`, `daemon run`, `tui`, daemon menu —
  updated in AUR-264/265/266.

## Acceptance-criteria mapping

| Criterion | Where |
|---|---|
| daemon start docs say backgrounded + idempotent | docs/commands/daemon.md (existing) + DECISIONS §33 |
| daemon run is foreground | DECISIONS §33 + EXPECTATIONS Daemon Lifecycle |
| TUI default-on auto-start | EXPECTATIONS + tui-flow.md (existing) |
| daemon_auto_start defaults to enabled | docs/storage/config.md (existing) |
| send/paste/doctor don't auto-start | EXPECTATIONS Daemon Lifecycle |
| stale docs purged | sweep README + docs/ |
| help text current | already done; spot-check |
| compatible daemon definition | DECISIONS §33 + lifecycle/auto-start.md |
| macOS signing/notarization expectation | lifecycle/auto-start.md |
| ad-hoc/quarantined/unnotarized/invalid/Gatekeeper-rejected | lifecycle/auto-start.md |
| debug override description | lifecycle/auto-start.md |
| explicit recovery paths | lifecycle/auto-start.md |
| daemon-backed vs direct-path distinction | EXPECTATIONS (existing) |

## Out of scope

- Code changes (no doctor warning for the override env var; the
  README/docs cover its meaning, and the launcher's pre-spawn
  diagnostic logs `trust=allow_override` already).
- Milestone-wide docs refactor (separate task: progressive disclosure
  + harness engineering).
