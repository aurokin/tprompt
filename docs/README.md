# tprompt Docs

These docs use progressive disclosure. Read the narrowest document that answers
your question, then follow links only when you need deeper context.

Execution tracking lives in Linear. This repository documents durable truth:
contracts, invariants, test seams, failure semantics, and proof surfaces.

## I Want To Use `tprompt`

- [README](../README.md) - quick product overview and commands.
- [CLI commands](commands/cli.md) - command behavior and exit codes.
- [TUI](commands/tui.md) - built-in board, search, clipboard row, and key behavior.
- [TUI flow](commands/tui-flow.md) - popup-to-daemon delivery sequence.
- [Paste command](commands/paste.md) - clipboard delivery behavior.
- [Tmux bindings](../examples/tmux-bindings.md) - popup binding examples.

## I Want To Change Command Behavior

- [Behavior contract](../EXPECTATIONS.md) - current user-visible guarantees.
- [CLI commands](commands/cli.md) - command-specific behavior.
- [Error handling](implementation/error-handling.md) - exit codes and error taxonomy.
- [Config](storage/config.md) - config fields and precedence.
- [Prompt store](storage/prompt-store.md) - prompt IDs, frontmatter, and keybinds.

## I Want To Change The TUI

- [TUI](commands/tui.md) - interaction contract and rendering rules.
- [TUI flow](commands/tui-flow.md) - daemon handoff behavior.
- [Architecture overview](architecture/overview.md) - where the TUI sits in the system.
- [Interfaces](implementation/interfaces.md) - renderer, state, and submitter seams.
- [Testing harness](testing/harness.md) - proof surfaces for model/view behavior.

## I Want To Change Tmux Delivery

- [Tmux delivery](tmux/delivery.md) - command construction for paste and type modes.
- [Tmux verification](tmux/verification.md) - readiness semantics.
- [Tmux integration](tmux/integration.md) - environment assumptions.
- [Error handling](implementation/error-handling.md) - tmux and delivery failure mapping.
- [Testing harness](testing/harness.md) - fake runner and adapter test strategy.

## I Want To Change The Daemon

- [Daemon command](commands/daemon.md) - daemon lifecycle and operator behavior.
- [Data model](architecture/data-model.md) - deferred job and status shapes.
- [Components](architecture/components.md) - package responsibilities.
- [Interfaces](implementation/interfaces.md) - daemon client/server seams.
- [Testing harness](testing/harness.md) - socket, queue, executor, and logging tests.

## I Want To Add Tests

- [Testing harness](testing/harness.md) - what to test and where.
- [Interfaces](implementation/interfaces.md) - seams designed for isolation.
- [Error handling](implementation/error-handling.md) - failure contracts to assert.

## I Want Historical Context

- [Locked decisions](../DECISIONS.md) - durable decisions that should not drift.
- [Future phases](roadmap/future-phases.md) - intentionally deferred product ideas.
