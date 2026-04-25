# CLI Commands

This file describes the current command surface.

## Commands

### `tprompt` (no subcommand)

Default dispatch: when stdin is a tty **and** `$TMUX` is set, the invocation is rewritten to `tprompt tui` before cobra parses flags, so a tmux binding can use `tprompt --target-pane '#{pane_id}' ...` instead of `tprompt tui --target-pane '#{pane_id}' ...`. Outside tmux (or without a tty), bare `tprompt` prints help.

Because rewriting happens before flag parsing, `tui`'s required `--target-pane` still fires — bare `tprompt` with no flags inside tmux+tty errors clearly with exit 2. This is intentional: see DECISIONS.md §29 and `examples/tmux-bindings.md`.

### `tprompt list`

Lists all available prompt IDs.

Current output shape:

```text
code-review
bug-hunt
deep-review
```

Additional output fields should be introduced through a scoped Linear issue.

### `tprompt show <id>`

Shows the resolved prompt.

Recommended default output:

- prompt ID
- source file path
- metadata summary (title, description, tags, declared key) if present
- body

### `tprompt send <id>`

Resolves the prompt and sends it to a tmux pane.

Flags:

- `--mode paste|type`
- `--enter`
- `--target-pane <pane-id>`
- `--sanitize strict|safe|off`

Behavior:

- if `--target-pane` is omitted, use current pane context when available
- if not inside tmux and no target pane supplied, fail clearly
- delivery settings resolve in this order:
  - CLI flags
  - prompt frontmatter defaults
  - config file
  - built-in defaults
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

Behavior:

- list prompts
- let user choose one via the external picker
- print the selected ID on stdout for shell composition

This is distinct from the built-in TUI, which is not configurable. `pick` is a scripting hook, not an end-user UX.

### `tprompt tui`

Launches the built-in interactive TUI, which submits a delivery job to the daemon for deferred injection into the target pane. Typically invoked from a tmux popup, but works in any terminal context. See `docs/commands/tui-flow.md` for the end-to-end flow and `docs/commands/tui.md` for the TUI details.

### `tprompt doctor`

Checks environment and configuration.

Checks:

- config loads and validates
- prompt directory exists
- prompt files discoverable
- duplicate IDs absent
- duplicate or reserved keybinds absent
- inside tmux or not
- clipboard reader resolved and installed when needed

### `tprompt daemon start`
### `tprompt daemon status`

Used for local daemon lifecycle.

`start` and `status` are the current daemon lifecycle commands.

## Cancel semantics

When the user cancels an interactive flow (TUI `Esc`, `pick` external cancel), the command exits with **status 0**. Cancellation is a valid outcome, not an error. Scripts should not treat it as a failure.

## Exit code guidance

- `0` success **or** user cancellation
- `2` usage/config error
- `3` prompt resolution error / clipboard validation error / sanitizer strict-mode rejection
- `4` tmux environment error
- `5` daemon/IPC error
- `6` delivery or verification error

These are the current command contract and should remain stable unless the
behavior contract is explicitly updated.
