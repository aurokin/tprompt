# Example tmux Bindings

These are examples for the implementation docs, not locked syntax.

## Popup binding concept

The popup command should receive the original pane/client context so the daemon knows where to deliver after popup exit.

Example shape:

```tmux
bind-key P display-popup -E "tprompt popup --target-pane '#{pane_id}' --client-tty '#{client_tty}' --session-id '#{session_id}'"
```

## Direct clipboard paste binding

For users who want a one-key "paste clipboard into current pane" shortcut that skips the TUI entirely:

```tmux
bind-key V run-shell "tprompt paste --target-pane '#{pane_id}'"
```

This invokes `tprompt paste` synchronously — no popup, no daemon. The clipboard is read immediately and delivered to the current pane. If you want `--enter` to auto-submit, append the flag:

```tmux
bind-key V run-shell "tprompt paste --target-pane '#{pane_id}' --enter"
```

## Direct prompt send binding

For users who want a specific prompt bound to a key outside the popup:

```tmux
bind-key R run-shell "tprompt send code-review --target-pane '#{pane_id}'"
```

## Notes

- exact flags may change during implementation
- preserve enough original context to verify the correct target later
- avoid relying only on "current pane at popup close time"
- `display-popup -E` is required so the popup exits when the wrapped command exits
- `run-shell` bindings do **not** need the daemon — they deliver synchronously
