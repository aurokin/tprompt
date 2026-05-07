# Testing Harness

`tprompt` is a tmux-first tool, but most behavior should be provable without a
live tmux session. The harness is built around small seams: store, config,
keybind resolution, sanitizer, clipboard reader, tmux runner, daemon client,
daemon queue, submitter, and TUI model.

Good tests assert public behavior and failure contracts. Avoid tests that only
mirror private helper structure.

## Harness Engineering Principles

- Start with the public contract: command output, exit code, daemon response,
  tmux command shape, rendered TUI behavior, or stored prompt metadata.
- Prefer the narrowest seam that proves the contract: pure unit test, fake
  runner/client, Unix socket round trip, testscript, then live tmux smoke.
- Keep planning artifacts out of the repo. Put PRDs and issue splits in Linear;
  keep repo docs focused on contracts, invariants, interfaces, and proof.
- When a contract changes, update three things together: the user-facing doc,
  the implementation seam, and the proof surface.
- Do not assert private helper structure unless the helper is the seam being
  intentionally hardened.

## Health Gates

Focused local iteration:

```bash
go test ./internal/<pkg>/
```

Broad local gate when tmux state is disposable:

```bash
go test ./...
```

Full project gate when tools are installed:

```bash
make check
```

`make check` runs formatting, linting, and the race-enabled test target from
the `Makefile`.

Testscripts in `cmd/tprompt/testdata/script/*.txtar` execute real `tmux`.
Use package-scoped tests while iterating, and avoid broad `go test ./...` runs
from a shell whose tmux state matters unless that state is disposable.

## Test Selection Ladder

1. Pure unit test for parsing, resolution, sanitization, ranking, and policy.
2. Fake dependency test for command construction, app orchestration, and error mapping.
3. Unix socket or temp-filesystem integration test for daemon/store behavior.
4. `testscript` for black-box CLI behavior and exit codes.
5. Manual/live tmux smoke only when real tmux focus or popup timing is the contract.

## Proof Surface By Subsystem

### Prompt Store

Proof surface: unit tests over temporary prompt directories and fixture files.

Assert:

- recursive discovery of markdown files
- filename-stem ID derivation
- duplicate stem detection with useful paths
- body extraction with and without frontmatter
- unsupported file extensions ignored
- supported frontmatter fields parsed
- invalid prompt delivery defaults rejected
- metadata escape stripping preserves body bytes (after the parser's single
  leading and single trailing line-break trim — see
  docs/storage/prompt-store.md "Body trimming")
- resolved prompt and list data are returned as cloned values

### Config

Proof surface: unit tests over config structs and temporary config files.

Assert:

- defaults match documented behavior
- config files decode expected TOML shape
- unknown or invalid fields fail clearly
- `reserved_keys` accepts printable and symbolic values
- disabled reserved keys are respected
- `keybind_pool` is deduplicated and filtered against reserved keys
- clipboard and picker command strings parse into argv
- delivery precedence is flags, frontmatter, config, defaults

### Keybind Resolver

Proof surface: pure unit tests.

Assert:

- frontmatter keys take precedence over auto-assignment
- auto-assignment scans prompts alphabetically by ID
- the configured pool is consumed in order
- prompts beyond the pool become overflow
- duplicate keys are case-insensitive errors
- reserved-key collisions are errors
- malformed keys are errors

### Clipboard Reader

Proof surface: unit tests with injected environment and look-path seams, plus
command-reader tests with controlled commands.

Assert:

- macOS selects `pbpaste`
- Wayland selects `wl-paste` when available
- X11 prefers `xclip` and falls back to `xsel`
- missing reader reports install guidance
- override commands are used verbatim
- non-zero command exits surface stderr
- empty, non-UTF-8, and oversized content is rejected before delivery

### Sanitizer

Proof surface: unit tests over byte fixtures.

Fixture corpus should include:

- OSC title and clipboard-write sequences
- DCS sequences
- CSI private mode toggles
- application keypad mode
- cosmetic SGR colors
- cursor movement and erase sequences
- malformed escape-adjacent input
- multi-byte UTF-8 adjacent to escape sequences
- content without escape sequences

Assert every relevant class across `off`, `safe`, and `strict`:

- `off` is identity
- `safe` strips dangerous classes and preserves cosmetic classes
- `strict` rejects any escape sequence with class and byte offset

### Tmux Adapter

Proof surface: fake `Runner` tests. Live tmux is not required for command
construction.

Assert:

- paste mode constructs `load-buffer` and `paste-buffer -d -p`
- paste mode with `--enter` sends Enter after `paste-buffer`
- type mode uses `send-keys -l -- <chunk>`
- type chunks split on rune boundaries
- pane-exists and selected-pane probes map tmux output correctly
- `display-message` uses client scope when available and target fallback otherwise
- tmux runner failures map into the correct error taxonomy
- size-cap rejection happens before tmux commands are invoked

### Daemon

Proof surface: unit tests for queue, executor, verifier, logger, and validation;
Unix-socket integration tests for server/client round trips.

Assert:

- job validation rejects invalid shape and preserves valid fields
- `source = clipboard` carries captured bytes and no prompt ID
- same-pane replacement cancels or drops the older pending job as specified
- different-pane jobs can proceed independently
- verification waits on tmux selection state and respects timeout/cancellation
- executor checks size, sanitizes, and delivers in the documented order
- failures are logged without prompt bodies or clipboard bytes
- socket permissions and stale-socket behavior are correct
- status responses expose pid, socket, log, uptime, pending jobs, and version

