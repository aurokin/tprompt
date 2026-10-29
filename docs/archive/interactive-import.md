# Interactive Import and Frontmatter Parity (archived PRD)

Historical. Follow-on to the Wispr import. Live contract: DECISIONS §34,
EXPECTATIONS (Import), `docs/implementation/interfaces.md#import-tui`.

## Problem

1. Imported prompts carried only `title` and `tags`, not the
   `description` / `key` / `mode` / `enter` stubs that `tprompt new` writes.
2. Import was all-or-nothing and scriptable-only: no way to pick snippets or
   resolve conflicts per item.

## Locked decisions

- Interactive mode is opt-in via `-i` / `--interactive`; bare `tprompt import`
  kept printing help (a bare-to-TUI dispatch was left to an optional capstone).
- The TUI only captures intent and returns a plan. `internal/app` replays the
  existing import path as the single writer and single owner of collision
  policy and the stdout / stderr / exit-code contract, so scripted and
  interactive results are structurally equivalent.
- Per-item overwrite is allowed (finer than `--overwrite`) but only for an exact
  same-path target, through the same writer, which still refuses cross-path
  duplicates (exit 3). Cross-path rows are not selectable. This widened §34 and
  had to be documented.
- `-i` without a tty, and `-i --dry-run`, are usage errors (exit 2) rather than
  silent fallbacks.
- A separate `internal/importtui` package, sibling to `internal/tui`; neither
  imports the other. The only change to `internal/tui` was exporting the search
  index constructor. Small pure helpers (viewport clamp, reserved-key matching)
  are copied rather than shared.

Defaults chosen: fresh items pre-checked, exact-target items unchecked
(skip by default), cross-path items non-selectable; `-i --project` honored.
Imported prompts get no `key:` and join the auto-assign pool, so re-import can
shift board key allocation; no board-stability guarantee.

## Slices

Frontmatter parity; extraction of a write-free plan used by the non-interactive
loop (byte-identical behavior); `importtui` tracer with `-i` wiring; conflict
review glyphs and per-item overwrite; `/` search reusing the board's index;
optional `ImportSource` seam and bare-import dispatch.

## Risks and mitigations

- Scriptable-path divergence: the TUI never writes or prints; a testscript pins
  confirm-all interactive output to the non-interactive output.
- Trusting a UI verdict at write time: the authoritative collision check and
  exclusive-create writer re-run at write.
- Frontmatter drift between `new` and import: a key-set and order parity test.
