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

## Core Commands

```bash
tprompt list
tprompt show code-review
tprompt send code-review
tprompt paste
tprompt pick
tprompt tui --target-pane '#{pane_id}'
tprompt daemon start
tprompt daemon status
tprompt daemon stop
tprompt doctor
```

Bare `tprompt` dispatches to `tprompt tui` when stdin is a tty and `$TMUX` is
set. Outside tmux, it prints help.

## Current Contract

- Prompt source of truth is a configured directory of markdown files.
- Prompt IDs are filename stems; directories organize files but do not namespace IDs.
- Duplicate prompt IDs are invalid.
- Frontmatter is metadata only; only the markdown body is delivered.
- Direct `send` and `paste` deliver synchronously through tmux.
- TUI selections are submitted to a local daemon for verified deferred delivery.
- Default delivery mode is bracketed paste via `tmux load-buffer` and `paste-buffer -p`.
- `type` mode is available as a fallback using `send-keys -l`.
- `--enter` is opt-in and sends Enter outside the paste wrapper.
- Clipboard reads are same-host only.
- Sanitization is opt-in: `off`, `safe`, or `strict`.
- Deferred-job failures are surfaced through `tmux display-message` and the daemon log.

For the full contract, read [EXPECTATIONS.md](EXPECTATIONS.md).

## Documentation Map

Start with [docs/README.md](docs/README.md). It is the progressive-disclosure
entrypoint for users, maintainers, and implementation agents.

High-value references:

- [DECISIONS.md](DECISIONS.md) - locked product and engineering decisions.
- [docs/architecture/overview.md](docs/architecture/overview.md) - system shape and data flow.
- [docs/commands/cli.md](docs/commands/cli.md) - command behavior and exit codes.
- [docs/testing/harness.md](docs/testing/harness.md) - proof surfaces and test strategy.
- [examples/tmux-bindings.md](examples/tmux-bindings.md) - tmux popup binding examples.

Execution tracking lives in Linear. Repo docs are durable harness engineering
material: behavior contracts, invariants, seams, failure semantics, and proof
surfaces.

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
go test ./...
make check
```

`make check` runs format checking, linting, and the race-enabled test target
defined in the project `Makefile`.

## Out Of Scope

- Prompt templating variables.
- Cross-host clipboard sync.
- Remote targets or distributed daemon behavior.
- Application-specific semantic confirmation.
- Modifier-key combos for TUI prompt keybinds.
- Live clipboard preview inside the TUI.
- GUI or web UI.
