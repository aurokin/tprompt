# Milestone PRD Archive

Condensed copies of past milestone PRDs, kept for the problem statements and
rationale that the live docs do not repeat. They are **historical**: where they
disagree with [DECISIONS.md](../../DECISIONS.md), [EXPECTATIONS.md](../../EXPECTATIONS.md),
or the subsystem docs, the live docs win. In particular, the background daemon
these PRDs assume was later removed (DECISIONS §33); deferred TUI delivery now
goes through a short-lived handoff worker.

Issue keys such as `AUR-183` refer to the retired tracker and are kept only as
history.

| Milestone | File | Status when archived |
|---|---|---|
| Phase 4: First-run UX | [phase-4-first-run-ux.md](phase-4-first-run-ux.md) | Shipped |
| Phase 5a: TUI command shell | [phase-5a-tui-command-shell.md](phase-5a-tui-command-shell.md) | Shipped; daemon parts superseded by §33 |
| Phase 5b: Built-in Bubble Tea TUI | [phase-5b-bubble-tea-tui.md](phase-5b-bubble-tea-tui.md) | Shipped |
| Multi-source prompts and scaffolder | [multi-source-prompts-and-scaffolder.md](multi-source-prompts-and-scaffolder.md) | Shipped |
| Daemon lifecycle macOS alignment and release signing | [daemon-lifecycle-and-release-signing.md](daemon-lifecycle-and-release-signing.md) | Signing shipped; daemon removed (§33) |
| Wispr Flow snippet import | [wispr-flow-import.md](wispr-flow-import.md) | Shipped (§34) |
| Interactive import and frontmatter parity | [interactive-import.md](interactive-import.md) | Shipped |

Two milestones had only a short summary, recorded here:

- **Phase 6/7 hardening polish.** Release-readiness pass for the MVP: test and
  failure-contract closure, richer `doctor` diagnostics, resolved keybind
  display, help text, readable delivery logs, warning-only post-injection
  verification, a documentation pass, and a final gate. Boundaries: markdown
  prompt files, local tmux delivery, same-host clipboard, single-character TUI
  keybinds, no live clipboard preview, no prompt templating. The coverage map
  is [docs/testing/phase-6-coverage.md](../testing/phase-6-coverage.md).
- **Platform-aware daemon lifecycle (AUR-183).** After the agentscan macOS
  kernel-panic investigation: non-macOS could keep implicit daemon auto-start,
  macOS never auto-starts implicitly, foreground `daemon run` was the macOS
  local path, and detached `daemon start` was signed-only on macOS. Superseded
  by the removal of the daemon (§33).
