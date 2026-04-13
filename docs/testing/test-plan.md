# Test Plan

## Goal

The MVP should be testable without requiring a full live tmux session for most logic.

## Unit tests

### Prompt store

- discovers markdown files recursively
- derives ID from filename stem
- detects duplicate stems
- extracts body correctly with and without frontmatter
- ignores unsupported file extensions
- parses `key:` frontmatter field
- rejects duplicate `key:` (case-insensitive)
- rejects reserved-key collisions
- rejects malformed `key:` values (multi-char, empty, non-printable, `ctrl+x`)

### Config

- loads defaults
- merges config + flags correctly
- validates invalid mode/path values
- validates `sanitize` must be one of `off`/`safe`/`strict`
- parses `reserved_keys` map with both character and symbolic (Esc/Enter/Tab/Space) forms
- deduplicates `keybind_pool`
- removes reserved keys from `keybind_pool` automatically

### Keybind resolver

- frontmatter-declared keys take precedence over auto-assignment
- auto-assignment scans alphabetical by id, consumes pool in order
- reserved keys are excluded from the pool
- overflow list contains prompts past the pool
- collision errors include all offending paths

### Job validation

- rejects missing target pane
- rejects invalid mode
- preserves prompt body and metadata correctly
- `source = "clipboard"` jobs include captured body and `prompt_id` unset
- `sanitize_mode` round-trips correctly

### Clipboard reader

- auto-detect picks pbpaste on darwin
- auto-detect picks wl-paste when `WAYLAND_DISPLAY` is set
- auto-detect picks xclip/xsel when `DISPLAY` is set
- override command is used verbatim
- missing reader surfaces a clear error with candidate list
- command non-zero exit surfaces stderr in the error
- empty output → error
- non-UTF-8 output → error
- oversized output (vs `max_paste_bytes`) → error

### Sanitizer

Fixture corpus covers each class:

- OSC (`ESC]0;title BEL`, `ESC]52;c;<base64> BEL`)
- DCS (`ESC P ... ESC \`)
- CSI mode toggles (`ESC[?1000h`, `ESC[?1049h`, `ESC[?2004h`)
- application keypad (`ESC=`, `ESC>`)
- SGR colors (`ESC[31m`, `ESC[0m`) — cosmetic
- cursor movement (`ESC[2A`, `ESC[10;5H`) — cosmetic
- multi-byte UTF-8 adjacent to escape sequences
- content with no escape sequences

Each class tested in all three modes (`off`, `safe`, `strict`):

- `off` → identity
- `safe` → strips dangerous, preserves cosmetic, no errors
- `strict` → rejects any escape sequence with class + byte offset

### Fuzzy search

- id match ranks above title match
- title match ranks above description match
- description ranks above tags
- tighter / earlier matches rank higher within same field
- overflow prompts are included in search results
- no body content appears in search results

## Adapter tests

Use mocks/fakes for tmux command execution where possible.

Test:

- `paste` mode constructs `load-buffer -b <name> -` + `paste-buffer -d -p -b <name> -t <target>`
- `paste` mode with `--enter` appends a separate `send-keys -t <target> Enter` **after** `paste-buffer`
- `type` mode constructs `send-keys -t <target> -l -- <chunk>` with correct chunk splitting on rune boundaries
- `type` mode respects chunk size limit (4096 bytes)
- pane-exists and selection checks map command output correctly
- `display-message` is called with `-c <client-tty>` when available, without `-c` otherwise
- size cap rejection happens before any tmux command runs

## TUI rendering tests

- three-column row layout with correct widths
- description soft-truncates with ellipsis to fit terminal width
- description falls back to title when missing, then blank
- clipboard row is always first
- reserved keys render in footer hints correctly
- `/`-search switches mode and renders query input
- overflow prompts are not shown in the board but appear in search results
- `Esc` from board returns cancel result
- `Esc` from search returns to board
- `P` keypress returns `clipboard` action
- prompt keypress returns `prompt` action with correct `prompt_id`

## CLI tests

- `list` success and duplicate-ID failure
- `list` duplicate-keybind failure
- `show` missing ID
- `send` outside tmux without target pane
- `send --sanitize strict` rejects content with escape sequences
- `paste` without clipboard reader available
- `paste` with empty clipboard
- `paste` oversized
- `doctor` reports missing prompt directory
- `doctor` reports clipboard reader status (installed/missing)
- `doctor` reports duplicate keybinds
- popup/`pick` cancel exits with status 0

## Daemon tests

- job with `source = "clipboard"` and pre-captured bytes delivers correctly
- replace-same-target drops the older pending job and logs the reason
- different-target jobs proceed independently
- verification timeout surfaces via `display-message` + log
- sanitizer rejection surfaces via `display-message` + log (no raw content in log)

## Integration-ish tests

If practical, add a small set of opt-in tests using a real tmux session in CI or local development.

Potential cases:

- create pane
- submit deferred job
- switch into popup-like intermediate process
- verify prompt lands in target pane after return
- bracketed paste arrives intact (multi-line content does not submit mid-paste)
- `--enter` fires exactly one Enter keypress after paste completes
- `tprompt paste` with a known static clipboard reader (via `clipboard_read_command` pointing at a fixture)

These tests are valuable but should not block MVP if they are disproportionately brittle.

## Manual test checklist

- open tmux pane with shell prompt
- run popup flow, select a prompt
- confirm prompt lands after popup closes
- repeat with `tprompt paste` after copying known text
- repeat with paste mode and type mode
- repeat after intentionally closing target pane to confirm failure path
- confirm `tmux display-message` banner shows on failure
- confirm no success banner (silence = success)
- confirm Esc from popup exits 0 (no error banner)
