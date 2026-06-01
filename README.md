<p align="center">
  <img src="assets/tprompt-logo.png" alt="tprompt logo" width="480">
</p>
<p align="center"><em>A tmux-first CLI for injecting markdown-backed prompts into a target tmux pane.</em></p>

# tprompt

`tprompt` keeps your prompts as markdown files and injects the one you pick into a
target tmux pane — as though you typed or pasted it. The main workflow runs in a tmux
popup: open the popup, pick a prompt (or the clipboard), and the text lands in the
pane you started from.

<p align="center">
  <img src="assets/tprompt-board.png" alt="The tprompt prompt board running in a tmux popup" width="800">
</p>
<p align="center"><em>The prompt board (<code>tprompt tui</code>): pick a prompt by key or arrows, and it injects into the pane you launched from.</em></p>

**Requirements:** a working `tmux` install on Linux or macOS. Windows is outside the
tmux-first workflow.

## Install

Tagged releases ship signed, notarized macOS Apple Silicon binaries plus Linux
x86_64 / arm64 tarballs. Install via [mise](https://mise.jdx.dev/) (uses
[ubi](https://github.com/houseabsolute/ubi)):

```bash
mise use -g ubi:aurokin/tprompt@latest
```

Or grab a tarball from the [GitHub Releases page](https://github.com/aurokin/tprompt/releases)
and verify it with `shasum -a 256 -c SHA256SUMS` (macOS) or `sha256sum --check SHA256SUMS`
(Linux). See [docs/lifecycle/macos-release-signing.md](docs/lifecycle/macos-release-signing.md)
for signing details, or [Building from source](#building-from-source) for a dev build.

## Quickstart

```bash
# 1. Scaffold a prompt — prints the absolute path of the new file.
tprompt new code-review

# 2. Open it in your editor (paste the path from step 1, or extract it):
$EDITOR "$(tprompt show code-review | sed -n 's/^Source: //p')"

# 3. Confirm it's discovered.
tprompt list

# 4. Send it to your current pane to test directly.
tprompt send code-review
```

`tprompt new` auto-creates the default global prompts directory on first use, so a
fresh install needs no hand-edited config. Pass `--project` to scaffold a per-repo
overlay at `<gitroot>/tprompt/<id>.md` instead.

### Wire up the popup

The popup workflow is the point of `tprompt`. Run `tprompt init` to print the exact
tmux binding — it only prints, and never edits your config:

```bash
tprompt init            # popup binding + install steps
tprompt init --snippet  # just the binding line, to append to a config file
tprompt init --more     # also the direct paste/send bindings
```

The binding it prints looks like:

```tmux
bind-key P display-popup -E "tprompt tui --target-pane '#{pane_id}' --client-tty '#{client_tty}' --session-id '#{session_id}'"
```

After you add it and reload tmux, press **prefix + P**: the prompt board opens in a
popup, you press a prompt's key (or move with `↑`/`↓` and press `Enter`), the popup
closes, and the prompt is injected into the pane you launched it from. See
[examples/tmux-bindings.md](examples/tmux-bindings.md) for the full set, including the
direct paste and send bindings.

## Core Commands

```bash
tprompt new code-review              # scaffold a global prompt
tprompt new project-only --project   # scaffold a per-repo overlay prompt
tprompt list                         # list prompts and their board keys
tprompt show code-review             # print a resolved prompt + metadata
tprompt send code-review             # deliver a prompt to a tmux pane
tprompt paste                        # deliver the clipboard to a tmux pane
tprompt pick                         # choose a prompt via your external picker (fzf)
tprompt tui                          # launch the built-in board
tprompt doctor                       # check environment and config
tprompt init                         # print the tmux popup binding
```

Bare `tprompt` dispatches to `tprompt tui` when stdin is a tty and `$TMUX` is set;
outside tmux it prints help. Full behavior and exit codes:
[docs/commands/cli.md](docs/commands/cli.md).

## How delivery works

When you pick a row in the TUI, it writes your selection to a private handoff job,
spawns a short-lived worker, and exits. The worker waits until the target pane is
actually ready — real tmux state, not a fixed sleep — then injects. Direct `send`
and `paste` skip the handoff entirely and deliver synchronously. Nothing runs as a
daemon. For the full data flow, see
[docs/architecture/overview.md](docs/architecture/overview.md).

## What tprompt guarantees

Prompts are plain markdown (frontmatter is metadata; only the body is delivered).
Delivery defaults to bracketed paste, sanitization defaults to `safe`, and `tprompt`
guarantees *verified tmux-targeted delivery* — not that the receiving application
interprets the text as you intended (a shell will; Vim in normal mode may not). The
authoritative contract is [EXPECTATIONS.md](EXPECTATIONS.md).

`tprompt` is intentionally narrow: no templating, no prompt composition, no cross-host
clipboard sync, no GUI. See [EXPECTATIONS.md](EXPECTATIONS.md#non-goals) and
[docs/roadmap/future-phases.md](docs/roadmap/future-phases.md) for what is deliberately
out of scope.

## Documentation

Start at [docs/README.md](docs/README.md) — the progressive-disclosure entrypoint that
routes "I want to change X" to the narrowest doc. Top-level references:

- [EXPECTATIONS.md](EXPECTATIONS.md) — user-visible behavior contract.
- [DECISIONS.md](DECISIONS.md) — locked product and engineering decisions.
- [AGENTS.md](AGENTS.md) — contributor and agent entrypoint (also linked as CLAUDE.md).

## Building from source

```bash
mise install   # pinned Go toolchain + lint/format tooling (see mise.toml)
make build     # version-stamped binary at bin/tprompt
make check     # format check + lint + race-enabled tests (the health gate)
```

For a quick compile check, `go build ./cmd/tprompt`. For the full contributor workflow,
toolchain, and contracts, start at [AGENTS.md](AGENTS.md). Testscripts execute real
`tmux`; see [docs/testing/harness.md](docs/testing/harness.md) before running broad test
targets in a shell with tmux state that matters.
