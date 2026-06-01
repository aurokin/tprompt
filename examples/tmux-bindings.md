# Example tmux Bindings

`tprompt init` prints these bindings generated from the binary itself, so they
always match the current flags — run it for copy-paste-ready setup:

```bash
tprompt init            # popup binding + install steps
tprompt init --more     # also the direct paste/send bindings below
tprompt init --snippet  # just the binding line, e.g. to append to a config file
```

This page documents the same bindings for reference.

## Tmux-popup binding concept

When launched inside a tmux popup, the TUI should receive the original pane/client context so the handoff worker knows where to deliver after the TUI exits.

Example shape:

```tmux
bind-key P display-popup -E "tprompt tui --target-pane '#{pane_id}' --client-tty '#{client_tty}' --session-id '#{session_id}'"
```

Bare `tprompt` works the same way when invoked inside tmux (it default-dispatches to `tprompt tui`):

```tmux
bind-key P display-popup -E "tprompt --target-pane '#{pane_id}' --client-tty '#{client_tty}' --session-id '#{session_id}'"
```

## Direct clipboard paste binding

For users who want a one-key "paste clipboard into current pane" shortcut that skips the TUI entirely:

```tmux
bind-key V run-shell "tprompt paste --target-pane '#{pane_id}'"
```

This invokes `tprompt paste` synchronously - no TUI, no handoff. The clipboard is read immediately and delivered to the current pane. If you want `--enter` to auto-submit, append the flag:

```tmux
bind-key V run-shell "tprompt paste --target-pane '#{pane_id}' --enter"
```

## Direct prompt send binding

For users who want a specific prompt bound to a key outside the TUI:

```tmux
bind-key R run-shell "tprompt send --target-pane '#{pane_id}' -- 'code-review'"
```

## Notes

- preserve enough original context to verify the correct target later
- avoid relying only on "current pane at TUI close time"
- `display-popup -E` is required so the tmux popup tears down when the wrapped command exits
- `run-shell` bindings do **not** need handoff - they deliver synchronously
