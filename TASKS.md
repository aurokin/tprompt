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

Status: complete

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

## Phase 2a — CLI foundation

Goal: define the execution model for all non-interactive commands before wiring
individual handlers.

Tasks:

- load config from TOML with defaults + validation
- normalize and validate:
  - `prompts_dir`
  - delivery mode
  - sanitize mode
  - keybind pool
  - reserved keys
  - command-style fields such as `clipboard_read_command`
- define precedence rules:
  - CLI flags
  - prompt frontmatter defaults
  - config file
  - built-in defaults
- introduce typed command/application dependencies so CLI handlers can be tested
  against fake store/tmux/clipboard implementations
- map internal errors to stable MVP exit codes
- add `testscript` black-box coverage for config resolution and exit-code behavior

Libraries introduced this phase:

- `github.com/BurntSushi/toml` (config loading)
- `github.com/rogpeppe/go-internal/testscript` (CLI black-box tests)

Read first:

- `docs/commands/cli.md`
- `docs/storage/config.md`
- `docs/implementation/error-handling.md`

## Phase 2b — read-only CLI commands

Goal: ship the user-facing commands that do not require tmux delivery.

Status: complete

Tasks:

- implement `tprompt list`
- implement `tprompt show <id>`
- implement a baseline `tprompt doctor` covering:
  - config load/validation
  - prompt directory existence
  - prompt discovery
  - duplicate ID detection
  - duplicate/reserved/malformed keybind detection
  - tmux presence/context detection
- lock exact output formatting for `list`, `show`, and `doctor`
- expand `testscript` coverage for stdout/stderr contracts and command-specific
  exit codes

Read first:

- `docs/commands/cli.md`
- `docs/storage/config.md`
- `docs/implementation/error-handling.md`

## Phase 3 — tmux adapter and synchronous delivery

Goal: centralize tmux command generation/inspection and wire direct delivery for
`tprompt send`.

Tasks:

- detect whether the process is inside tmux
- identify current pane/session/client when possible
- resolve `tprompt send` target pane:
  - explicit `--target-pane`
  - current pane context when available
  - clear failure when neither is available
- implement pane existence checks
- implement selected-pane checks
- implement paste delivery: `load-buffer` + `paste-buffer -d -p` with separate `send-keys Enter` when `--enter`
  - use a per-invocation buffer name (`tprompt-send-<pid>-<unix-nanos>`) since
    direct `send` has no daemon-issued job ID
- implement type delivery: `send-keys -l` with rune-boundary chunking (4096B
  default)
- implement `display-message` error surfacing with `-c <client-tty>` scoping
- introduce a `Runner` seam around `os/exec` so tmux command construction is
  unit-testable without invoking real tmux
- define tmux error taxonomy and wire into `app.ExitCode`:
  - `tmux.EnvError` (not inside tmux, no target supplied) → exit 4
  - `tmux.PaneMissingError` (resolved/supplied pane does not exist) → exit 4
  - `tmux.DeliveryError` (load-buffer / paste-buffer / send-keys non-zero) → exit 6
- wire `tprompt send <id>` for synchronous direct delivery (never via daemon):
  - flags `--target-pane`, `--mode`, `--enter`, `--sanitize`
  - `ResolveDelivery` (config package) is the first real caller — handler must
    feed it flags + prompt frontmatter defaults
  - enforce `max_paste_bytes` before the adapter is invoked (applies to both
    modes, per `docs/tmux/delivery.md`)
  - `--sanitize` flag is accepted and validated, but the sanitizer itself
    lands in Phase 3.5; Phase 3 delivers bodies as-is
- `CapturePaneTail` (from `Adapter` interface) is deferred to Phase 4; it has
  no caller in Phase 3 and exists for daemon post-injection verification
- add adapter-focused tests for target resolution, command construction,
  chunking, and tmux-specific failure modes (against a fake Runner)
- add `testscript` coverage for `tprompt send`: happy path (paste, type,
  enter), pane-missing, not-in-tmux-no-target, oversize body, unknown prompt
  id — using a test-only Deps hook that injects a fake adapter

Read first:

- `docs/tmux/integration.md`
- `docs/tmux/delivery.md`
- `docs/tmux/verification.md`
- `docs/implementation/interfaces.md`
- `docs/implementation/error-handling.md`

