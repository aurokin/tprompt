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
  - clipboard reader
  - sanitizer
  - TUI
  - keybind resolver
- add formatter/linter/test scaffolding
- add fixture prompt files for tests (including ones with `key:` frontmatter and sanitize-relevant content)

Read first:

- `DECISIONS.md`
- `EXPECTATIONS.md`
- `docs/architecture/overview.md`
- `docs/architecture/components.md`
- `docs/implementation/tech-stack.md`

## Phase 1 — prompt discovery and resolution

Goal: make prompt IDs resolvable from markdown files, with keybind validation.

Tasks:

- walk prompt directory recursively
- accept `.md` files only for MVP
- derive ID from filename stem
- detect duplicate stems
- parse optional frontmatter (`title`, `description`, `tags`, `mode`, `enter`, `key`)
- validate `key:` values (duplicate / reserved / malformed → hard error)
- expose APIs:
  - list prompts
  - resolve prompt by ID
  - return body + metadata + source path
  - resolve final keybind assignment (frontmatter + auto-assign from pool)

Libraries introduced this phase:

- `gopkg.in/yaml.v3` (frontmatter parsing)

Read first:

- `docs/storage/prompt-store.md`
- `docs/architecture/data-model.md`
- `docs/implementation/interfaces.md`

## Phase 2 — basic CLI commands

Goal: implement the user-facing commands that do not depend on TUI-flow deferral yet.

Tasks:

- `tprompt list`
- `tprompt show <id>`
- `tprompt send <id>`
- `tprompt doctor` (including duplicate-keybind checks)
- basic output formatting and exit codes (including cancel = 0)

Libraries introduced this phase:

- `github.com/BurntSushi/toml` (config loading)
- `github.com/rogpeppe/go-internal/testscript` (CLI black-box tests)

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
- implement paste delivery: `load-buffer` + `paste-buffer -d -p` with separate `send-keys Enter` when `--enter`
- implement type delivery: `send-keys -l` with rune-boundary chunking
- implement `display-message` error surfacing with `-c <client-tty>` scoping

Read first:

- `docs/tmux/integration.md`
- `docs/tmux/delivery.md`
- `docs/tmux/verification.md`
- `docs/implementation/interfaces.md`

## Phase 3.5 — clipboard reader and sanitizer

Goal: add the two content-source/transformation modules needed for paste and strict-mode workflows.

Tasks:

- implement `ClipboardReader` with auto-detect (pbpaste, wl-paste, xclip, xsel) and command override
- implement `doctor` reporting for clipboard reader resolution
- implement `Sanitizer` with `off`/`safe`/`strict` modes
- test sanitizer against fixture corpus covering each escape-sequence class
- wire both into `tprompt send` and the future `tprompt paste`

Read first:

- `docs/storage/clipboard.md`
- `docs/implementation/sanitization.md`
- `docs/implementation/error-handling.md`

## Phase 4 — daemon and job queue

Goal: implement deferred TUI-flow delivery with local IPC.

Tasks:

- create a per-user daemon
- define local socket path
- define job payload (including `source`, `sanitize_mode`, and captured clipboard bytes when applicable)
- enqueue send jobs
- implement replace-same-target semantics
- validate target pane before execution
- run sanitizer immediately before adapter delivery
- return structured success/failure to the CLI
- surface failures via `tmux display-message` + append-only log

Read first:

- `docs/commands/daemon.md`
- `docs/architecture/components.md`
- `docs/architecture/data-model.md`
- `docs/implementation/error-handling.md`

## Phase 5 — TUI flow and built-in TUI

Goal: make the TUI flow (typically launched from a tmux popup) the best experience.

### Phase 5a — TUI shell

- implement `tprompt tui` command
- capture target pane/client/session context
- wire to the built-in TUI
- submit job to daemon based on TUI result
- exit TUI process cleanly (cancel = exit 0)

(Bare-`tprompt` dispatch per DECISIONS.md §29 was wired in Phase 0; Phase 5a only replaces the `tui` stub with the real command.)

Libraries introduced this phase:

- `github.com/charmbracelet/bubbletea` (TUI runtime)
- `github.com/charmbracelet/lipgloss` (TUI styling, used by Phase 5b)

### Phase 5b — built-in TUI

- render three-column row layout (`[key]  id  description`)
- soft-truncate description with ellipsis
- fall back `description → title → blank`
- render reserved-key hints in footer
- handle single-key prompt selection
- handle `P` for clipboard (read-on-keypress, validate, submit)
- handle `/`-search with fuzzy matching over id + title + description + tags
- handle `Esc` cancel and `Esc` exit-search
- handle overflow (search-only, not on board)
- inline error display on clipboard validation failure

Read first:

- `docs/commands/tui-flow.md`
- `docs/commands/tui.md`
- `docs/tmux/verification.md`
- `examples/tmux-bindings.md`

## Phase 5.5 — `tprompt paste`

Goal: dedicated clipboard-delivery command, synchronous, no daemon.

Tasks:

- implement `tprompt paste` command with `--target-pane`, `--mode`, `--enter`, `--sanitize` flags
- read clipboard via the configured reader
- validate (empty / UTF-8 / size cap)
- run sanitizer
- deliver via tmux adapter

Read first:

- `docs/commands/paste.md`
- `docs/storage/clipboard.md`
- `docs/tmux/delivery.md`

## Phase 6 — tests and hardening

Goal: make failures explicit and predictable.

Tasks:

- complete unit tests for store/config/daemon payload validation
- add tests for duplicate prompt IDs and duplicate/reserved/malformed keybinds
- add tests for body/frontmatter behavior including `key:` field
- add tests for fuzzy search ranking
- add tests for sanitizer corpus across all three modes
- add tests for clipboard reader detection and override
- add tests for CLI exit codes (including cancel = 0)
- add fake/mock tmux adapter tests for paste + type command construction
- add TUI rendering tests
- document known limitations clearly (same-host clipboard, no modifier keybinds, no live clipboard preview)

Read first:

- `docs/testing/test-plan.md`
- `EXPECTATIONS.md`
- `docs/implementation/error-handling.md`

## Phase 7 — polish

Goal: make the tool pleasant to use without changing scope.

Tasks:

- improve `doctor`
- improve help text
- improve prompt list/show formatting (show resolved keybind)
- support configurable picker command if desired (for `tprompt pick` only)
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
- modifier-key combos for TUI keybinds
- live clipboard preview in the TUI
- cross-host clipboard

See:

- `docs/roadmap/future-phases.md`
