# Locked Decisions

These decisions are already made and should be treated as constraints unless the user explicitly reopens them.

## Terminology

- **tmux popup** — a floating window created by `tmux display-popup`. Tmux's feature.
- **TUI** — tprompt's built-in interactive terminal UI (the `tprompt tui` subcommand). Runs inside any terminal context; typically a tmux popup.
- **TUI flow** — the end-to-end path from TUI selection through daemon-verified injection into the target pane.

## Product

### 1. `tprompt` is both interactive and non-interactive

It must support:

- direct send by ID
- interactive prompt selection
- tmux-popup-first workflows (TUI launched from a tmux popup)

### 2. The source of truth is markdown files on disk

Prompts live as markdown files in a configured prompts directory.

### 3. Prompt ID is the file name stem

This is locked.

Examples:

- `code-review.md` -> `code-review`
- `bug-hunt.md` -> `bug-hunt`
- `agents/deep-review.md` -> `deep-review`

Directories do **not** contribute to the ID.

### 4. Duplicate IDs are invalid

Because directories do not namespace IDs, the following is invalid:

- `agents/code-review.md`
- `reviews/code-review.md`

Both produce the same ID: `code-review`

The tool must detect this and fail clearly.

### 5. Tmux-popup workflow is first-class

The tool should be designed around reliable use when the TUI is launched from a tmux popup, not treat that launch path as a bolt-on extra.

### 6. Deferred send must be daemon-backed

The TUI process should not sleep and then try to inject directly. It should hand off a job to a daemon, then exit.

### 7. Verification must use tmux state, not timers

The daemon should only inject after confirming that the original target pane is available and back in the intended active state.

### 8. Delivery modes

Two delivery modes are required:

- `paste` (default)
- `type` (fallback)

### 9. Prompt body is what gets injected

If markdown files include YAML frontmatter, only the markdown body is injected. Frontmatter is metadata only.

### 10. MVP is tmux-first

Outside-tmux support is not required for MVP.

## Rationale for filename-stem IDs

This makes prompt invocation fast and memorable.

Benefits:

- short IDs
- easy shell usage
- easy TUI selection
- easier to remember than path-based IDs

Tradeoff:

- duplicate filenames become invalid across the whole prompt store

This tradeoff is acceptable for MVP.

## Required duplicate-ID behavior

On startup, list, send, or doctor, the tool should detect collisions and return a clear error with all conflicting paths.

Example:

```text
Duplicate prompt ID detected: code-review
- /home/user/.config/tprompt/prompts/agents/code-review.md
- /home/user/.config/tprompt/prompts/reviews/code-review.md
```

The coding agent should not try to silently disambiguate duplicate IDs in MVP.

## Phase 2 locks

These decisions were locked after the initial spec and extend the MVP contract. They are constraints, not suggestions.

### 11. Clipboard is a separate command, not a flag

A dedicated top-level command `tprompt paste` reads the host clipboard and delivers it. Clipboard content is **not** a prompt ID and does **not** appear in `tprompt send`.

Flag surface mirrors `send` for uniformity: `--target-pane`, `--mode paste|type`, `--enter`, `--sanitize strict|safe|off`.

### 12. Delivery mechanism is bracketed paste by default

Default paste mode is implemented as:

- `tmux load-buffer -b <name> -` (content fed via stdin)
- `tmux paste-buffer -d -p -b <name> -t <target>` (`-p` enables bracketed paste; `-d` deletes the buffer after)

Fallback `type` mode uses `tmux send-keys -l -- "<body>"` with chunking for large payloads.

### 13. No trailing Enter by default

Default behavior is **no** automatic Enter. Users finish and submit themselves. `--enter` is an opt-in that fires Enter as a separate `send-keys` call **outside** the bracketed-paste wrapper.

### 14. Same-host scope only

Clipboard reader, daemon, and tmux pane all run on the same host. Cross-host clipboard (laptop → remote via OSC-52 read or similar) is an explicit non-goal for MVP.

### 15. TUI is built-in and first-class

`tprompt tui` launches a built-in interactive TUI, not an external picker. It features:

- a keybind "board" of single-keypress shortcuts for pinned prompts
- a pinned clipboard row (default keybind `P`)
- `/` for fuzzy search over `id + title + description + tags`
- `Esc` to cancel (exit 0)
- `Enter` to select the highlighted row

