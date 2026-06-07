# Configuration

## Goal

Keep configuration small and explicit.

## Suggested config file

Example path:

- `~/.config/tprompt/config.toml`

Example:

```toml
# prompts_dir is optional. Leave it unset to use the auto-created default
# (~/.config/tprompt/prompts, or $XDG_CONFIG_HOME/tprompt/prompts). Setting it
# explicitly opts OUT of that safety net (DECISIONS §31): an explicit path is
# used verbatim and is NOT auto-created — it must already exist.
# prompts_dir = "~/.config/tprompt/prompts"
additional_prompts_dirs = []
prompt_priority = "global"             # "global" | "project"
default_mode = "paste"
default_enter = false
log_path = "~/.local/state/tprompt/delivery.log"
picker_command = "fzf"
verification_timeout_ms = 5000
verification_poll_interval_ms = 100
post_injection_verification = false     # opt-in diagnostic warning only

# Clipboard
clipboard_read_command = ""            # empty = auto-detect (pbpaste/wl-paste/xclip/xsel)
max_paste_bytes = 2097152              # 2 MiB cap on paste size

# Sanitization
sanitize = "safe"                      # "off" | "safe" | "strict"

# tprompt new
edit_on_new = true                     # auto-open the editor after `new` (interactive only)

# TUI keybinds
keybind_pool = "12345qerfgtzxc"        # auto-assign pool order

[reserved_keys]
clipboard = "P"
search    = "/"
cancel    = "Esc"
select    = "Enter"
```

## Required config fields

- default delivery mode
- default enter behavior
- log path (for TUI handoff jobs and diagnostics)

## Optional config fields

- prompts directory (defaults to `$XDG_CONFIG_HOME/tprompt/prompts`,
  falling back to `~/.config/tprompt/prompts`; auto-created on first access)
- additional global prompt directories
- prompt priority policy (`global` by default; `project` opt-in)
- picker command (affects `tprompt pick`; does not affect the built-in TUI)
- daemon auto-start compatibility field (ignored)
- verification timeout
- poll interval
- post-injection verification warning
- clipboard reader override
- max paste bytes
- sanitize mode
- auto-open editor on `new` (`edit_on_new`, default `true`; interactive only)
- reserved keys map
- keybind pool

## Prompts directory resolution

`prompts_dir` is optional. When unset (or omitted from `config.toml`):

1. If `XDG_CONFIG_HOME` is set, the prompts directory resolves to
   `$XDG_CONFIG_HOME/tprompt/prompts`.
2. Otherwise it resolves to `~/.config/tprompt/prompts`.

The resolved default directory is auto-created on first access, so a fresh
install can run `tprompt list` (or any prompt-store command) without any
manual setup.

When `prompts_dir` is set explicitly, the path is used verbatim and is **not**
auto-created — a missing explicit directory remains a hard error
(`prompts directory missing: <path>`).

`additional_prompts_dirs` entries are appended after the primary global source.
Missing additional directories are skipped at runtime and reported as doctor
warnings. Duplicate prompt IDs within the global tier are hard errors.

## Source resolution order

Discovery walks the configured sources in this fixed order:

1. **Primary global** — `prompts_dir` if set, otherwise the auto-created
   default at `$XDG_CONFIG_HOME/tprompt/prompts` (or `~/.config/tprompt/prompts`).
2. **Additional global sources** — each entry of `additional_prompts_dirs`,
   in declared order.
3. **Project overlay** — the `tprompt/` directory discovered by walking up
   from the current working directory, when one is active.

Sources 1 and 2 form the **global tier**. Source 3 is the **project tier**.
Within a single tier, duplicate prompt IDs remain a hard error. Across tiers,
`prompt_priority` decides the winner and the loser is shadowed (see below).

## Project overlay

From the current working directory, tprompt walks up looking for the closest
project marker. A `tprompt/` directory inside a git tree activates a project
source. If `.git` is reached before any `tprompt/`, no project overlay is
active. The walk stops at the user's home directory or filesystem root so a
home-level `~/tprompt` folder is never treated as project scope.

Project sources are appended after global sources. When a prompt ID appears in
both global and project tiers, `prompt_priority` decides the winner:

- `global` (default): the global prompt wins and the project prompt is shadowed.
- `project`: the project prompt wins and the global prompt is shadowed.

Shadowed prompts remain visible in `tprompt list` and searchable in the TUI,
but they are excluded from automatic board keybind assignment.

## Resolution order

Recommended order for resolved delivery settings (`mode`, `enter`, `sanitize`, target-independent behavior):

1. CLI flags
2. prompt frontmatter defaults
3. config file
4. built-in defaults

`prompts_dir`, log path, picker configuration, reserved keys, and the keybind
pool are config-only settings, so they resolve as:

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

`sanitize` accepts `"off"`, `"safe"`, or `"strict"`. Default is `"safe"`: strips dangerous control sequences (OSC, DCS, mode toggles, bracketed-paste protocol terminators) while preserving cosmetic CSI (SGR colors, cursor movement). See `docs/implementation/sanitization.md` for the full denylist and the rationale behind the default. `"off"` is opt-in for users who legitimately need raw escape passthrough; `"strict"` rejects on any escape sequence and reports class plus byte offset. Invalid values fail config validation.

## `edit_on_new`

`edit_on_new` defaults to `true`. When `true`, `tprompt new` opens the created
file in your editor in an interactive terminal (stdin and stdout both ttys).
The editor command is resolved as `--editor` flag › `$VISUAL` › `$EDITOR`; with
none set, nothing opens (there is no built-in `vi`/nano fallback). Piped,
redirected, and CI runs never open an editor. Set `edit_on_new = false` to keep
`new` scaffold-and-stop by default; you can still open per-run with `--edit` or
`--editor <cmd>`, or force-skip with `--no-edit`.

## Legacy daemon fields

`socket_path` and `daemon_auto_start` are retained only so existing config
files keep loading. They are ignored by the current handoff path. `tprompt tui`
does not contact or auto-start a daemon; it spawns a short-lived handoff worker
per selection.

## `max_paste_bytes`

Applies to both `tprompt paste` and prompt body delivery. Content exceeding this cap is rejected before any tmux command runs.

Default: 2 MiB (2,097,152 bytes). The cap exists to bound accidental large pastes (truncated binaries, runaway logs); the primary defense against malicious payloads is sanitization (default `safe`). Raise it in config if you legitimately paste larger content. The adapter still caps per-chunk size in `type` mode (see `docs/tmux/delivery.md`).

## Post-injection verification

`post_injection_verification` defaults to `false`.

When set to `true`, the handoff worker captures the target pane tail before and after successful TUI-flow delivery. If the tail appears unchanged, the worker emits a warning diagnostic. This warning does not change delivery success or failure, and it does not prove whether the target application interpreted the input.

## Config validation

The tool fails clearly if:

- prompts directory is set explicitly but missing on disk
- default mode is invalid
- `prompt_priority` is not `global` or `project`
- `sanitize` value is not `off`/`safe`/`strict`
- `clipboard_read_command` is set but unparseable as an argv
- `reserved_keys` contains a malformed value
- a reserved key and the pool conflict in unresolvable ways (e.g., pool is empty after removing reserved keys — only a warning, not a hard error)
