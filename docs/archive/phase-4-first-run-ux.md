# Phase 4: First-run UX (archived PRD)

Historical. Live contract: DECISIONS §30, EXPECTATIONS, `docs/commands/cli.md`.

## Problem

A first-time-user audit (2026-05) found the product forgiving once set up, but
reaching the flagship tmux popup workflow was the weak link: onboarding docs
had correctness bugs, there was no guided popup setup, and bare `tprompt` in a
normal pane just failed.

## Locked decisions

- **`tprompt init` is print-only.** It prints setup instructions with
  progressive disclosure (concise default, `--more` full menu, `--snippet` raw
  binding). It never edits `tmux.conf` or mutates tmux. The authoritative
  binding text lives in the binary, not in an examples doc.
- **Bare `tprompt` in a normal pane runs direct mode** against the current
  pane, with a banner nudging popup setup (`tprompt init`). This amended §30
  and EXPECTATIONS deliberately: a working-but-alerting mode beats a hard
  failure. Safety: direct mode triggers only when tprompt can confirm it is not
  in a popup; on any uncertainty it keeps the exit-2 error.
- Onboarding doc fixes were batched into one pass (sanitize default, Quickstart,
  prerequisites, config foot-gun, platform matrix).

## Slices

`init` shipped first because `doctor` binding detection and direct mode both
point at it. Then: `doctor` warns when no popup binding is wired; direct mode;
`new` prints a tty-gated "add a body" hint; `new --edit` / `--force`; a TUI
on-screen legend for board keys; a `--help` nudge on flag errors; `handoff
--job` exit-code consistency; guidance on an empty `list` / first run; clarified
release-org and module-path identity.
