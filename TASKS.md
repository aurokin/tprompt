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

Goal: implement deferred TUI-flow delivery with local IPC. The daemon has no
in-tree caller until Phase 5 (TUI); Phase 4 builds and tests it end-to-end via
direct `daemon.Client` usage and `tprompt daemon start`/`status`.

Status: complete

Locked decisions (resolved during plan review):

- **Wire format:** line-delimited JSON, one request per connection. Server
  reads one request, writes one response, closes. No multi-message framing.
- **`Submit` is fire-and-ack.** It returns immediately with
  `{Accepted, ReplacedJobID}`; verification + delivery happens asynchronously
  on the server. The TUI (Phase 5) submits and exits — it does not wait for
  delivery.
- **No `daemon stop` subcommand.** Lifecycle is SIGINT/SIGTERM only. The
  current CLI scaffold and `TestZeroArgCommandsAcceptBareInvocation` already
  reflect this — only `start` and `status` are registered. `stop` may be
  revisited if a real need surfaces; deferred to Phase 7.
- **No CLI auto-start of the daemon.** `tprompt tui` (Phase 5) and
  `tprompt daemon status` will fail with a clear "daemon not running" error if
  the socket is unreachable. Auto-start is deferred to Phase 7.
- **Post-injection capture-pane verification is out of MVP.** Optional per
  `docs/tmux/verification.md`. `CapturePaneTail` is wired but unused by the
  daemon. May land in Phase 7 if it earns its keep.
- **Stale-socket detection** uses a dial-probe: `net.DialTimeout("unix", path,
  200ms)`. Dial succeeds → another daemon is live, refuse to start with
  `SocketUnavailableError`. Dial fails → unlink, bind. Same primitive backs
  `daemon status`'s "daemon not running" detection.
- **Daemon entry point is `daemon.Run(ctx, *Server, onReady func()) error`**,
  not a blocking cobra handler. The `daemon start` handler is a thin shim
  that builds the `*Server` from cfg+deps, wires
  `signal.NotifyContext(SIGINT, SIGTERM)`, and calls `Run`. The `onReady`
  callback fires once after `Listen` succeeds and before `Serve` accepts —
  callers gate "daemon started" log/banner output on it. Tests drive `Run`
  with a controlled context.
- **Log format:** logfmt single-line entries
  (`time=… job_id=… pane=… source=… outcome=… msg=…`). Bodies and clipboard
  bytes are never logged (sanitizer rejections record class + offset only,
  consistent with Phase 3.5).
- **Job ID format:** `j-<unix-nanos>-<atomic-counter>` per daemon process.
  Unique within one daemon, monotonic. The daemon mints the ID in
  `Server.handle()` before calling `Queue.Enqueue`; any client-supplied
  `Job.JobID` is discarded so logs and the `SubmitResponse` always reference
  the same authoritative key.

### Phase 4a — data model and wire types

- replace the stub `daemon.Job` with the real model from
  `docs/architecture/data-model.md`: `Job` containing `JobID`, `CreatedAt`,
  `Source`, `PromptID`, `SourcePath`, `Body`, `Mode`, `Enter`, `SanitizeMode`,
  `Target tmux.TargetContext`, `VerificationPolicy`
- define wire envelopes: `SubmitRequest`/`SubmitResponse`,
  `StatusRequest`/`StatusResponse`
- introduce typed errors `daemon.SocketUnavailableError`, `daemon.TimeoutError`,
  `daemon.InvalidPolicyError`
- map all three into `app.ExitCode` → `ExitDaemon` (5)
- delete the existing zero-value `daemon_test.go` stub

### Phase 4b — append-only logger

- `daemon.NewLogger(path)` opens with `O_APPEND|O_CREATE`, mkdir-p the parent
- mutex-guarded `Write`; logfmt one-line entries
- API forbids passing body bytes (signature accepts only metadata fields)
- tests: format snapshot, concurrent-write integrity, parent-dir creation

### Phase 4c — job queue with replace-same-target

