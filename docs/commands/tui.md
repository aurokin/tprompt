# TUI

The TUI is a **built-in** interactive terminal UI. It is not a thin wrapper around an external picker. `picker_command` in config is a separate mechanism used only by `tprompt pick` (see `cli.md`).

This file describes what the TUI renders and how keybinds behave. For the end-to-end delivery flow (handoff worker, verification, injection) see `docs/commands/tui-flow.md`.

## Layout

The TUI renders a compact board in whatever terminal context it's launched in (typically a tmux popup). Each prompt shown on the board is a single row:

```
[key]  id                description
```

- **key** — single printable character; always present on the board
- **id** — filename stem
- **description** — soft-truncated with ellipsis to fit the current terminal width; fallback order `description → title → blank`

Example:

```
[P]  clipboard            (read on select)
[c]  code-merge           Merge review prompt
[q]  quick-hack           Short quick prompt
[1]  code-review          Review for correctness, risk, and missing tests
[2]  commit               Generate a conventional commit message
[3]  deploy-checklist     Preflight checks before prod push
```

When the clipboard action is bound to a printable reserved key, the clipboard row is **first** and pinned. It has no real prompt id, but it is labeled `clipboard` in the id column and renders a short hint such as `(read on select)` in the description column. If the clipboard reserved key is disabled or symbolic, the board omits the pinned clipboard row, but clipboard remains reachable from the empty-query search catalog.

## Reserved keys

| Key | Behavior | Default | Reconfigurable |
|---|---|---|---|
| `P` | Read clipboard and deliver | yes | yes |
| `/` | Enter search mode | yes | yes |
| `Esc` | Cancel and exit 0 | yes | yes |
| `Enter` | Deliver the currently highlighted row | yes | yes |

Reserved keys are overridable via `[reserved_keys]` in `config.toml`.

## Keybind assignment

Keys are assigned to prompts in two stages:

1. **Frontmatter-declared.** A prompt with `key: c` in YAML frontmatter gets exactly that character.
2. **Auto-assigned from the pool** `1 2 3 4 5 q e r f g t z x c` (in that order) for prompts that did not declare `key:`. Assignment scan order is **alphabetical by prompt `id`**.

Matching is **case-insensitive** — `c` and `C` are the same key.

**Board display order.** Explicitly keybound prompts (stage 1) render at the top of the board, above auto-assigned prompts (stage 2). Within each group, rows stay alphabetical by prompt `id`. This keeps user-chosen shortcuts pinned to the top regardless of where their ids fall alphabetically. (Assignment *scan* order remains alphabetical by `id` across all unbound prompts — see DECISIONS §17.)

Overflow: once the auto-assign pool is exhausted and frontmatter keys are satisfied, remaining prompts are **not shown on the board**. They are reachable only via `/`-search.

Shadowed prompts (the loser of a global/project ID collision under
`prompt_priority`) behave like overflow for board purposes: they receive no
board key and cannot double-bind an existing key. They remain searchable via
`/`, and selecting a shadowed search result delivers that scoped prompt.

### Collisions (hard errors at load time)

- two prompts declaring the same `key:`
- a prompt declaring a reserved key (e.g., `key: P` when `P` is reserved for clipboard)
- a malformed `key:` value (empty string, multi-character, non-printable, modifier combo)

These surface in `tprompt doctor` and cause `tprompt list|show|send|tui` to fail.

## Search mode

Triggered by `/`. All prompts (including overflow and shadowed prompts) are searchable — search is the complete-catalog view.

- **Matching:** fuzzy (fzf-style). Typing `cmv` matches `code-merge-verification`.
- **Scope:** `id + title + description + tags`. Body content is **not** indexed.
- **Ranking:** `id` match beats `title` match beats `description` match beats `tags` match. Within the same field, tighter/earlier matches rank higher.
- **Empty query:** shows the full catalog alphabetically, with the clipboard row first when clipboard is available.
- **Non-empty query:** searches prompts only; the clipboard row is omitted because it has no searchable content.
- **Exit search:** `Esc` to leave search and return to the board.
- **Select in search:** `Enter` delivers the highlighted match.

## Clipboard row behavior

