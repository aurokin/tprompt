# tprompt Docs

These docs use progressive disclosure. Start from the intent that matches your
work, read the narrowest contract first, then follow links only when you need
deeper context.

Execution tracking lives in Linear. This repository documents durable harness
engineering material: user-visible contracts, locked decisions, architecture,
interfaces, failure semantics, and proof surfaces. PRDs and issue breakdowns
belong in Linear milestones, not in the repo.

## Documentation Rules

- Keep entrypoints short: `README.md` for product orientation, this file for routing, subsystem docs for details.
- Link to the narrowest durable doc instead of duplicating contracts across files.
- When behavior changes, update the behavior contract, the subsystem doc, and the proof surface together.
- When adding a new doc, add it here under the intent it serves.

## I Want To Use `tprompt`

- [README](../README.md) - quick product overview and commands.
- [CLI commands](commands/cli.md) - command behavior and exit codes.
- [TUI](commands/tui.md) - built-in board, search, clipboard row, and key behavior.
- [TUI flow](commands/tui-flow.md) - popup-to-handoff delivery sequence.
- [Paste command](commands/paste.md) - clipboard delivery behavior.
- [Import from Wispr Flow](commands/cli.md#tprompt-import-wispr) - bring Wispr snippets in as prompts.
- [Tmux bindings](../examples/tmux-bindings.md) - popup binding examples.

## I Want To Change Command Behavior

- [Behavior contract](../EXPECTATIONS.md) - current user-visible guarantees.
- [CLI commands](commands/cli.md) - command-specific behavior.
- [Error handling](implementation/error-handling.md) - exit codes and error taxonomy.
- [Config](storage/config.md) - config fields and precedence.
- [Prompt store](storage/prompt-store.md) - prompt IDs, frontmatter, and keybinds.
- [Testing harness](testing/harness.md) - app-level and testscript proof surfaces.

## I Want To Change The TUI

- [TUI](commands/tui.md) - interaction contract and rendering rules.
- [TUI flow](commands/tui-flow.md) - short-lived handoff worker behavior.
- [Architecture overview](architecture/overview.md) - where the TUI sits in the system.
- [Interfaces](implementation/interfaces.md) - renderer, state, and submitter seams.
- [Search index (shared fuzzy core)](implementation/interfaces.md#search-index-shared-fuzzy-core) - the dependency-free ranking core the board and the import picker both adapt to.
- [Testing harness](testing/harness.md) - proof surfaces for model/view behavior.

## I Want To Change Tmux Delivery

- [Tmux delivery](tmux/delivery.md) - command construction for paste and type modes.
- [Tmux verification](tmux/verification.md) - readiness semantics.
- [Tmux integration](tmux/integration.md) - environment assumptions.
- [Error handling](implementation/error-handling.md) - tmux and delivery failure mapping.
- [Testing harness](testing/harness.md) - fake runner and adapter test strategy.

## I Want To Change Import (External Ingest)

- [CLI commands](commands/cli.md#tprompt-import-wispr) - `import wispr` behavior, flags, and exit codes (and the bare-`import` dispatch).
- [Locked decisions §34](../DECISIONS.md#34-wispr-flow-snippet-import) - the import contract (read-only driver, id minting, skip-existing, error taxonomy, source seam, bare-import dispatch).
- [Behavior contract](../EXPECTATIONS.md#import) - user-visible import guarantees.
- [Import source seam](implementation/interfaces.md#import-source-seam) - the `ImportSource`/`ImportRecord` interfaces to add a new source (the engine is generic).
- [Interactive picker](implementation/interfaces.md#import-tui) - the `importtui` conflict-review picker for `import wispr -i`.
- [Search index (shared fuzzy core)](implementation/interfaces.md#search-index-shared-fuzzy-core) - the picker's `/`-search ranking, shared with the board.
- [Wispr import reader](implementation/interfaces.md#wispr-import-reader) - the `wispr.Reader` seam and `Deps.NewWisprReader` injection.
- [Prompt store](storage/prompt-store.md) - the filename-stem IDs and frontmatter the importer writes into.
- [Testing harness](testing/harness.md#import-wispr) - import proof surfaces (unit, app, testscript, fixture DB).

## I Want To Change Prompt Storage Or Config

- [Prompt store](storage/prompt-store.md) - discovery, frontmatter, IDs, shadowing, and keybinds.
- [Config](storage/config.md) - config fields, defaults, and validation.
- [Clipboard](storage/clipboard.md) - same-host clipboard detection and validation.
- [Testing harness](testing/harness.md) - store/config/clipboard proof surfaces.

## I Want To Change Architecture Or Module Boundaries

- [Architecture overview](architecture/overview.md) - system shape and data flow.
- [Components](architecture/components.md) - package responsibilities.
- [Data model](architecture/data-model.md) - stable structs and wire shapes.
- [Interfaces](implementation/interfaces.md) - seams designed for isolation.
- [Tech stack](implementation/tech-stack.md) - toolchain and implementation choices.

## I Want To Add Tests

- [Testing harness](testing/harness.md) - what to test and where.
- [Phase 6 coverage audit](testing/phase-6-coverage.md) - release-hardening checklist mapped to proof.
- [Interfaces](implementation/interfaces.md) - seams designed for isolation.
- [Error handling](implementation/error-handling.md) - failure contracts to assert.

## I Want Historical Context

- [Locked decisions](../DECISIONS.md) - durable decisions that should not drift.
- [Future phases](roadmap/future-phases.md) - intentionally deferred product ideas.