### Daemon lifecycle and TUI auto-start

Proof surface: unit tests for the launcher seams and primitives, integration
tests for the macOS trust gate, and testscript end-to-end for the CLI flows.

Assert (`internal/daemon/lifecycle/`):

- run lock primitive holds during daemon lifetime and releases on Close
- run lock probe is non-mutating (does not create the lock file)
- start lock primitive serializes concurrent acquirers
- identity sidecar atomic write + ownership match (pid, start_time)
- cooldown sidecar atomic write, expiry semantics, idempotent clear
- `PathsFor` canonicalizes via `filepath.Abs` + `Clean`
- `Server.Listen` acquires the run lock, drops stale sidecars under the
  documented four-cell matrix, binds, then writes the identity
- `Server.Close` removes identity-if-owned then releases the run lock,
  and a fresh `Listen` on the same path then succeeds
- `Server.Listen` releases the run lock if a post-lock failure happens
- a competing `Listen` over a held run lock fails with
  `SocketUnavailableError`

Assert (`internal/app/lifecycle/`):

- `Launcher.Start` short-circuits on `ProbeOK` (already running)
- `ProbeReachableBroken` refuses to spawn (manual recovery message)
- spawn happens once under concurrent calls (in-process mutex +
  cross-process start lock)
- readiness timeout maps to `ReasonReadinessTimeout` with daemon log
  path in detail
- child early-exit (kill(pid, 0) -> ESRCH) maps to
  `ReasonChildExitedEarly` without burning the full readiness budget
- explicit intent: cooldown and trust gate both bypassed
- implicit intent: failure records cooldown; subsequent implicit
  start is gated until expiry
- pre-spawn diagnostic includes `parent_pid`, `intent`, `exec`,
  `socket`, lifecycle paths, `log`, `config`, and trust verdict

Assert (`internal/app/lifecycle/trust_darwin*`):

- ad-hoc detection via both `Signature=adhoc` and `flags=...adhoc...`
- invalid-signature reject (codesign --verify exit non-zero)
- Gatekeeper reject for non-CLI rejection
- CLI-bypass for "the code is valid but does not seem to be an app"
- override env var honors `1`/`true`/`yes`/`on`; ignores other values
- override short-circuits before any `codesign`/`spctl` invocation
- tools-missing fails closed with "trust tools unavailable"
- integration tests exercise real `/usr/bin/git` (Allow) and a
  freshly-built ad-hoc clang binary (RejectAdHoc), guarded with skips

Assert (testscripts in `cmd/tprompt/testdata/script/`):

- `daemon_start_stop_roundtrip.txtar` — explicit start spawns, status
  succeeds, stop tears down, post-stop status fails
- `daemon_start_idempotent_when_running.txtar` — repeat starts
  short-circuit; daemon log retains exactly one `outcome=started`
- `daemon_run_collision_with_existing.txtar` — second `daemon run`
  fails with daemon/IPC and the run-lock primitive's wording
- `tui_auto_start_cold_start.txtar` — TUI cold start spawns daemon
  via launcher (with override env to bypass ad-hoc gate on test binary)
- `tui_auto_start_warm_reuse.txtar` — TUI invocations against a
  running daemon reuse it (one `outcome=started` only)

Locked decisions: see [DECISIONS.md §33](../../DECISIONS.md). Narrative:
[docs/lifecycle/auto-start.md](../lifecycle/auto-start.md).

### TUI

Proof surface: pure model/update/view tests. Avoid brittle terminal snapshot
tests unless a specific rendering regression demands them.

Assert:

- board rows render key, id, and display description
- description falls back to title, then blank
- descriptions truncate to terminal width without wrapping
- clipboard row appears first when enabled
- reserved keys render in footer hints
- `/` enters search
- search covers board and overflow prompts, but not body content
- search ranks ID above title, title above description, and description above tags
- cursor and scroll offsets stay in bounds
- `Esc` and configured cancel keys exit with cancel result
- prompt keypresses submit the correct prompt
- clipboard keypress reads and validates clipboard content asynchronously
- inline errors persist or clear according to user-visible action rules

### CLI And App Layer

Proof surface: app-level tests with fake deps plus `testscript` for black-box
command behavior.

Assert:

- command registration and help surface are stable
- config load failures map to usage/config errors
- prompt discovery failures map to prompt errors
- `list`, `show`, `send`, `paste`, `pick`, `tui`, `doctor`, and daemon commands
  expose the documented stdout/stderr behavior
- cancellation exits with status 0
- direct sends do not require daemon state
- TUI preflight checks happen in the documented order

## Optional Live Tmux Checks

Live tmux tests are valuable but should stay opt-in unless the project promotes
them into the main gate. They are best suited for final confidence checks:

- create a disposable tmux pane
- submit a deferred job
- verify prompt text arrives after returning focus to the target pane
- verify bracketed paste preserves multiline content
- verify `--enter` sends exactly one Enter after paste
- intentionally close the target pane and confirm the failure path

## Manual Smoke Checklist

- Run `tprompt doctor`.
- Start the daemon.
- Open a tmux pane with a shell prompt.
- Launch the TUI through the documented popup binding.
- Select a prompt and confirm it lands after the TUI closes.
- Repeat with `tprompt paste`.
- Repeat with paste mode and type mode.
- Close the target pane before delivery and confirm failure is surfaced.
- Confirm success remains silent by default.
- Confirm TUI cancellation exits 0.
