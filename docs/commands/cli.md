# CLI Commands

This file describes the command surface for MVP.

## Commands

### `tprompt list`

Lists all available prompt IDs.

Recommended output shape:

```text
code-review
bug-hunt
deep-review
```

Optional later enhancement:

- include path, title, or resolved keybind in verbose mode

### `tprompt show <id>`

Shows the resolved prompt.

Recommended default output:

- prompt ID
- source file path
- metadata summary (title, description, tags, resolved keybind) if present
- body

### `tprompt send <id>`

Resolves the prompt and sends it to a tmux pane.

Flags for MVP:

- `--mode paste|type`
- `--enter`
- `--target-pane <pane-id>`
- `--sanitize strict|safe|off`

Behavior:

- if `--target-pane` is omitted, use current pane context when available
- if not inside tmux and no target pane supplied, fail clearly
- always a direct send; never touches the daemon queue

### `tprompt paste`

Delivers the host system clipboard into a tmux pane.

Flags (mirror `send`):

- `--mode paste|type`
- `--enter`
- `--target-pane <pane-id>`
- `--sanitize strict|safe|off`

See `docs/commands/paste.md` for full behavior, exit codes, and failure modes.

### `tprompt pick`

Interactive prompt selection in the current process using the configured external picker (`picker_command`, default `fzf`).

Recommended behavior:

- list prompts
- let user choose one via the external picker
- print the selected ID on stdout (caller pipes into `tprompt send -` or similar)

This is distinct from the popup TUI, which is built-in and not configurable. `pick` is a scripting hook, not an end-user UX.

### `tprompt popup`

Interactive popup-oriented flow. See `docs/commands/popup-flow.md` for the end-to-end flow and `docs/commands/popup-ui.md` for the TUI details.

### `tprompt doctor`

Checks environment and configuration.

Suggested checks:

- prompt directory exists
- prompt files discoverable
- duplicate IDs absent
- duplicate or reserved keybinds absent
- inside tmux or not
- daemon socket reachable or daemon status known
- clipboard reader resolved and installed
- picker command availability if an external picker is configured

### `tprompt daemon start`
### `tprompt daemon stop`
### `tprompt daemon status`

Used for local daemon lifecycle.

For MVP, `start` and `status` are the most important. `stop` is optional if lifecycle management is intentionally minimal.

## Cancel semantics

When the user cancels an interactive flow (popup `Esc`, `pick` external cancel), the command exits with **status 0**. Cancellation is a valid outcome, not an error. Scripts should not treat it as a failure.

## Exit code guidance

- `0` success **or** user cancellation
- `2` usage/config error
- `3` prompt resolution error / clipboard validation error
- `4` tmux environment error
- `5` daemon/IPC error
- `6` delivery or verification error

These do not need to be externally guaranteed forever, but should be consistent within MVP.
