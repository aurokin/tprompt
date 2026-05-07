# tprompt

`tprompt` is a tmux-first CLI for injecting markdown-backed prompts into a
target tmux pane as though the user typed or pasted them.

The core workflow is built for tmux popups:

1. Open `tprompt` in a popup.
2. Select a prompt or the clipboard row.
3. Let the TUI exit.
4. The daemon waits until the original target pane is active again.
5. The selected content is injected into that pane.

That deferred handoff avoids sleep-based popup timing and keeps delivery tied
to tmux state.

## Quickstart

```bash
tprompt new code-review              # scaffold ~/.config/tprompt/prompts/code-review.md
$EDITOR "$(tprompt show code-review | head -1 | cut -d' ' -f2)"  # or just open it
tprompt list                         # confirm it loads with a board key
tprompt send code-review --target-pane '#{pane_id}'
```

`tprompt new` auto-creates the default global prompts directory on first use,
so a fresh install needs no hand-edited config to start writing prompts. Pass
`--project` to scaffold a per-repo overlay at `<gitroot>/tprompt/<id>.md`
instead.

## Core Commands

```bash
tprompt new code-review
tprompt new project-only --project
tprompt list
tprompt show code-review
tprompt send code-review
tprompt paste
tprompt pick
tprompt tui --target-pane '#{pane_id}'
tprompt daemon start    # spawn detached, idempotent if already running
tprompt daemon run      # foreground (Ctrl-C / SIGTERM to stop)
tprompt daemon status
tprompt daemon stop
tprompt doctor
```

Bare `tprompt` dispatches to `tprompt tui` when stdin is a tty and `$TMUX` is
set. Outside tmux, it prints help.

## Current Contract

- Prompt source of truth is markdown files loaded from global prompt sources and an optional project overlay.
- Prompt IDs are filename stems; directories organize files but do not namespace IDs.
- Duplicate prompt IDs within a source tier are invalid; global/project collisions resolve through the documented priority policy.
- Frontmatter is metadata only; only the markdown body is delivered.
- Direct `send` and `paste` deliver synchronously through tmux. They never start, contact, or depend on the daemon.
- TUI selections are submitted to a local daemon for verified deferred delivery. The daemon auto-starts on demand by default; see [`docs/lifecycle/auto-start.md`](docs/lifecycle/auto-start.md) for the modes, the macOS trust gate, and the `TPROMPT_UNSAFE_SKIP_TRUST_GATE` debug override.
- Default delivery mode is bracketed paste via `tmux load-buffer` and `paste-buffer -p`.
- `type` mode is available as a fallback using `send-keys -l`.
- `--enter` is opt-in and sends Enter outside the paste wrapper.
- Clipboard reads are same-host only.
- Sanitization defaults to `safe`; `off` and `strict` are explicit config/flag choices.
- Deferred-job failures are surfaced through `tmux display-message` and the daemon log.

For the full contract, read [EXPECTATIONS.md](EXPECTATIONS.md).

## Documentation Map

Start with [docs/README.md](docs/README.md). It is the progressive-disclosure
entrypoint for users, maintainers, and implementation agents.

High-value references:

- [AGENTS.md](AGENTS.md) - agent / contributor entry point (also linked as CLAUDE.md).
- [DECISIONS.md](DECISIONS.md) - locked product and engineering decisions.
- [EXPECTATIONS.md](EXPECTATIONS.md) - user-visible behavior contract.
- [docs/architecture/overview.md](docs/architecture/overview.md) - system shape and data flow.
- [docs/commands/cli.md](docs/commands/cli.md) - command behavior and exit codes.
- [docs/implementation/interfaces.md](docs/implementation/interfaces.md) - subsystem seams.
- [docs/testing/harness.md](docs/testing/harness.md) - proof surfaces and test strategy.
- [examples/tmux-bindings.md](examples/tmux-bindings.md) - tmux popup binding examples.

Execution tracking lives in Linear. Repo docs are durable harness engineering
material: behavior contracts, invariants, seams, failure semantics, and proof
surfaces. Do not keep temporary PRDs or issue breakdowns in the repo after they
are uploaded to Linear.

## Tool Bootstrap

This repo includes a project-local `mise.toml` for the pinned Go toolchain and
CLI tooling used by the health gate:

```bash
mise install
```

That installs:

- `go 1.26.2`
- `golangci-lint v2.1.6`
- `gofumpt v0.7.0`
- `goimports v0.26.0`

`make tools` remains available as an alternative bootstrap path.

## Health Gate

```bash
make check
```

`make check` runs format checking, linting, and the race-enabled test target
defined in the project `Makefile`. Testscripts execute real `tmux`; see
[docs/testing/harness.md](docs/testing/harness.md) before running broad test
targets in a shell with tmux state that matters.

## Out Of Scope

- Prompt templating variables.
- Cross-host clipboard sync.
- Remote targets or distributed daemon behavior.
- Application-specific semantic confirmation.
- Modifier-key combos for TUI prompt keybinds.
- Live clipboard preview inside the TUI.
- GUI or web UI.