- Clipboard is **not** read when the TUI opens. No preview text, no size count.
- When the user presses `P` (or whatever the reserved clipboard key is):
  1. TUI invokes the clipboard reader.
  2. TUI validates content (empty / non-UTF-8 / size cap).
  3. On validation failure, the TUI shows an **inline error** in the footer and stays open so the user can choose something else.
  4. On success, the TUI submits a handoff job with `source = clipboard` and exits.

## Template input behavior

When a selected prompt declares frontmatter `variables`, the TUI does not submit
immediately. It enters template input mode and shows all declared variables at
once as a single form, in the order declared by the prompt.

- The form shows the prompt id, then one row per variable: its label (or name),
  a `*` marker when required, and its current value. Defaults are prefilled and
  the first field is focused; the focused field's description (if any) is shown
  below the form.
- `Tab`/`↓` and `Shift+Tab`/`↑` move the focused field (clamped at the ends).
  Moving focus parks the caret at the end of the newly focused field.
- The focused field has an inline caret. `←`/`→` (or `Ctrl+B`/`Ctrl+F`) move it;
  `Home`/`Ctrl+A` and `End`/`Ctrl+E` jump to the ends. Printable text and space
  insert at the caret. `Backspace` deletes the rune before the caret;
  `Delete`/`Ctrl+D` deletes the rune at the caret. `Ctrl+W` deletes the word
  before the caret, `Ctrl+K` kills from the caret to the end of the line, and
  `Ctrl+U` clears the whole field (handy for replacing a prefilled default). The
  field scrolls horizontally to keep the caret visible, marking hidden text with
  a leading/trailing `…`, so recently typed text stays on screen in a narrow
  popup.
- `Enter` validates the whole form and submits through the handoff path.
- An empty required value shows an inline footer error and focuses the offending
  field without submitting.
- `Esc` leaves template input and returns to the board without submitting.
- `Ctrl+C` cancels the TUI and exits 0.
- Rendered prompt size is checked against `max_paste_bytes` before submission; an
  oversized rendered body stays in the form with an inline error (reporting how
  many bytes over the limit).

The handoff worker receives only the rendered body. It never prompts for
variables and never re-renders a prompt.

## Footer / status line

The TUI renders a single-line footer showing context-sensitive hints:

- board view: `press a row's [key] to select  [/ search]  [Enter select]  [Esc cancel]`, or with `[/ search (N more)]` when overflow exists. The leading `press a row's [key] to select` legend is width-aware: it is dropped (functional hints kept) when the full line would exceed the terminal width, so the footer stays one line.
- search view: `/query    [Esc exit search]  [Enter select]  [N matches]`
- template input view: `[Enter submit]  [Ctrl+U clear]  [Esc back]  [Ctrl+C cancel]`,
  adding `[Tab/↑↓ move]` when the prompt declares more than one variable
- error view: `clipboard is empty — choose another option  [Esc cancel]`

## Selection

There are two ways to deliver a row from the board:

- **Single-key shortcut.** Press the printable rune in `[brackets]` next to the row (e.g. `c` for the `code-review` row). Selects that row regardless of cursor position.
- **Cursor + Select.** Move the cursor with `↑`/`↓`, then press the **Select** key (default `Enter`). Selects the row currently under the cursor. The Select binding is reconfigurable via `[reserved_keys]` in `config.toml`.

Both paths route through the same `selectPrompt` / `selectClipboard` flow: the pinned clipboard row triggers a clipboard read; a non-template prompt row resolves the body and submits through the prompt handoff path; a templated prompt row collects variables before submission.

## Scrolling

If the board (frontmatter-declared + auto-assigned rows + clipboard) exceeds the available height, the cursor stays visible by adjusting the scroll offset as `↑`/`↓` move it. Single-key selection continues to work regardless of scroll position.

Overflow rows (those past the auto-assign pool) are not visible in the board even with scrolling.

Search results use the same visible-height limit and scroll with `↑`/`↓`, including the empty-query complete catalog.

## Non-goals

- modifier-key combos for keybinds (current contract is single printable char only)
- live preview of clipboard content (read-on-select only)
- inline prompt editing
- re-ordering the board at runtime
