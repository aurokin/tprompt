# Tasks

This file breaks the implementation into phases. Each task references the deeper docs that matter for that step.

## Phase 0 — repo and scaffolding

Goal: create the project skeleton and implementation shape.

Tasks:

- choose implementation language and create repo skeleton
- create packages/modules for:
  - CLI
  - prompt store
  - tmux adapter
  - daemon/queue
  - config
- add formatter/linter/test scaffolding
- add fixture prompt files for tests

Read first:

- `DECISIONS.md`
- `EXPECTATIONS.md`
- `docs/architecture/overview.md`
- `docs/architecture/components.md`

## Phase 1 — prompt discovery and resolution

Goal: make prompt IDs resolvable from markdown files.

Tasks:

- walk prompt directory recursively
- accept `.md` files only for MVP
- derive ID from filename stem
- detect duplicate stems
- parse optional frontmatter
- expose APIs:
  - list prompts
  - resolve prompt by ID
  - return body + metadata + source path

Read first:

- `docs/storage/prompt-store.md`
- `docs/architecture/data-model.md`
- `docs/implementation/interfaces.md`

## Phase 2 — basic CLI commands

Goal: implement the user-facing commands that do not depend on popup deferral yet.

Tasks:

- `tprompt list`
- `tprompt show <id>`
- `tprompt send <id>`
- `tprompt doctor`
- basic output formatting and exit codes

Read first:

- `docs/commands/cli.md`
- `docs/storage/config.md`
- `docs/implementation/error-handling.md`

## Phase 3 — tmux adapter

Goal: centralize tmux command generation and target inspection.

Tasks:

- detect whether the process is inside tmux
- identify current pane/session/client when possible
- implement pane existence checks
- implement selected-pane checks
- implement capture-pane helper
- implement paste delivery
- implement type delivery

Read first:

- `docs/tmux/integration.md`
- `docs/tmux/verification.md`
- `docs/implementation/interfaces.md`

## Phase 4 — daemon and job queue

Goal: implement deferred popup delivery with local IPC.

Tasks:

- create a per-user daemon
- define local socket path
- define job payload
- enqueue send jobs
- validate target pane before execution
- return structured success/failure to the CLI

Read first:

- `docs/commands/daemon.md`
- `docs/architecture/components.md`
- `docs/architecture/data-model.md`
- `docs/implementation/error-handling.md`

## Phase 5 — popup flow

Goal: make popup usage the best experience.

Tasks:

- implement `tprompt popup`
- capture original pane/client/session context
- launch picker
- submit job to daemon
- exit popup process cleanly
- daemon waits for verification condition
- daemon injects after popup closes and target is valid

Read first:

- `docs/commands/popup-flow.md`
- `docs/tmux/verification.md`
- `examples/tmux-bindings.md`

## Phase 6 — tests and hardening

Goal: make failures explicit and predictable.

Tasks:

- complete unit tests for store/config/daemon payload validation
- add tests for duplicate prompt IDs
- add tests for body/frontmatter behavior
- add tests for CLI exit codes
- add fake/mock tmux adapter tests
- document known limitations clearly

Read first:

- `docs/testing/test-plan.md`
- `EXPECTATIONS.md`
- `docs/implementation/error-handling.md`

## Phase 7 — polish

Goal: make the tool pleasant to use without changing scope.

Tasks:

- improve `doctor`
- improve help text
- improve prompt list/show formatting
- support configurable picker command if desired
- refine logs and daemon status output

Read first:

- `docs/commands/cli.md`
- `docs/storage/config.md`

## Deferred backlog

Do not implement during MVP unless explicitly requested:

- templating variables
- prompt history
- app-specific adapters
- richer verification strategies
- remote sending
- GUI or web layer

See:

- `docs/roadmap/future-phases.md`
