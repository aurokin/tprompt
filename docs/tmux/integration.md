# Tmux Integration

Conceptual responsibilities of the tmux adapter. Concrete command construction (flag choices, chunking, Enter placement) lives in `docs/tmux/delivery.md`.

## Tmux assumptions for MVP

- target workflow is inside tmux
- the TUI flow typically runs inside a tmux popup (but the TUI itself is not popup-specific)
- pane IDs are stable enough to identify the intended target during a short verification window

## Required tmux operations

The tmux adapter centralizes operations like:

- determine current pane ID
- determine current session/window when helpful
- inspect whether a pane still exists
- inspect whether a pane is selected
- capture pane output
- load buffer for paste
- paste buffer into pane
- send literal keys to pane
- post `display-message` errors to the originating client

## Why centralization matters

Tmux command invocation is a major source of fragility. One adapter reduces duplicated shell command logic and makes tests easier.

## Delivery modes

See `docs/tmux/delivery.md` for the full mechanics. Short version:

- **`paste` (default)** — bracketed paste via `load-buffer` + `paste-buffer -p`. Enter (if requested) fires as a separate `send-keys` call **outside** the wrapper.
- **`type` (fallback)** — literal keystrokes via `send-keys -l`, with chunking for large payloads.

Neither mode guarantees semantic success in the target application. See `docs/tmux/verification.md` for the boundary between "delivery happened" and "the app interpreted it correctly."

## Error surfacing

The adapter is responsible for running `tmux display-message` when the daemon asks it to surface a failure. Message text is provided by the caller; the adapter handles the `-c <client-tty>` scoping when available.
