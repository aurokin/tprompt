# Components

This file gives a more concrete internal breakdown.

## Suggested packages/modules

### `cmd/tprompt`
CLI entrypoint.

### `internal/config`
Config loading, validation, defaults, path expansion. Includes the `reserved_keys` map, `keybind_pool`, `clipboard_read_command`, `sanitize`, and `max_paste_bytes` fields.

### `internal/store`
Prompt discovery, parsing, duplicate detection, prompt lookup. Also enforces keybind validation (duplicate/reserved/malformed `key:`).

### `internal/promptmeta`
Frontmatter parsing and body extraction helpers.

### `internal/tmux`
All tmux-facing functions and types. See `docs/tmux/delivery.md` for the command construction this module owns.

### `internal/daemon`
IPC server, job handling, verification loop, replace-same-target logic, `display-message` error surfacing.

### `internal/clipboard`
Clipboard reader: auto-detect, override, error wrapping. See `docs/storage/clipboard.md`.

### `internal/sanitize`
Content sanitizer with `off` / `safe` / `strict` modes. See `docs/implementation/sanitization.md`.

### `internal/tui`
Built-in popup TUI: keybind board, clipboard row, `/`-search, selection → job submission. Distinct from `internal/picker` below.

### `internal/picker`
External-picker wrapper used only by `tprompt pick` (honors `picker_command` config). Not used by the popup flow.

### `internal/keybind`
Keybind resolver: combines frontmatter-declared keys with auto-assignment from the pool, surfaces collisions. Exposed to `internal/tui` and to `doctor`.

### `internal/app`
Command orchestration if a shared application service layer is helpful.

## Suggested core services

### PromptIndex
An immutable or cheaply rebuildable view of available prompts.

Responsibilities:

- scan directory
- detect duplicates (ID and keybind)
- return prompt summaries including resolved keybind
- resolve prompt by ID

### TmuxContextResolver
Returns best-effort information about current tmux state.

### DeliveryEngine
Takes content + target + mode + sanitize mode and performs injection.

### VerificationEngine
Evaluates whether it is safe to inject yet.

### JobQueue / DaemonServer
Receives jobs and processes them serially or with carefully bounded concurrency. Implements replace-same-target semantics.

### KeybindResolver
Pure function over the `PromptIndex` + `reserved_keys` + `keybind_pool` config → final `(key → prompt)` map, plus a list of overflow prompt IDs (search-only).

### ClipboardReader
Single-method service returning bytes. Auto-detect or override implementations. Exposed to `cmd/tprompt paste` and `internal/tui`.

### Sanitizer
Single-method service returning cleaned bytes or a rejection. One instance per configured mode.

## Concurrency guidance

MVP can keep daemon execution simple.

Recommended default:

- single daemon process
- jobs processed one at a time
- same-target replacement: dropping the old pending job when a new one arrives
- different-target jobs may run concurrently if implementation is simple; otherwise serialize

## Logging guidance

The daemon should emit logs that help diagnose:

- target pane vanished
- popup did not return to expected pane
- tmux command failed
- duplicate ID or duplicate keybind prevented resolution
- sanitizer rejected content (class + offset, not content)
- IPC failure occurred
- pending job was replaced by a newer one
