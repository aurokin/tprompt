# Expectations

This file defines the MVP contract for the coding agent.

## Success criteria

A correct MVP implementation should satisfy all of the following.

### Prompt resolution

- reads markdown files from a configured prompts directory
- derives ID from filename stem only
- rejects duplicate filename-stem IDs with a clear error
- can list prompts and show which file each ID maps to
- parses optional frontmatter including a `key:` field for TUI keybinds
- rejects duplicate, reserved, or malformed `key:` values with a clear error

### CLI behavior

- supports non-interactive send by ID (`tprompt send <id>`)
- supports clipboard delivery as a separate top-level command (`tprompt paste`)
- supports interactive selection (`tprompt pick`) with optional external picker
- supports the built-in TUI command (`tprompt tui`), typically launched from a tmux popup
- bare `tprompt` (no subcommand) dispatches to `tprompt tui` when stdin is a tty and `$TMUX` is set; otherwise prints help
- returns non-zero exit codes on operational failure
- returns **zero** when the user cancels an interactive picker or TUI
- emits human-readable errors

### TUI delivery behavior

- the TUI is built-in and interactive (not an external picker)
- TUI presents a keybind "board" for pinned prompts and a pinned clipboard row
- TUI supports `/`-search over id, title, description, tags
- the TUI flow hands work to a daemon
- the TUI process exits before delivery occurs
- when the clipboard is selected, the TUI reads the clipboard at keypress and submits content as part of the job
- daemon verifies target pane readiness using tmux state
- daemon injects only after verification passes
- daemon fails cleanly if the target pane vanished or became invalid
- when a new deferred job targets the same pane as a pending one, the pending job is replaced

### Delivery behavior

- default mode is bracketed paste (`load-buffer` + `paste-buffer -p`)
- fallback `type` mode uses `send-keys -l` with chunking for large payloads
- can optionally send Enter **outside** the bracketed-paste wrapper (`--enter`)
- default is **no** trailing Enter
- injects the prompt body, not frontmatter
- supports a configurable max paste size with clear rejection when exceeded

### Clipboard reader

- auto-detects a reader by platform and env (pbpaste / wl-paste / xclip / xsel)
- respects `clipboard_read_command` config override
- scope is always the host running tmux; cross-host clipboard is not supported
- rejects empty, non-UTF-8, or oversized clipboard content with clear errors

### Sanitization

- implements three modes: `strict`, `safe`, `off`
- default is `off`
- same rule applies uniformly to `tprompt paste` and `tprompt send <id>`

### Error feedback

- deferred-job failures are surfaced via `tmux display-message` **and** appended to the daemon log
- log path is stable and documented (`~/.local/state/tprompt/daemon.log` by default)

### Reliability

- does not depend on fixed sleeps for TUI-flow correctness
- can detect tmux pane disappearance
- can detect duplicate prompt IDs and duplicate keybinds
- can surface daemon connectivity problems clearly
- direct sends never block on daemon state

### Testing

- unit tests for prompt discovery, duplicate detection, and keybind validation
- unit tests for frontmatter/body parsing
- unit tests for job validation
- unit tests for sanitizer modes against fixture escape sequences
- unit tests for fuzzy search ranking
- integration-ish tests for tmux command construction (bracketed paste and `send-keys -l`)
- rendering tests for TUI row layout and overflow
- test coverage for error conditions

## Non-goals for MVP

Do not expand scope into these features during MVP:

- prompt templating variables
- snippets/composition
- cross-host clipboard sync (laptop ↔ remote)
- per-application readiness adapters
- remote targets
- distributed daemon
- editing UI
- history browser
- analytics dashboard
- multi-user support
- modifier-key combos (`ctrl+x`, `alt+p`, etc.) for prompt keybinds
- live clipboard preview inside the TUI

## Behavioral contract

`tprompt` guarantees **verified tmux-targeted delivery**, not semantic confirmation that the target application interpreted the input as intended.

Examples:

- If the target pane is a shell prompt, delivery is likely to behave as expected.
- If the target pane is Vim in normal mode, the injection may technically succeed but not semantically do what the user wanted.

That is acceptable for MVP.

## Preferred operator experience

### Direct prompt send

```bash
tprompt send code-review
```

### Clipboard send

```bash
tprompt paste
```

### TUI use (typical: launched from a tmux popup)

- user opens a tmux popup running `tprompt` via a tmux key binding
- built-in TUI renders the keybind board plus the clipboard row
- user presses a single key (prompt keybind, `P` for clipboard, or `/` to search)
- TUI closes (tmux tears down the popup)
- daemon verifies the target pane and injects

## Packaging expectation

Prefer a single binary plus a per-user local daemon.

## Platform expectation

Primary support target for MVP:

- Linux
- macOS

Windows support is not required unless tmux workflow is explicitly re-scoped.
