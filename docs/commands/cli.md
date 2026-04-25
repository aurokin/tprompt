# CLI Commands

This file describes the current command surface.

## Commands

### `tprompt` (no subcommand)

Default dispatch: when stdin is a tty **and** `$TMUX` is set, the invocation is rewritten to `tprompt tui` before cobra parses flags, so a tmux binding can use `tprompt --target-pane '#{pane_id}' ...` instead of `tprompt tui --target-pane '#{pane_id}' ...`. Outside tmux (or without a tty), bare `tprompt` prints help.

Because rewriting happens before flag parsing, `tui`'s required `--target-pane` still fires — bare `tprompt` with no flags inside tmux+tty errors clearly with exit 2. This is intentional: see DECISIONS.md §29 and `examples/tmux-bindings.md`.

### `tprompt list`

Lists all available prompt IDs with their resolved TUI board key state.

Current output shape:

```text
code-review  key c (explicit)
bug-hunt  key 1 (auto)
deep-review  key none (overflow, not on board)
```

### `tprompt show <id>`

Shows the resolved prompt. Output order:

- `ID:` — prompt ID
- `Source:` — source file path
- `Title:`, `Description:`, `Tags:` — included only when the frontmatter sets them
- `Key:` — resolved board key state, formatted as `<key> (explicit)`,
  `<key> (auto)`, or `none (overflow, not on board)`
- a blank line, then the markdown body

Example:

```text
ID: code-review
Source: /home/user/.config/tprompt/prompts/code-review.md
Title: Code Review
Description: Deep review prompt focused on correctness, risk, tests
Tags: review, code
Key: c (explicit)

Review this code for correctness, risk, and missing tests.
```

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

Checks environment and configuration. Each line is prefixed `ok`, `warn`, or
`FAIL` so output is greppable.

Checks, in order:

1. **config loads and validates** — `FAIL` on any load or validation error.
2. **prompts directory exists** — `FAIL` when missing.
3. **prompts discovered** — `FAIL` on duplicate IDs, malformed
   frontmatter, or duplicate/reserved/malformed `key:` values; reports the
   discovered prompt count on success.
4. **inside tmux** — `warn` when `$TMUX` is unset.
5. **clipboard reader** — `warn` when no reader is auto-detected and no
   override is configured, or when a configured override is missing on
   `$PATH`. `tprompt send`-only workflows do not need a reader.
6. **picker command** — `warn` when `picker_command` is empty or its binary is
   not on `$PATH`. Only `tprompt pick` needs this.
7. **daemon reachable** — `warn` when the configured socket is unreachable or
   the daemon is not running. Only the TUI flow requires it; direct
   `send`/`paste` are unaffected.

Only the first three checks affect the exit code. Tmux, clipboard, picker, and
daemon failures are reported as warnings so a user who only runs `tprompt
send` is not blocked by missing optional tooling.

Example output:

```text
ok   config loaded (/home/user/.config/tprompt/config.toml)
ok   prompts directory exists (/home/user/.config/tprompt/prompts)
ok   4 prompts discovered
ok   inside tmux
ok   clipboard reader: pbpaste (auto-detected, darwin)
warn picker command: fzf not found on $PATH (tprompt pick unavailable)
warn daemon not running (/home/user/.local/state/tprompt/daemon.sock): connection refused
```

### `tprompt daemon start`
### `tprompt daemon status`
### `tprompt daemon stop`

Used for local daemon lifecycle.

`start`, `status`, and `stop` are the current daemon lifecycle commands. `stop`
requests graceful shutdown over local daemon IPC and reports `daemon not
running` when the configured socket is unreachable.

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
