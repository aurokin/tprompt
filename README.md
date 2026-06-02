# tprompt

`tprompt` is a tmux-first CLI for injecting markdown-backed prompts into a
target tmux pane as though the user typed or pasted them.

The core workflow is built for tmux popups:

1. Open `tprompt` in a popup.
2. Select a prompt or the clipboard row.
3. Let the TUI exit.
4. A short-lived handoff worker waits until the original target pane is active again.
5. The selected content is injected into that pane.

That deferred handoff avoids sleep-based popup timing and keeps delivery tied
to tmux state. Nothing is injected while the popup is still open — the worker
waits until it closes and the target pane is active again, so the brief pause
before the prompt lands is expected, not a failure.

## Quickstart

```bash
tprompt new code-review              # scaffold ~/.config/tprompt/prompts/code-review.md
$EDITOR "$(tprompt show code-review | head -1 | cut -d' ' -f2)"  # or just open it
tprompt list                         # confirm it loads with a board key
tprompt send code-review --target-pane "$TMUX_PANE"  # $TMUX_PANE = your current pane inside tmux
```

The shell example uses `$TMUX_PANE` because your shell expands it to the current
pane. In a tmux *binding*, use the literal `#{pane_id}` instead — tmux expands
that at trigger time. See [examples/tmux-bindings.md](examples/tmux-bindings.md).

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
tprompt tui                          # or --target-pane <id>; from a tmux binding pass '#{pane_id}'
tprompt doctor
tprompt init
```

Bare `tprompt` dispatches to `tprompt tui` when stdin is a tty and `$TMUX` is
set. Outside tmux, it prints help.

To wire the popup workflow into tmux, run `tprompt init` — it prints the exact
binding to add to your tmux config (it never edits the config itself). See
[examples/tmux-bindings.md](examples/tmux-bindings.md) for the full set.

## Current Contract

- Prompt source of truth is markdown files loaded from global prompt sources and an optional project overlay.
- Prompt IDs are filename stems; directories organize files but do not namespace IDs.
- Duplicate prompt IDs within a source tier are invalid; global/project collisions resolve through the documented priority policy.
- Frontmatter is metadata only; only the markdown body is delivered.
- Direct `send` and `paste` deliver synchronously through tmux. They never start, contact, or depend on a background service.
- TUI selections are submitted to a short-lived handoff worker for verified deferred delivery. The TUI does not require a long-running daemon, does not auto-start one, and still exits before injection.
- Default delivery mode is bracketed paste via `tmux load-buffer` and `paste-buffer -p`.
- `type` mode is available as a fallback using `send-keys -l`.
- `--enter` is opt-in and sends Enter outside the paste wrapper.
- Clipboard reads are same-host only.
- Sanitization defaults to `safe`; `off` and `strict` are explicit config/flag choices.
- Deferred-job failures are surfaced through `tmux display-message` and the configured log.

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

## Install

### Requirements

- **tmux** is required — `tprompt` delivers into tmux panes by shelling out to `tmux`.
- **Clipboard** (for `tprompt paste` and the TUI clipboard row): macOS has
  `pbpaste` built in; on Linux install one of `wl-paste` (Wayland), `xclip`, or
  `xsel`. `send`-only workflows need no clipboard tool.

### Platforms

| Platform | How to install |
|---|---|
| macOS Apple Silicon (arm64) | prebuilt signed + notarized binary |
| Linux x86_64 / arm64 | prebuilt tarball |
| macOS Intel (amd64) | build from source — no prebuilt binary yet |
| Windows | out of scope |

### Prebuilt binaries

Tagged releases ship signed and notarized macOS Apple Silicon binaries plus
Linux x86_64 / arm64 tarballs. Install via [mise](https://mise.jdx.dev/) (uses
[ubi](https://github.com/houseabsolute/ubi)):

```bash
mise use -g ubi:aurokin/tprompt@latest
```

Or grab a tarball from the [GitHub Releases page](https://github.com/aurokin/tprompt/releases)
and verify with `shasum -a 256 -c SHA256SUMS` (macOS) or `sha256sum --check SHA256SUMS` (Linux). The macOS binary is
signed and notarized; see [docs/lifecycle/macos-release-signing.md](docs/lifecycle/macos-release-signing.md)
for the details.

> **Note:** releases publish from drafts, so right after a tag push `@latest`
> can briefly resolve to the previous version (or nothing, for a first release)
> until the operator publishes the draft. If an install can't find the version,
> wait for the release to go public.

### From source

```bash
go install github.com/hsadler/tprompt/cmd/tprompt@latest
```

For a dev build instead, follow `Tool Bootstrap` and `make build` below.

## Tool Bootstrap

This repo includes a project-local `mise.toml` for the pinned Go toolchain and
CLI tooling used by the health gate:

```bash
mise install
```

That installs:

- `go 1.26.3`
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
