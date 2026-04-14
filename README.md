# tprompt Specification Bundle

`tprompt` is a tmux-first CLI for injecting prewritten markdown documents into a target tmux pane as though the user typed or pasted them.

This bundle is organized for **progressive disclosure**:

1. Start with this file.
2. Read `DECISIONS.md` to understand what is already locked.
3. Read `EXPECTATIONS.md` to understand the MVP contract.
4. Read `TASKS.md` to implement in phases.
5. Only then move into the deeper docs referenced by each task.

## Terminology

- **tmux popup** — a floating window created by `tmux display-popup`. Tmux's feature; not tprompt's.
- **TUI** — tprompt's built-in interactive terminal UI. Launched by `tprompt tui` (or bare `tprompt` when in tmux + tty). Runs inside whatever terminal context invokes it — typically a tmux popup.
- **TUI flow** — the end-to-end path: TUI selects → submits `DeliveryRequest` → daemon verifies + injects.

## Product summary

`tprompt` solves one job well:

> Pick a markdown-backed prompt by ID and deliver its body into a tmux pane reliably, especially when launched from a tmux popup.

The distinguishing feature is the **deferred TUI flow**:

- user opens `tprompt` in a tmux popup (or anywhere else)
- user picks a prompt
- TUI exits
- daemon waits for the target pane to become the active target again
- daemon injects the prompt text

This avoids fragile sleep-based behavior.

## Locked MVP framing

- prompt source of truth is the filesystem
- source files are markdown
- **prompt ID is the file name stem**
- directories are organizational only, not part of the ID
- duplicate filename stems are invalid
- tmux-popup workflow is first-class, with a **built-in TUI** (not an external picker)
- the TUI features a keybind board (from frontmatter + auto-assign pool) plus a pinned clipboard row
- daemon-backed deferred delivery is required for the TUI flow
- verification should be based on tmux state, not timers
- **bracketed paste** is the default delivery mode (`load-buffer` + `paste-buffer -p`)
- `type` mode is supported as a fallback
- no trailing Enter by default; `--enter` is opt-in
- **clipboard is first-class** via `tprompt paste` and the pinned TUI clipboard row; same-host only
- sanitization is opt-in (`off` default, `safe` and `strict` available)

## Reading map

### Minimum reading for an implementation agent

- `DECISIONS.md`
- `EXPECTATIONS.md`
- `TASKS.md`
- `docs/architecture/overview.md`
- `docs/commands/cli.md`
- `docs/commands/tui-flow.md`
- `docs/commands/tui.md`
- `docs/commands/paste.md`
- `docs/tmux/verification.md`
- `docs/tmux/delivery.md`

### Deeper implementation references

- Architecture: `docs/architecture/*`
- Command behavior: `docs/commands/*` (including `paste.md` and `tui.md`)
- Tmux mechanics: `docs/tmux/*` (including `delivery.md`)
- Filesystem/config: `docs/storage/*` (including `clipboard.md`)
- Internal interfaces, failure handling, sanitization: `docs/implementation/*` (including `sanitization.md`)
- Tests: `docs/testing/test-plan.md`
- Post-MVP ideas: `docs/roadmap/future-phases.md`

## Deliverable expectation for the coding agent

The coding agent should produce a working CLI application with:

- a built-in interactive TUI (keybind board + search + clipboard row)
- a non-interactive send path (`tprompt send`)
- a clipboard delivery command (`tprompt paste`)
- a small daemon for deferred TUI-flow delivery with same-target replacement
- a clipboard reader with auto-detect + override
- an opt-in sanitizer with tested `safe` and `strict` modes
- robust tmux target verification
- tests around ID resolution, keybind validation, queueing, sanitization, and delivery behavior

## Recommended implementation language

Go is the recommended default for v1 because it fits:

- single-binary CLI distribution
- local daemon + Unix socket IPC
- reliable subprocess handling
- low startup overhead
- easy cross-platform packaging for Linux/macOS

Rust is acceptable if the implementation team strongly prefers it.

## Example user experience

### Non-interactive

```bash
tprompt send code-review
```

### Clipboard

```bash
tprompt paste
```

### TUI flow (typical: launched from a tmux popup)

1. User is in a tmux pane running some terminal application.
2. User presses a tmux binding that opens `tprompt` (default-dispatches to `tprompt tui`) inside a tmux popup.
3. Built-in TUI renders a keybind board with the pinned clipboard row on top.
4. User presses a single key (a prompt's keybind, `P` for clipboard, or `/` to search).
5. TUI closes. For the clipboard row, the TUI reads and captures the clipboard before exit.
6. Daemon confirms the target pane is active again.
7. Content is sanitized (if configured) and injected into that pane.

## Out of scope for MVP

- prompt templating variables
- cloud sync
- cross-host clipboard sync (laptop → remote)
- modifier-key combos for TUI keybinds
- live clipboard preview in the TUI
- GUI/web UI
- application-specific adapters
- semantic confirmation that the target app interpreted the prompt correctly
- remote multi-host orchestration

Read `EXPECTATIONS.md` for the exact implementation contract.
