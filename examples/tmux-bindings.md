# Example tmux Bindings

These are examples for the implementation docs, not locked syntax.

## Popup binding concept

The popup command should receive the original pane/client context so the daemon knows where to deliver after popup exit.

Example shape:

```tmux
bind-key P display-popup -E "tprompt popup --target-pane '#{pane_id}' --client-tty '#{client_tty}' --session-id '#{session_id}'"
```

## Notes

- exact flags may change during implementation
- preserve enough original context to verify the correct target later
- avoid relying only on “current pane at popup close time”
