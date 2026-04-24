# TUI

The TUI is a **built-in** interactive terminal UI. It is not a thin wrapper around an external picker. `picker_command` in config is a separate mechanism used only by `tprompt pick` (see `cli.md`).

This file describes what the TUI renders and how keybinds behave. For the end-to-end delivery flow (daemon handoff, verification, injection) see `docs/commands/tui-flow.md`.

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
[1]  code-review          Review for correctness, risk, and missing tests
[2]  commit               Generate a conventional commit message
[3]  deploy-checklist     Preflight checks before prod push
[q]  quick-hack           Short quick prompt
[c]  code-merge           Merge review prompt
```

When the clipboard action is bound to a printable reserved key, the clipboard row is **first** and pinned. It has no prompt id; it may render a short hint such as `(read on select)`. If the clipboard reserved key is disabled or symbolic, the board omits the pinned clipboard row, but clipboard remains reachable from the empty-query search catalog.

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

Overflow: once the auto-assign pool is exhausted and frontmatter keys are satisfied, remaining prompts are **not shown on the board**. They are reachable only via `/`-search.

### Collisions (hard errors at load time)

- two prompts declaring the same `key:`
- a prompt declaring a reserved key (e.g., `key: P` when `P` is reserved for clipboard)
- a malformed `key:` value (empty string, multi-character, non-printable, modifier combo)

These surface in `tprompt doctor` and cause `tprompt list|show|send|tui` to fail.

## Search mode

Triggered by `/`. All prompts (including overflow) are searchable — search is the complete-catalog view.

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
  4. On success, the TUI submits a `DeliveryRequest` with `source = clipboard` to the daemon and exits.

## Footer / status line

The TUI renders a single-line footer showing context-sensitive hints:

- board view: `[/ search]  [Esc cancel]`, or `[/ search (N more)]  [Esc cancel]` when overflow exists
- search view: `/query    [Esc exit search]  [Enter select]  [N matches]`
- error view: `clipboard is empty — choose another option  [Esc cancel]`

## Scrolling

If the board (frontmatter-declared + auto-assigned rows + clipboard) exceeds the available height, vertical scrolling is permitted with `↑`/`↓` but single-key selection continues to work regardless of scroll position.

Overflow rows (those past the auto-assign pool) are not visible in the board even with scrolling.

Search results use the same visible-height limit and scroll with `↑`/`↓`, including the empty-query complete catalog.

## Non-goals

- modifier-key combos for keybinds (MVP is single printable char only)
- live preview of clipboard content (read-on-select only)
- inline prompt editing
- re-ordering the board at runtime
