# Delivery Mechanics

How `tprompt` actually injects content into a tmux pane. This file captures the locked choice of tmux primitives, Enter semantics, and chunking rules. `docs/tmux/integration.md` stays at the conceptual level; this file is the implementation reference.

## Summary

Two modes, both routed through the tmux adapter.

| Mode | Primary tmux commands | Default? |
|---|---|---|
| `paste` (bracketed) | `load-buffer` + `paste-buffer -d -p` | yes |
| `type` (fallback) | `send-keys -l -- <chunk>` | no |

## Mode: `paste` (bracketed, default)

Sequence:

1. `tmux load-buffer -b <buffer-name> -` — feed the prompt/clipboard body via stdin. A per-job unique buffer name (e.g., `tprompt-<job-id>`) avoids collisions between concurrent jobs.
2. `tmux paste-buffer -d -p -b <buffer-name> -t <target-pane>` — paste into the target. Flags:
   - `-p` — bracketed paste. Wraps content in `ESC[200~ … ESC[201~` so bracketed-aware apps (Claude Code, modern shells, vim insert, editors) treat the input as a paste, not typed keystrokes. Newlines inside a bracketed paste do **not** trigger Enter.
   - `-d` — delete the buffer after paste. Cleans up state.
3. If `--enter` was set, fire `tmux send-keys -t <target-pane> Enter` **outside** the bracketed wrapper. This lands as a real submit keystroke at the correct moment for shells, agent CLIs, and readline-style inputs.

Why bracketed paste is the default:

- Preserves newlines as literal characters rather than turning each into a submit.
- Matches the user's stated goal: "paste whatever I saved or copied, as if I typed it, including newlines in terminal UIs."
- Modern agent CLIs (Claude Code, aider, etc.) detect bracketed paste and buffer content until the user manually submits.

Gotchas:

- Apps that do not support bracketed paste may show the literal `[200~` / `[201~` escape codes. For those targets, use `--mode type`.
- vim in **normal mode** will treat each pasted character as a command. That is a user-responsibility issue, not something the adapter prevents. See `docs/tmux/verification.md`.

## Mode: `type` (fallback)

Sequence:

1. Split the payload into chunks (see below).
2. For each chunk: `tmux send-keys -t <target-pane> -l -- <chunk>`.
   - `-l` disables tmux key-name lookup so bytes are sent literally. A `\n` byte becomes a line-feed keypress, which most apps treat as Enter — this is the correct behavior for "type this as if I typed it line by line."
3. If `--enter` was set, append a final `tmux send-keys -t <target-pane> Enter`.

### Chunking

`send-keys -l` content rides on the tmux command line and is subject to argv length limits (typically around `ARG_MAX` minus overhead). Chunking rule:

- split on UTF-8 **rune** boundaries, not byte boundaries, to avoid splitting a multi-byte character
- default chunk size: 4096 bytes (conservative; safe across platforms)
- chunk size is a compile-time constant for MVP; config exposure is deferred

## Target resolution

`TargetContext` (see `docs/architecture/data-model.md`) supplies `pane_id` and optionally `session_id`, `window_id`, `client_tty`. The adapter uses `pane_id` as the authoritative `-t` target. Other fields are used for verification (see `docs/tmux/verification.md`).

## Size cap

`max_paste_bytes` (config) applies to both modes.

- Exceeded for `paste` mode → delivery is rejected before `load-buffer` is called.
- Exceeded for `type` mode → same — the cap is a policy decision, not an implementation detail of a specific tmux primitive.

Default cap: 1,048,576 bytes (1 MiB). Users can raise it in config but the adapter will still refuse to run `send-keys` with content larger than one chunk per call.

## Sanitization interaction

Sanitization (see `docs/implementation/sanitization.md`) runs **before** the payload reaches the adapter. The adapter receives already-sanitized bytes.

## Enter placement — why outside the wrapper?

For bracketed paste, placing Enter inside the buffer would make the Enter byte part of the pasted content. Most bracketed-paste-aware apps do **not** submit on newlines inside a paste. Issuing `send-keys Enter` as a separate call, after `paste-buffer` completes, produces the unambiguous "submit now" behavior users expect when they explicitly request `--enter`.

For `type` mode, there is no wrapper — Enter can be part of the payload or a separate `send-keys` call. Keeping it as a separate call makes behavior uniform across modes and easier to test.

## Failure classes

- `load-buffer` or `paste-buffer` returns non-zero → delivery error (exit 6)
- `send-keys -l` returns non-zero on any chunk → delivery error; no automatic retry
- Target pane vanished between verification and execution → delivery error; surface via `tmux display-message`

## Testing

See `docs/testing/test-plan.md` for the command-construction test matrix.
