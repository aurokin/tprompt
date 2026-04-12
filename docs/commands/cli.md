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

- include path or title in verbose mode

### `tprompt show <id>`

Shows the resolved prompt.

Recommended default output:

- prompt ID
- source file path
- metadata summary if present
- body

### `tprompt send <id>`

Resolves the prompt and sends it to a tmux pane.

Flags for MVP:

- `--mode paste|type`
- `--enter`
- `--target-pane <pane-id>`

Behavior:

- if `--target-pane` is omitted, use current pane context when available
- if not inside tmux and no target pane supplied, fail clearly

### `tprompt pick`

Interactive prompt selection in the current process.

Recommended behavior:

- list prompts
- let user choose one
- either print the selected ID or immediately send it, depending on implementation choice

For MVP, keep semantics simple and document them clearly.

### `tprompt popup`

Interactive popup-oriented flow. See `popup-flow.md` for exact behavior.

### `tprompt doctor`

Checks environment and configuration.

Suggested checks:

- prompt directory exists
- prompt files discoverable
- duplicate IDs absent
- inside tmux or not
- daemon socket reachable or daemon status known
- picker availability if an external picker is configured

### `tprompt daemon start`
### `tprompt daemon stop`
### `tprompt daemon status`

Used for local daemon lifecycle.

For MVP, `start` and `status` are the most important. `stop` is optional if lifecycle management is intentionally minimal.

## Exit code guidance

- `0` success
- non-zero for all operational failures

Recommended distinctions if convenient:

- `2` usage/config error
- `3` prompt resolution error
- `4` tmux environment error
- `5` daemon/IPC error
- `6` delivery verification error

These do not need to be externally guaranteed forever, but should be consistent within MVP.
