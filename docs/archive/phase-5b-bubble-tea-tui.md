# Phase 5b: Built-in Bubble Tea TUI (archived PRD)

Historical. Live contract: `docs/commands/tui.md`, DECISIONS §15-§25.

## Problem

After 5a the popup closed before any selection was possible; the plumbing
worked but the pick-a-prompt workflow did not.

## Solution

A Bubble Tea `Renderer` replacing the cancel stub, with no command-shell
changes:

- Compact three-column board `[key]  id  description`, clipboard row pinned
  first, rows alphabetical by id. Long descriptions truncate with an ellipsis;
  description falls back to title, then blank.
- Single-key selection, case-insensitive for letters (rendered lowercase);
  digits and symbols match literally. Unbound prompts take keys from the
  auto-assign pool in id order; prompts beyond the pool are hidden from the
  board, reachable by search, and counted as `(N more)` in the footer.
- `/` search: empty query shows the whole catalog (overflow and clipboard row
  included); per-keystroke fuzzy filtering ranked by weighted fields
  (id 1.0, title 0.75, description 0.5, tags 0.25, summed per row,
  alphabetical tiebreak); `[N matches]` counter; the highlighted prompt stays
  highlighted across query edits when still present. In search mode every
  printable key goes to the query; Esc returns to the board; Enter selects.
- Clipboard key reads the clipboard asynchronously via a `tea.Cmd`, validates
  it, and submits or shows an inline error and stays open.
- Inline errors clear on a real action (select, search, retry, cancel) and
  persist through navigation so they can be read.
- `↑/↓` scrolls when the board exceeds the viewport; single-key selection works
  regardless of scroll position.
- Esc on the board and Ctrl+C anywhere exit 0. Empty store shows only the
  clipboard row with `no prompts found — press P for clipboard or Esc to exit`.
- The clipboard key is the configured reserved key, never a hardcoded `P`.

## Design

- One `Model` with a `modeBoard | modeSearch` enum, no nested submodels; `Update`
  is a pure `(model, msg) -> (model, cmd)` so every transition is unit-testable
  without a terminal.
- The search indexer is a deep module wrapping `github.com/sahilm/fuzzy`; the
  rest of the TUI sees only ranked rows.
- Submit failures propagate out of `Renderer.Run` (no stay-open on submit
  failure).

## Testing approach

Indexer ranking tests (field precedence, tiebreak, multi-field sums, empty
results) and pure `Update` transition tests. No golden-file `View()` tests in
this phase; layout regressions were left to manual testing.

## Deferred then

Scroll indicators, match-character highlighting, a "reading clipboard" spinner,
timed error fade, history, multi-character or modifier keybinds, live reload of
prompt files while open.