## Phase 3.5 — clipboard reader and sanitizer

Goal: land the two content-source/transformation modules. The sanitizer is
fully wired into `tprompt send` in this phase. The clipboard reader is built
and wired into `Deps.NewClip`, but has no CLI caller until Phase 5.5
(`tprompt paste`) and Phase 5b (TUI `P`). `tprompt send` never reads the
clipboard.

Status: complete

Locked decisions (resolved during plan review):

- `max_paste_bytes` is enforced **pre-sanitize** — the cap gates what we hand
  to the sanitizer, not what the sanitizer emits.
- Strict-mode rejection exits with code **3** (content-validation error,
  parallel to clipboard validation failures), not 6.
- Strict-mode byte offsets in error messages are **0-based** (matches Go slice
  indexing; no internal-vs-user translation).
- `doctor` clipboard check is **warn-only** when no reader is detected, and
  does **not** exec a dry-run (xclip/xsel exit non-zero on empty clipboards,
  which would false-fail). `exec.LookPath` is the only probe.

### Sanitizer

- implement `safe` (strip dangerous classes: OSC, DCS, CSI mode toggles,
  application keypad, DEC private modes) and `strict` (reject on any escape
  sequence, cosmetic included)
- add a concrete error type `sanitize.StrictRejectError{Class, Offset}` whose
  `Error()` matches the shape in `docs/implementation/sanitization.md`
- map `sanitize.StrictRejectError` to `ExitPrompt` (3) in `app.ExitCode`
- test against a fixture corpus: each dangerous class (one positive case
  each), each cosmetic class (preserved in `safe`, rejected in `strict`),
  multi-byte UTF-8 adjacent to escape sequences, identity in all modes for
  content with no sequences

### Sanitizer wiring into `tprompt send`

- construct the sanitizer from `delivery.Sanitize` inside `runSend`
- call `Process` **after** the `max_paste_bytes` check and **before** the
  adapter call (paste or type)
- remove the "currently validated but not applied" note from the `--sanitize`
  flag help in `internal/app/commands.go`
- extend `testscript` coverage for `tprompt send --sanitize strict` on an
  escape-carrying fixture (exit 3, no delivery attempted)

### Clipboard reader

- implement `NewAutoDetect(getenv, lookPath)`:
  - `runtime.GOOS == "darwin"` → `pbpaste`
  - else `WAYLAND_DISPLAY` set → `wl-paste`
  - else `DISPLAY` set → try `xclip -selection clipboard -o`, then
    `xsel -b -o`, each gated on `lookPath`
  - else return an error shaped for the install-hint message in
    `docs/storage/clipboard.md`
- implement `NewCommand(argv)` that execs the argv, returns stdout on exit 0,
  and surfaces stderr verbatim on non-zero
- seams `getenv` and `lookPath` are injected so tests can fake platform and
  `$PATH` without touching the real host
- add a shared `clipboard.Validate(content []byte, cap int64) error` helper
  (empty / non-UTF-8 / oversize) using the error strings locked in
  `docs/implementation/error-handling.md`; consumed by Phase 5.5 and 5b
- tests: platform/env selection, X11 fallback order, command-reader stdout
  capture, non-zero-exit stderr surfacing, `Validate` matrix

### Clipboard wiring

- replace `Deps.NewClip` stub in `internal/app/deps.go`:
  - if `cfg.ClipboardArgv` non-empty → `clipboard.NewCommand(cfg.ClipboardArgv)`
  - else → `clipboard.NewAutoDetect(...)`
- no CLI caller yet; the wiring exists so Phase 5.5 and 5b have a seam and
  `doctor` can share the resolution logic

### Doctor

- add a clipboard section to `runDoctor`:
  - if `cfg.ClipboardArgv` set → report
    `clipboard reader: <argv[0]> (override)` and warn if `exec.LookPath` fails
  - else run the auto-detect selection and report
    `clipboard reader: <tool> (auto-detected, <reason>)`, or
    `clipboard reader: none available` as a **warning**
- doctor exit code is unaffected by clipboard warnings (a user who only runs
  `tprompt send` should still see `doctor` succeed on a clipboard-less host)

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
