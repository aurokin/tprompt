# tprompt Specification Bundle

`tprompt` is a tmux-first CLI for injecting prewritten markdown documents into a target tmux pane as though the user typed or pasted them.

This bundle is organized for **progressive disclosure**:

1. Start with this file.
2. Read `DECISIONS.md` to understand what is already locked.
3. Read `EXPECTATIONS.md` to understand the MVP contract.
4. Read `TASKS.md` to implement in phases.
5. Only then move into the deeper docs referenced by each task.

## Product summary

`tprompt` solves one job well:

> Pick a markdown-backed prompt by ID and deliver its body into a tmux pane reliably, especially when launched from a tmux popup.

The distinguishing feature is the **deferred popup flow**:

- user opens `tprompt` in a popup
- user picks a prompt
- popup exits
- daemon waits for the original pane to become the active target again
- daemon injects the prompt text

This avoids fragile sleep-based behavior.

## Locked MVP framing

- prompt source of truth is the filesystem
- source files are markdown
- **prompt ID is the file name stem**
- directories are organizational only, not part of the ID
- duplicate filename stems are invalid
- popup workflow is first-class
- daemon-backed deferred delivery is required
- verification should be based on tmux state, not timers
- paste mode is the default delivery mode
- type mode is supported as a fallback

## Reading map

### Minimum reading for an implementation agent

- `DECISIONS.md`
- `EXPECTATIONS.md`
- `TASKS.md`
- `docs/architecture/overview.md`
- `docs/commands/cli.md`
- `docs/commands/popup-flow.md`
- `docs/tmux/verification.md`

### Deeper implementation references

- Architecture: `docs/architecture/*`
- Command behavior: `docs/commands/*`
- Tmux mechanics: `docs/tmux/*`
- Filesystem/config: `docs/storage/*`
- Internal interfaces and failure handling: `docs/implementation/*`
- Tests: `docs/testing/test-plan.md`
- Post-MVP ideas: `docs/roadmap/future-phases.md`

## Deliverable expectation for the coding agent

The coding agent should produce a working CLI application with:

- an interactive picker
- a non-interactive send path
- a small daemon for deferred popup delivery
- robust tmux target verification
- tests around ID resolution, queueing, and delivery behavior

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

### Popup flow

1. User is in a tmux pane running some terminal application.
2. User presses a tmux binding that opens `tprompt popup`.
3. User selects `code-review`.
4. Popup closes.
5. Daemon confirms the original pane is active again.
6. Prompt body is injected into that pane.

## Out of scope for MVP

- prompt templating variables
- cloud sync
- GUI/web UI
- application-specific adapters
- semantic confirmation that the target app interpreted the prompt correctly
- remote multi-host orchestration

Read `EXPECTATIONS.md` for the exact implementation contract.