- `Queue` keyed by `target.PaneID`; mutex-protected map of pending workers
- `Enqueue(job)`:
  - if a worker exists for the same pane: keep only the newest pending job;
    cancel the active worker and wait for it to exit before starting its
    replacement; queued replacements are discarded immediately
  - log `outcome=replaced msg="replaced by job <new-id>"` and send
    `DisplayMessage(target, "tprompt: replaced by a newer job — this delivery was dropped")`
    for the displaced job
  - register the new worker, return `SubmitResult{Accepted, ReplacedJobID}`
- different-pane jobs run concurrently
- tests: same-pane replace cancels and banners the displaced job;
  different-pane concurrent jobs both run; displaced-job banner content
  matches the documented string

### Phase 4d — verification engine

- `Verify(ctx, adapter, target, policy)`:
  - poll `IsTargetSelected(target)` every `VerificationPollIntervalMS` until
    `VerificationTimeoutMS`
  - propagate `tmux.PaneMissingError` immediately when the underlying check
    surfaces it
  - on timeout return `daemon.TimeoutError{TimeoutMS}`
- tests with fake adapter: ready-now; ready-after-N polls; never-ready
  (timeout); pane-vanishes-mid-loop

### Phase 4e — delivery executor

- after verification: `MaxPasteBytes` check → sanitize → `Paste`/`Type`
  (mirrors `runSend`'s ordering — cap is pre-sanitize)
- on any failure: log entry + `DisplayMessage(target, "tprompt: <error>")`
  with explicit pane targeting when `client_tty == ""`
- on success: no log entry, no banner (per spec)
- tests: happy paste; strict-reject (no delivery, banner + log); oversize
  (banner + log); post-verify pane-vanish race surfaces `DeliveryError`;
  adapter `DeliveryError` propagates

### Phase 4f — IPC server, client, and Deps seam

- `Server` binds Unix socket at `cfg.SocketPath` with `0600`; stale-socket
  cleanup via dial-probe; one JSON request per connection; dispatches to
  queue or status responder
- `Client` is a thin `net.Dial("unix", path)` + JSON write/read
- add `Deps.NewDaemonClient func(cfg config.Resolved) (daemon.Client, error)`
  to `internal/app/deps.go`; production wires `daemon.NewSocketClient`
- `StatusResponse`: `pid`, `socket`, `log`, `uptime`, `pending_jobs`, `version`
- tests over real `unix` socket on `t.TempDir()`: submit ack round-trip;
  status round-trip; stale-socket cleanup; refusal when a live daemon holds
  the socket; `0600` permission check

### Phase 4g — CLI wiring

- replace `ErrNotImplemented` in `daemon start` and `daemon status` handlers
- `daemon start`: shim that builds adapter + logger + queue + server, calls
  `daemon.Run(signal.NotifyContext(SIGINT, SIGTERM), cfg, deps)`; clean
  shutdown drains in-flight workers up to a bounded grace period
- `daemon status`: dial socket, send `StatusRequest`, print fields. Connect
  refused → `daemon not running` on stderr + `ExitDaemon` (5)
- update `TestZeroArgCommandsAcceptBareInvocation` to drop the `daemon start`
  and `daemon status` rows (they no longer return `ErrNotImplemented`)
- testscript: `daemon status` when no daemon is running (no live process
  needed). Live-daemon lifecycle is covered by Go integration tests in
  `internal/daemon/`, not testscript.

Read first:

- `docs/commands/daemon.md`
- `docs/architecture/components.md`
- `docs/architecture/data-model.md`
- `docs/implementation/error-handling.md`
- `docs/tmux/verification.md`

## Phase 5 — TUI flow and built-in TUI

Goal: make the TUI flow (typically launched from a tmux popup) the best
experience. Phase 5a ships the command shell and submission path with a
cancel-only stub `Renderer`; Phase 5b swaps in the real bubbletea TUI.

Libraries introduced in Phase 5:

- `github.com/charmbracelet/bubbletea` (TUI runtime, 5b)
- `github.com/charmbracelet/lipgloss` (TUI styling, 5b)
- `github.com/sahilm/fuzzy` (fuzzy search, 5b)

Locked decisions (resolved during plan review):

- **`--target-pane` is required.** Inside a popup, `CurrentContext()` returns
  the popup's own pane, so env-fallback is actively dangerous. Missing flag
  exits 2 with a usage error. Applies equally to bare-`tprompt` dispatch.
- **`--client-tty` and `--session-id` are optional.** When passed, they scope
  daemon failure banners to the originating client; when omitted, the daemon
  falls back to pane-scoped `display-message` per existing Phase 4 behavior.
- **No delivery-override flags on `tprompt tui`.** `--mode`, `--enter`, and
  `--sanitize` are exposed on `send` and `paste` only. TUI submissions resolve
  delivery from frontmatter + config + defaults. Per-prompt overrides belong
  in frontmatter; session-wide knobs belong in config.
- **Pre-flight order: config → store → daemon socket → target pane.** Each
  step short-circuits on error. This surfaces the most fundamental broken
  state first (misconfigured prompts beat missing daemon beats vanished pane).
- **Pre-flight daemon dial runs before rendering.** Failure exits 5 with
  "daemon not running" on stderr. No auto-start (deferred to Phase 7); no
  stay-open fallback.
- **Pre-flight pane existence check.** `adapter.PaneExists(target)` before
  rendering. Missing pane exits 4. Matches `send`'s behavior for an explicit
  `--target-pane`.
- **Store metadata escape-strip at load.** OSC/DCS/CSI sequences stripped
  from `title`, `description`, and each `tags` entry as prompts are loaded.
  Body is untouched (body sanitization remains the user's `--sanitize`
  choice). Touches Phase 1 code but lands in Phase 5a because it is
  load-bearing for Phase 5b rendering safety and also benefits `list`/`show`.
- **TUI owns submission via an injected `Submitter`.** The "stay open on
  clipboard validation error" requirement forces the TUI to know about the
  daemon client anyway; making prompt submission asymmetric would add
  complexity for no gain. One code path: on selection, TUI resolves body,
  validates size, dials daemon, exits.
- **Clipboard read is `tea.Cmd`-async.** `P` dispatches a command that execs
  the clipboard reader; result arrives as a message. No "reading clipboard…"
  feedback. Freezes visually for the (usually <20ms) subprocess duration.
- **Submit failure exits 5 with no retry.** Daemon death between pre-flight
  and submit is non-correctable from inside the TUI. Per `tui-flow.md`.
- **`Ctrl+C` equals `Esc` on the board.** Both cancel with exit 0. Popup
  callers don't benefit from SIGINT distinction.
- **`max_paste_bytes` pre-checked in the TUI.** Oversize prompt body → inline
  footer error, TUI stays open. Mirrors clipboard validation.
- **`ReplacedJobID` from `SubmitResponse` is ignored.** The displaced job's
  banner is surfaced by the daemon in its target pane; no user-facing
  notification in the replacer's TUI.
- **Verification policy from config only.** `VerificationTimeoutMS` and
  `VerificationPollIntervalMS` come from resolved config. No TUI flags, no
  frontmatter overrides.

### Phase 5a — TUI command shell

Status: planned.

- register `tprompt tui` with flags `--target-pane` (required), `--client-tty`
  (optional), `--session-id` (optional); remove the `ErrNotImplemented` stub
- implement `runTUI` orchestrator in `internal/app/`:
  - load + validate config
  - build store, surface load errors (duplicate ID, malformed/duplicate/
    reserved keys) as `ExitPrompt` (3)
  - dial daemon socket; `daemon.SocketUnavailableError` → `ExitDaemon` (5)
  - check `--target-pane` exists via tmux adapter; missing → `ExitTmux` (4)
  - build `tui.State` (board rows alphabetical by id, overflow rows,
    reserved-key map, clipboard-row hint)
  - call `Renderer.Run(state)` → `tui.Result`
  - on `ActionCancel` return nil (exit 0); on selection dispatch to
    `Submitter.Submit(result)`
- extend `tui.Result` with `ClipboardBody []byte` for `ActionClipboard` so the
  bytes captured by the TUI travel through to the daemon (the daemon never
  reads the clipboard itself)
- implement `Submitter` in `internal/tui/` (deep module):
  - for `ActionPrompt`: resolve from store, run
    `config.ResolveDelivery(cfg, frontmatter, nil)`, check body ≤
    `max_paste_bytes` (returns typed `BodyTooLargeError` that the Renderer can
    surface inline without exiting), build `SubmitRequest` with verification
    policy from config, dial `daemon.Client`, return on non-`Accepted`
  - for `ActionClipboard`: same as above but with `Source = clipboard`,
    `Body = result.ClipboardBody`, `PromptID`/`SourcePath` empty; no store
    resolution
- add `Deps.NewRenderer func(state tui.State, submitter tui.Submitter)
  tui.Renderer` to `internal/app/deps.go`; production returns a cancel-stub
  `Renderer` whose `Run` immediately yields `Result{Action: ActionCancel}`;
  tests override this factory to inject canned results and observe the
  captured `SubmitRequest`
- implement the store metadata escape-stripper in `internal/store/`:
  - strip the same escape classes as the `safe` sanitizer from `title`,
    `description`, and each `tags` entry at load
  - body bytes are untouched
  - applies uniformly to every store consumer
- tests:
  - Go unit tests on `Submitter` against a fake `daemon.Client`: prompt-happy,
    prompt-oversize, clipboard-happy, clipboard-oversize, non-`Accepted`
    response, dial failure; assert on captured `SubmitRequest` fields
  - Go unit tests on the metadata stripper: each dangerous class, UTF-8
    adjacency, body-preservation invariant
  - Go integration tests on `runTUI` with fake Renderer + store + daemon
    client + tmux adapter: each pre-flight failure class, cancel path,
    selection → submit happy path
  - testscript: missing flag (exit 2), daemon unreachable (exit 5), pane
    missing (exit 4), stub-Renderer cancel (exit 0), injected-Renderer
    selection (exit 0 with captured request), oversize prompt (exit 3)

### Phase 5b — built-in Bubble Tea TUI

Status: planned.

- replace the cancel-stub `Renderer` in `app.ProductionDeps.NewRenderer` with
  a production implementation that wraps `tea.NewProgram(model).Run()` and
  returns the final `Result`
- implement `internal/tui/` Bubble Tea `Model`:
  - single struct implementing `Init`/`Update`/`View`
  - `mode` enum (`modeBoard` | `modeSearch`)
  - state fields: cursor index, search query, `highlightedPromptID` (anchor
    across filter changes), scroll offset, inline error, pending-clipboard
    flag, terminal dimensions from `tea.WindowSizeMsg`
  - `Update` is a pure `(Model, Msg) → (Model, Cmd)` — no hidden I/O, all
    external work via `tea.Cmd`
  - board-mode keys: `Esc`/`Ctrl+C` cancel, `Enter` selects highlighted,
    `↑`/`↓` scroll+cursor, `/` enter search, `P` dispatch clipboard-read
    `tea.Cmd`; any other printable key matched case-insensitively against the
    board's assigned keys
  - search-mode keys: `Esc` return to board (clear query), `Enter` select
    highlighted match, `Ctrl+C` cancel, `Backspace`/`↑`/`↓` edit/navigate;
    every other keystroke appends to query and triggers a re-filter
  - letter keybinds render lowercase in the `[key]` column; digits/symbols
    render as-declared. Matching uses `unicode.ToLower` on both sides for
    letters, literal for non-letters
  - inline errors cleared on real action (selection, mode switch, clipboard
    retry, cancel); navigation keys preserve them
- implement `searchIndex` deep module in `internal/tui/`:
  - constructor takes board rows + overflow rows + clipboard row
  - builds four corpuses (`ids`, `titles`, `descriptions`, `tags`) with back-
    references to the source rows
  - `Query(q string)` returns `[]MatchedRow`:
    - empty query → full alphabetical catalog (board + overflow + clipboard
      row first)
    - non-empty → one `sahilm/fuzzy.Find` per corpus, weighted merge
      (id×1.0, title×0.75, description×0.5, tags×0.25), summed per row,
      sorted descending with alphabetical-by-id tiebreak; clipboard row is
      excluded (no searchable text)
  - `sahilm/fuzzy` coupling fully contained inside this module
- implement highlight anchoring: before re-filter on query change, capture
  current `highlightedPromptID`; after filter, set cursor to that ID's new
  index or 0 if gone
- rendering (`View`) with lipgloss:
  - three-column board rows (`[key]  id  description`), soft-truncated
    description to terminal width with ellipsis, never wrapped
  - clipboard row pinned first on board with resolved reserved key (default
    `[p]`) and hint text `(read on select)`
  - board footer `[/ search (N more)]  [Esc cancel]`, omitting `(N more)` when
    no overflow
  - search footer `/query  [Esc exit search]  [Enter select]  [N matches]`
  - error view prepends the inline error text to the mode-appropriate footer
  - empty-store board shows only the clipboard row plus footer hint
    `no prompts found — press P for clipboard or Esc to exit`
  - fixed viewport scroll with `↑`/`↓`; no scrollbar, chevrons, or position
    indicator
- clipboard read command:
  - on `P`: return a `tea.Cmd` that calls the injected `clipboard.Reader` and
    runs `clipboard.Validate(content, cfg.MaxPasteBytes)`
  - result message populates `Result.ClipboardBody` and fires the submit
    `tea.Cmd` followed by `tea.Quit`, or populates inline error and stays open
- prompt selection:
  - on matched keypress (board) or `Enter` (search): resolve prompt from
    store, pre-check `len(body) ≤ cfg.MaxPasteBytes` → set inline error if
    oversize (stay open); otherwise fire submit `tea.Cmd` then `tea.Quit`
- tests:
  - unit tests on `searchIndex`: empty-query catalog; per-field ranking
    precedence; id-match-beats-title-match ordering; alphabetical tiebreak;
    no-match empty result; summed multi-field scores
  - pure-function `Update` tests: every board keypress class, search mode
    entry/exit, letter-typing into query, `Esc`/`Ctrl+C` cancel, case-
    insensitive keybind match, oversize-prompt inline error, empty-clipboard
    inline error, error-persists-on-navigation, error-cleared-on-action,
    highlight anchoring across query edits, scroll bounds, empty-store state
  - no golden-file or teatest coverage in Phase 5b; layout regressions
    surface via manual testing or a later phase

Read first:

- `docs/commands/tui-flow.md`
- `docs/commands/tui.md`
- `docs/tmux/verification.md`
- `examples/tmux-bindings.md`
- `docs/implementation/interfaces.md`

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

- improve `doctor` (add a daemon-reachability check now that the socket is real)
- improve help text
- improve prompt list/show formatting (show resolved keybind)
- support configurable picker command if desired (for `tprompt pick` only)
- refine logs and daemon status output
- **CLI auto-start of the daemon** (deferred from Phase 4): when the TUI flow
  or `daemon status` finds the socket unreachable, optionally spawn the
  daemon and retry. Behavior, opt-in flag, and PID-file/lock semantics to be
  designed here, not in MVP.
- **`daemon stop` subcommand** (deferred from Phase 4): only if SIGTERM
  proves insufficient in practice. Would dial the socket, send a shutdown
  request, and wait for graceful exit.
- **Post-injection capture-pane verification** (deferred from Phase 4):
  optional per `docs/tmux/verification.md`. Capture pane tail before/after
  delivery and log a warning if the tail is unchanged. Adds noise without
  proving semantic success, so only land this if it earns its keep in real
  use.

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
