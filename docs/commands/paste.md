# `tprompt paste`

Dedicated command for delivering the host system clipboard into a tmux pane, as a sibling to `tprompt send`.

## Goal

Allow the user to paste whatever is currently on their clipboard into the target pane **as if they typed it**, preserving newlines, without relying on terminal-native paste shortcuts that may not reach the target application correctly.

## Command surface

```bash
tprompt paste [flags]
```

Flags (mirror `tprompt send` for uniformity):

- `--target-pane <pane-id>` — tmux pane to deliver into; defaults to current pane context when running inside tmux
- `--mode paste|type` — default `paste` (bracketed); `type` uses `send-keys -l` fallback
- `--enter` — opt-in; sends a trailing Enter keypress outside the bracketed wrapper
- `--sanitize strict|safe|off` — overrides config default (which defaults to `off`)

See `docs/tmux/delivery.md` for the exact tmux command construction.

## Behavior

1. Resolve config (prompts dir is irrelevant, but clipboard reader, sanitize mode, max paste bytes, and tmux target are all resolved).
2. Read the system clipboard via the configured reader (see `docs/storage/clipboard.md`).
3. Validate the content:
   - empty → fail with `clipboard is empty`
   - non-UTF-8 → fail with `clipboard content is not valid UTF-8 text`
   - exceeds `max_paste_bytes` → fail with `clipboard content exceeds max_paste_bytes (N bytes)`
4. Apply sanitization if configured.
5. Deliver via the tmux adapter using the selected mode.

## Daemon vs direct path

`tprompt paste` is **always a direct send**. It never uses the daemon queue.

| Invocation context | Path |
|---|---|
| `tprompt paste` from shell or a tmux `run-shell` binding | direct (synchronous) |
| Clipboard row selected from inside `tprompt tui` | daemon — the TUI flow captures the clipboard content and submits it as a `DeliveryRequest` with `source = clipboard` |

The TUI code path is in `docs/commands/tui-flow.md`; this file describes the standalone command.

## Exit codes

- `0` — delivery succeeded
- `2` — usage/config error (e.g., no `--target-pane` and not in tmux)
- `3` — clipboard read/validation error (empty, non-UTF-8, oversized, reader missing)
- `4` — tmux environment error
- `6` — tmux delivery or verification error

## Failure modes

- `pbpaste`/`wl-paste`/`xclip` not installed and no override set → prints install hint listing candidates
- clipboard reader exits non-zero → surface stderr in the error message
- target pane missing → same behavior as `tprompt send`
- sanitize mode rejected the content (strict mode may reject sequences that safe mode would strip) → fail with `content rejected by sanitizer (mode=strict)`

## Example tmux binding

```tmux
bind-key V run-shell "tprompt paste --target-pane '#{pane_id}'"
```

See `examples/tmux-bindings.md` for more.

## Non-goals for this command

- reading a remote host's clipboard when SSH'd in
- prompting the user to confirm large pastes (hard cap instead)
- stdin as an alternative content source — that belongs on `tprompt send -`, not here
