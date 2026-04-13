# Architecture Overview

`tprompt` is composed of seven major pieces.

## 1. CLI layer

Responsibilities:

- parse commands and flags
- load config
- talk to prompt store
- invoke tmux adapter directly for `send` and standalone `paste`
- talk to daemon for deferred popup sends

## 2. Prompt store

Responsibilities:

- walk configured prompt directory
- find markdown files
- derive IDs from filename stems
- parse optional frontmatter (including `key:`)
- expose prompt metadata and body
- detect duplicate IDs and duplicate/reserved/malformed keybinds

## 3. Tmux adapter

Responsibilities:

- detect whether current execution is inside tmux
- obtain current pane/session/client context when available
- inspect pane existence and selection state
- capture pane output
- perform `paste` (bracketed via `load-buffer` + `paste-buffer -p`) or `type` (via `send-keys -l`) delivery
- surface errors via `tmux display-message`

All tmux interaction is centralized here rather than scattered through the CLI and daemon. See `docs/tmux/delivery.md` for the concrete command construction.

## 4. Daemon

Responsibilities:

- receive deferred delivery jobs over local IPC (Unix socket)
- validate job shape
- verify target pane readiness
- inject only after verification passes
- replace any pending job targeting the same pane when a new job arrives
- surface success/failure via `display-message` + append-only log

## 5. Clipboard reader

Responsibilities:

- auto-detect the host clipboard utility (pbpaste / wl-paste / xclip / xsel)
- honor `clipboard_read_command` override
- expose a single `Read()` method returning raw bytes
- surface missing-reader and reader-failure errors clearly

See `docs/storage/clipboard.md`.

## 6. Sanitizer

Responsibilities:

- apply the configured sanitize mode (`off` | `safe` | `strict`) to content before it reaches the tmux adapter
- report strict-mode rejections with class + byte offset

See `docs/implementation/sanitization.md`.

## 7. TUI (built-in popup UI)

Responsibilities:

- render the keybind board and pinned clipboard row
- resolve single-key selection to a prompt ID or the clipboard action
- handle `/`-search with fuzzy matching over id + title + description + tags
- read the clipboard on keypress when the user selects the clipboard row
- submit a `DeliveryRequest` to the daemon and exit

This is distinct from `internal/picker`, which only wraps the optional external `picker_command` used by `tprompt pick`. See `docs/commands/popup-ui.md`.

## Data flow summary

### Direct send (`tprompt send <id>`, `tprompt paste` from CLI)

1. CLI resolves the source (prompt body from store, or clipboard via reader)
2. Sanitizer processes the content
3. CLI resolves the target tmux pane
4. Adapter delivers immediately

### Popup send

1. `tprompt popup` launches inside a tmux popup with target context passed in
2. Built-in TUI renders the board + clipboard row
3. User selects a prompt, the clipboard row, or searches
4. If clipboard: TUI reads and validates the clipboard; on success, submits a job with `source = clipboard`
5. If prompt: TUI submits a job with `source = prompt` and the resolved body
6. Popup process exits
7. Daemon verifies the target pane has returned to selection
8. Sanitizer processes the content in the daemon's context
9. Adapter delivers

## Architectural priorities

1. reliability over cleverness
2. clear failure modes (no silent fallbacks)
3. small and understandable internals
4. tmux-specific logic isolated behind the adapter
5. content transformations (sanitization) isolated behind a single interface
