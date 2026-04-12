# Configuration

## Goal

Keep configuration small for MVP.

## Suggested config file

Example path:

- `~/.config/tprompt/config.toml`

Example:

```toml
prompts_dir = "~/.config/tprompt/prompts"
default_mode = "paste"
default_enter = false
socket_path = "~/.local/state/tprompt/daemon.sock"
picker_command = "fzf"
verification_timeout_ms = 5000
verification_poll_interval_ms = 100
```

## Required config fields for MVP

- prompts directory
- socket path
- default delivery mode
- default enter behavior

## Optional config fields for MVP

- picker command
- verification timeout
- poll interval

## Resolution order

Recommended order:

1. CLI flags
2. config file
3. built-in defaults

## Config validation

The tool should fail clearly if:

- prompts directory is missing
- default mode is invalid
- socket path is invalid/unusable
