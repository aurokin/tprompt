# Architecture Overview

`tprompt` is composed of eight major pieces.

## 1. CLI layer

Responsibilities:

- parse commands and flags
- load config
- talk to prompt store
- invoke tmux adapter directly for `send` and standalone `paste`
- spawn a short-lived handoff worker for deferred TUI-flow sends

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

All tmux interaction is centralized here rather than scattered through command handlers and handoff code. See `docs/tmux/delivery.md` for the concrete command construction.

## 4. Deferred handoff

Responsibilities:

- receive deferred delivery jobs from private per-user job files
- validate job shape
- verify target pane readiness
- inject only after verification passes
- surface success/failure via `display-message` + append-only log

The TUI path does not require or auto-start a long-running daemon.

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

## 7. TUI (built-in interactive UI)

Responsibilities:

- render the keybind board and pinned clipboard row
- resolve single-key selection to a prompt ID or the clipboard action
- handle `/`-search with fuzzy matching over id + title + description + tags
- read the clipboard on keypress when the user selects the clipboard row
- submit a handoff job and exit

This is distinct from `internal/picker`, which only wraps the optional external `picker_command` used by `tprompt pick`. The fuzzy scorer itself lives in a dependency-free `internal/searchindex` core that the board and the `import wispr -i` picker both adapt to, so the two renderers share ranking without sharing dependencies. See `docs/commands/tui.md`.

## 8. Import (Wispr reader)

Responsibilities:

- read snippets from a local Wispr Flow `flow.sqlite`, opened **read-only** (it never writes to Wispr, never reads dictation history)
- map each live snippet to a prompt id + markdown file (phrase → title + slug, replacement → body, starred → tag)
- write through the same collision-checked, TOCTOU-safe prompt writer that `tprompt new` uses, applying skip-existing / `--overwrite` / `--dry-run` policy

The SQLite driver (`modernc.org/sqlite`, pure-Go) is **isolated to `internal/wispr`** so the rest of the binary stays CGO-free. This is an offline, one-way ingest — not a sync — and is the only piece that reads an external tool's database. See `docs/commands/cli.md` and `DECISIONS.md` §34.

## Data flow summary

### Direct send (`tprompt send <id>`, `tprompt paste` from CLI)

1. CLI resolves the source (prompt body from store, or clipboard via reader)
2. Sanitizer processes the content
3. CLI resolves the target tmux pane
4. Adapter delivers immediately

### TUI flow (typical: launched from a tmux popup)

1. `tprompt tui` (or bare `tprompt` when in tmux + tty) launches — typically inside a tmux popup — with target context passed in
2. Built-in TUI renders the board + clipboard row
3. User selects a prompt, the clipboard row, or searches
4. If clipboard: TUI reads and validates the clipboard; on success, writes a handoff job with `source = clipboard`
5. If prompt: TUI writes a handoff job with `source = prompt` and the resolved body
6. TUI process exits
7. Handoff worker verifies the target pane has returned to selection
8. Sanitizer processes the content in the worker's context
9. Adapter delivers

### Import (`tprompt import wispr`)

1. CLI resolves the Wispr `flow.sqlite` path (`--db-path` or the OS default; no default → usage error)
2. The Wispr reader opens the DB read-only and returns the live snippets
3. CLI resolves the write target (primary global dir, or `--project` overlay)
4. Each snippet is mapped to a prompt id (slug, with deterministic uuid fallback/disambiguation) and markdown
5. The shared prompt writer applies the collision/skip-existing/`--overwrite` policy and writes (or, under `--dry-run`, previews)

No tmux adapter, sanitizer, or handoff is involved — import only touches the prompt store.

`import` is a source-dispatch parent: sources register through an `ImportSource` seam and the ingest engine is generic over an `ImportRecord`, so steps 4–5 name no source-specific type (wispr is the only built-in today). Bare `tprompt import` in tmux + tty opens the default source's interactive picker — the §34 analog of the bare-`tprompt` → `tui` default. See `DECISIONS.md` §34.

## Architectural priorities

1. reliability over cleverness
2. clear failure modes (no silent fallbacks)
3. small and understandable internals
4. tmux-specific logic isolated behind the adapter
5. content transformations (sanitization) isolated behind a single interface
