# Components

This file gives a more concrete internal breakdown.

## Suggested packages/modules

### `cmd/tprompt`
CLI entrypoint.

### `internal/config`
Config loading, validation, defaults, path expansion.

### `internal/store`
Prompt discovery, parsing, duplicate detection, prompt lookup.

### `internal/promptmeta`
Frontmatter parsing and body extraction helpers.

### `internal/tmux`
All tmux-facing functions and types.

### `internal/daemon`
IPC server, job handling, verification loop.

### `internal/picker`
Interactive selection logic.

### `internal/app`
Command orchestration if a shared application service layer is helpful.

## Suggested core services

### PromptIndex
An immutable or cheaply rebuildable view of available prompts.

Responsibilities:

- scan directory
- detect duplicates
- return prompt summaries
- resolve prompt by ID

### TmuxContextResolver
Returns best-effort information about current tmux state.

### DeliveryEngine
Takes prompt content + target info + mode and performs injection.

### VerificationEngine
Evaluates whether it is safe to inject yet.

### JobQueue / DaemonServer
Receives jobs and processes them serially or with carefully bounded concurrency.

## Concurrency guidance

MVP can keep daemon execution simple.

Recommended default:

- single daemon process
- jobs processed one at a time or with very small concurrency
- avoid complex locking schemes unless proven necessary

## Logging guidance

The daemon should emit logs that help diagnose:

- target pane vanished
- popup did not return to expected pane
- tmux command failed
- duplicate ID prevented resolution
- IPC failure occurred