`picker_command` config is kept but only affects the separate `tprompt pick` scripting command.

### 16. Keybinds are declared in frontmatter

Frontmatter `key:` field assigns a single printable character to a prompt. Rules:

- **Case-insensitive** — `c` and `C` are the same key.
- **Any single printable character** is allowed (not restricted to the auto-assign pool).
- **Duplicate `key:` across prompts** is a **hard error**, same strictness as duplicate IDs.
- **Collision with a reserved key** (`P`, `/`, `Esc`, `Enter`) is a **hard error**.
- **Malformed key** (multi-char, empty, symbolic like `ctrl+x`) is a **hard error**.
- Modifier-key combinations are **not supported** in MVP.

### 17. Auto-assign pool for unbound prompts

Prompts without a frontmatter `key:` receive one from this pool in order:

```
1 2 3 4 5 q e r f g t z x c
```

Prompts are scanned **alphabetically by `id`** to assign from this pool. Once the pool is exhausted, remaining prompts are **overflow** — they are not shown on the board and are reachable only via `/`-search.

### 18. Reserved keys are reconfigurable

Default reserved keys are `P` (clipboard), `/` (search), `Esc` (cancel), `Enter` (select). All are overridable via `config.toml`.

### 19. TUI row layout

Rows are rendered as three columns: `[key]  id  description`.

- Description is **soft-truncated** with ellipsis to fit terminal width — never wrapped.
- When `description` is absent, fall through: `description → title → blank`.
- No body preview in rows.

### 20. Clipboard read on keypress, no preview

When the TUI opens, the clipboard is **not** read. It is read only when the user presses the clipboard key (`P` by default).

The TUI process reads the clipboard, captures the content, and submits it as part of the daemon job payload before exiting. The daemon never reads the clipboard itself.

### 21. Clipboard edge cases fail loudly

- **Empty clipboard** → inline error in TUI; TUI stays open.
- **Non-UTF-8 / binary clipboard** → reject with clear error.
- **Oversized clipboard** → hard cap via `max_paste_bytes` config; reject when over.

### 22. Clipboard reader is auto-detected, with override

The reader is chosen at runtime from platform/env signals:

- macOS → `pbpaste`
- Linux Wayland → `wl-paste`
- Linux X11 → `xclip` or `xsel`

Users can override via `clipboard_read_command` in `config.toml`. `doctor` reports which reader was chosen and whether it is installed.

### 23. Sanitization is opt-in with three tested modes

`sanitize = "strict" | "safe" | "off"`, default `off`. The rule applies uniformly to `tprompt paste` and `tprompt send <id>`. Both `strict` and `safe` require tested implementations before release.

### 24. Search is fuzzy, scope-limited

Search uses fuzzy (fzf-style) matching over `id + title + description + tags`, ranked id-first. Body content is **not** searched.

### 25. Error feedback is in-tmux plus logs

Deferred-job failures surface in two channels:

- `tmux display-message` banner on the originating client at the moment of failure
- append-only daemon log at `~/.local/state/tprompt/daemon.log`

No success banner by default.

### 26. Pending jobs are replaced, not queued

When a new deferred job arrives for the **same target pane** as one already pending, the new job **replaces** the old one. Matches "I changed my mind" intent. Different targets are independent.

### 27. TUI singletons are not enforced

Multiple TUI instances (including multiple tmux popups) can be open simultaneously — across clients or even on the same client. This may be revisited post-MVP; for now, the simpler "any TUI may submit a job" rule applies.

### 28. Direct sends bypass the daemon queue entirely

`tprompt send <id>` and `tprompt paste` (invoked outside the TUI flow) always deliver synchronously through the tmux adapter. They do not touch the daemon queue and cannot be affected by pending TUI jobs.

### 29. Bare `tprompt` defaults to `tprompt tui` in tmux + tty

When invoked with no subcommand and no args, `tprompt` dispatches to `tprompt tui` if stdin is a tty **and** `$TMUX` is set. Otherwise it prints help. This keeps the tmux binding short (`display-popup -E tprompt`) while preserving the convention that no-args → usage in a regular shell. The TUI is the signature workflow, so this default matches user intent when the environment supports it.
