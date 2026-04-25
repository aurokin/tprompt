# Configuration

## Goal

Keep configuration small and explicit.

## Suggested config file

Example path:

- `~/.config/tprompt/config.toml`

Example:

```toml
prompts_dir = "~/.config/tprompt/prompts"
default_mode = "paste"
default_enter = false
socket_path = "~/.local/state/tprompt/daemon.sock"
log_path = "~/.local/state/tprompt/daemon.log"
daemon_auto_start = false
picker_command = "fzf"
verification_timeout_ms = 5000
verification_poll_interval_ms = 100
post_injection_verification = false     # opt-in diagnostic warning only

# Clipboard
clipboard_read_command = ""            # empty = auto-detect (pbpaste/wl-paste/xclip/xsel)
max_paste_bytes = 1048576              # 1 MiB cap on paste size

# Sanitization
sanitize = "off"                       # "off" | "safe" | "strict"

# TUI keybinds
keybind_pool = "12345qerfgtzxc"        # auto-assign pool order

[reserved_keys]
clipboard = "P"
search    = "/"
cancel    = "Esc"
select    = "Enter"
```

## Required config fields

- prompts directory
- socket path
- default delivery mode
- default enter behavior

## Optional config fields

- picker command (affects `tprompt pick`; does not affect the built-in TUI)
- daemon auto-start for TUI flows
- verification timeout
- poll interval
- post-injection verification warning
- clipboard reader override
- max paste bytes
- sanitize mode
- reserved keys map
- keybind pool

## Resolution order

Recommended order for resolved delivery settings (`mode`, `enter`, `sanitize`, target-independent behavior):

1. CLI flags
2. prompt frontmatter defaults
3. config file
4. built-in defaults

`prompts_dir`, socket/log paths, picker configuration, reserved keys, and the
keybind pool are config-only settings, so they resolve as:

1. CLI flags where supported
2. config file
3. built-in defaults

## Keybind pool

The `keybind_pool` string is read character-by-character in order. Default: `12345qerfgtzxc`. Each character becomes one slot for auto-assignment. Duplicates within the string are treated as one slot (deduplicated on load).

Any character listed in `[reserved_keys]` is automatically removed from the pool, so users can redefine reserved keys without manually trimming the pool.

## Reserved keys

Each reserved key accepts:

- a single printable character (e.g., `"P"`)
- a symbolic form for non-printables: `"Esc"`, `"Enter"`, `"Tab"`, `"Space"`

Symbolic forms are case-insensitive on input. Invalid values fail config validation with a clear error.

To disable a reserved role entirely (e.g., to free `P` for a prompt), set the value to an empty string:

```toml
[reserved_keys]
clipboard = ""     # disable clipboard keybind; still accessible via search
```

## Sanitize

`sanitize` accepts `"off"`, `"safe"`, or `"strict"`. Invalid values fail config validation. See `docs/implementation/sanitization.md`.

## Daemon Auto-Start

`daemon_auto_start` defaults to `false`. When set to `true`, `tprompt tui`
may start the daemon if the configured socket is unreachable, wait briefly for
readiness, then retry the daemon preflight. This is limited to TUI-oriented
flows; explicit lifecycle commands such as `tprompt daemon status` do not
start the daemon implicitly.

## `max_paste_bytes`

Applies to both `tprompt paste` and prompt body delivery. Content exceeding this cap is rejected before any tmux command runs.

Sensible default: 1 MiB (1048576 bytes). Users can raise it but the adapter still caps per-chunk size in `type` mode (see `docs/tmux/delivery.md`).

## Post-injection verification

`post_injection_verification` defaults to `false`.

When set to `true`, the daemon captures the target pane tail before and after successful TUI-flow delivery. If the tail appears unchanged, the daemon emits a warning diagnostic. This warning does not change delivery success or failure, and it does not prove whether the target application interpreted the input.

## Config validation

The tool fails clearly if:

- prompts directory is missing
- default mode is invalid
- socket path is invalid/unusable
- `sanitize` value is not `off`/`safe`/`strict`
- `clipboard_read_command` is set but unparseable as an argv
- `reserved_keys` contains a malformed value
- a reserved key and the pool conflict in unresolvable ways (e.g., pool is empty after removing reserved keys — only a warning, not a hard error)
