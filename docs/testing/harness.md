# Testing Harness

`tprompt` is a tmux-first tool, but most behavior should be provable without a
live tmux session. The harness is built around small seams: store, config,
keybind resolution, sanitizer, clipboard reader, tmux runner, handoff client,
delivery executor, submitter, and TUI model.

Good tests assert public behavior and failure contracts. Avoid tests that only
mirror private helper structure.

## Harness Engineering Principles

- Start with the public contract: command output, exit code, handoff response,
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
3. Temp-filesystem integration test for handoff/store behavior.
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

### Delivery And Handoff

Proof surface: unit tests for executor, verifier, logger, and validation;
temp-filesystem tests for handoff job write/read/remove behavior.

Assert:

- job validation rejects invalid shape and preserves valid fields
- `source = clipboard` carries captured bytes and no prompt ID
- verification waits on tmux selection state and respects timeout/cancellation
- executor checks size, sanitizes, and delivers in the documented order
- failures are logged without prompt bodies or clipboard bytes
- handoff writes private job files at 0600 under a 0700 jobs directory
- handoff rejects malformed jobs before spawning or delivering
- handoff worker removes successfully processed jobs

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
- a set `State.Banner` renders as a header line in both board and search modes
  and reserves matching viewport height; no banner leaves viewport math unchanged

The fuzzy scorer itself lives in `internal/searchindex` (below), not in `tui`;
`tui`'s `search_index.go` is a thin adapter over it, and the board search tests
listed above run **unchanged** against that adapter — they are the behavior-
preservation guard for the extraction.

### Search Index (shared fuzzy core)

Proof surface: pure unit tests (`internal/searchindex/searchindex_test.go`),
generic over a sample record so the tests exercise the seam (`Fields` + `tieKey`)
the real callers use.
This is the dependency-free core both `internal/tui` and `internal/importtui` adapt
to; keeping its scoring pinned here means a ranking regression is caught in the core,
not only positionally in a caller.

Assert:

- empty query returns the full catalog in `tieKey` order (Score 0)
- a non-empty query with no fuzzy hit returns an empty (non-nil) result
- field-weight priority: an id match outranks a title match, title outranks
  description, description outranks tags (same raw fuzzy score)
- a multi-field match outscores a single-field match at the same best priority
- equal score + equal priority breaks ties by `tieKey` ascending (deterministic,
  never map-iteration order)
- the tags corpus is searchable
- the index carries the caller's own item type back out in `Match` (generic seam)

### CLI And App Layer

Proof surface: app-level tests with fake deps plus `testscript` for black-box
command behavior.

Assert:

- command registration and help surface are stable
- config load failures map to usage/config errors
- prompt discovery failures map to prompt errors
- `list`, `show`, `send`, `paste`, `pick`, `tui`, `import`, and `doctor`
  expose the documented stdout/stderr behavior
- cancellation exits with status 0
- direct sends do not require handoff state
- TUI preflight checks happen in the documented order
- `tui` without `--target-pane` enters direct mode only when `$TMUX_PANE` is
  confirmed in `tmux list-panes -a` (resolved before the config/store
  pre-flight); otherwise it exits with a usage error pointing at `tprompt init`

### Import (Wispr)

Proof surface: pure unit tests for the reader and mapping (`internal/wispr`),
app-level tests with a fake `Reader` (`internal/app/import_test.go`), write-free
plan/classification tests (`internal/app/import_plan_test.go`), the source-agnostic
seam and bare-import dispatch (`internal/app/import_source_test.go`,
`internal/app/root_test.go`), the interactive picker model (`internal/importtui`)
and its app wiring with a recording/stub renderer
(`internal/app/import_interactive_test.go`), exit-code mapping
(`internal/app/exit_test.go`), and `testscript` against a committed fixture
`flow.sqlite` (`cmd/tprompt/testdata/script/import_wispr_*.txtar`). The
binary fixture cannot live inline in a `.txtar`, so it is committed under
`cmd/tprompt/testdata/wispr/` (regenerated by `gen.go`) and exposed to scripts by
absolute path via `$WISPR_FIXTURE`.

The import subsystem consumes `ImportRecord` values and registers sources through
an `ImportSource` seam (interfaces.md). The seam is proved without the wispr type:
a test-only `fakeRecord`/`fakeImportSource` imports through the same engine
(`runImport`) — confirming a registry entry yields a working subcommand and that
disambiguation uses the record's own `Disambiguator()`. The bare-import dispatch
(`dispatchArgs`, DECISIONS §34) is a pure arg-rewrite unit-tested over its full
branch set.

`internal/importtui` is a sibling of `internal/tui` (neither imports the other);
its pure model/view tests mirror the board's — toggle/select-all/confirm/cancel
key handling, conflict-aware pre-check (fresh checked, exact-target skip-by-default,
cross-path non-selectable, a CLI-authorized refresh pre-armed), status-glyph
rendering (`[ ]`/`[x]`/`[=]`/`[!]`) and the cross-path blocker path, the footer
counter (`N selected · M overwrite · K blocked`), select-all reset semantics
(clears ad-hoc arms, keeps CLI-authorized refreshes), `/`-search filtering (over the
shared `internal/searchindex` core), a viewport regression (rows never wrap or
overflow the terminal height at any width, with the two-line footer), and label
sanitization (a verbatim snippet phrase with a newline or ANSI escape can neither
spill a row nor inject terminal control codes). The `-i` wiring is driven by
a stub renderer selected via `TPROMPT_TEST_IMPORT_RENDERER` (a test-only env,
mirroring `TPROMPT_TEST_RENDERER`) so the black-box testscript can run the picker
over pipes.

Assert:

- only live snippets are read (snippet rows that are not deleted); non-snippet and
  deleted rows are excluded
- the reader opens read-only and never mutates the DB or leaves a `-wal` sidecar
- snippet→prompt mapping: phrase → verbatim title + slug id, replacement → body
  (byte-for-byte through the trim contract), `isStarred` → `starred` tag, provenance
  tag (default `wispr`, `--tag` override)
- id minting is deterministic: empty-slug phrase → `wispr-<uuid8>`; intra-batch
  collisions get a `-<uuid6>` suffix extended with a counter, so no snippet is dropped
- empty/NULL replacement is skipped without writing a hidden file
- skip-existing keeps re-runs idempotent (a stat per snippet, no store walk);
  a same-id prompt at another path is a §4 hard error; `--overwrite` refreshes
- `--dry-run` previews the plan and writes nothing; `--project` targets the overlay
- the write-free classifier (`classifySnippet`/`dryRunPlan`, the seam the import TUI
  renders) matches the scriptable writer: the three §34 cases classify correctly,
  intra-batch claim-ordering holds (an empty-body snippet claims no slug), dry-run
  still aborts on a cross-path duplicate, and partial progress is flushed before an abort
- interactive `-i`: confirm-all is byte-for-byte equal to a non-interactive import (pinned
  by testscript + app test); the picker shows fresh rows and conflict rows (exact-target and
  cross-path, the latter carrying its conflicting path); deselecting honors the disambiguated
  id (shown id == written id); a cross-path duplicate is shown but non-selectable and, when
  confirmed around, writes nothing without aborting, while a genuine classify error
  (unreadable collision-scan subtree) still surfaces; cancel / deselect-all / zero-fresh write
  nothing and create no directory; `-i` without a tty and `-i --dry-run` map to usage errors
  (exit 2)
- per-item overwrite (`-i`): arming an exact-target conflict routes through the existing
  overwrite write-path (refreshes the file, exact-target only); a re-classification unit test
  pins armed exact-target → `planImportable` with `overwrite=true` (not `planExists`); a
  *forced* cross-path overwrite still yields the §4 hard error (exit 3) with nothing written
  at the exact target (writer stays authoritative); an idempotent re-run defaults to all-skip
- interactive `/`-search (`-i`): `/` enters search and filters rows by id, title, and tags
  (a `starred`-only hit proves the tags corpus is wired via `pickerItems`→`wispr.Snippet.Tags`);
  in search mode every printable rune — `a`/`j`/`k`/space — is query text, not an action;
  selection is keyed by id and survives entering/typing/clearing/leaving search; footer counts
  and `write N prompts?` stay global while `Space`/`a` act on the visible set; the
  overwrite-safety invariant holds — filtering an armed *ad-hoc* overwrite out of view disarms
  it (so a bare confirm cannot execute a hidden overwrite), while a hidden CLI-authorized
  refresh survives; a committed filter is visible in the list header and `Esc` clears it before
  it cancels; the search view honors the viewport (re-clamps as the cursor moves past the frame)
- DB error taxonomy maps to exit codes: missing DB / no default location →
  usage (2); unreadable/garbage/locked DB → general (1)
- the awkward-path DSN survives spaces and URI metacharacters in the db path
- no prompt body or replacement content appears in summary/log output
- the source seam is source-neutral: a non-wispr `fakeImportSource`/`fakeRecord`
  imports through the shared engine, and two records minting the same id are
  disambiguated by the record's own `Disambiguator()` (collision policy names no
  source type); the registry's default source is `wispr` and wires the `import
  wispr` subcommand
- bare-import dispatch (`dispatchArgs`): inside tmux + interactive tty, bare
  `import` rewrites to `import wispr -i`, with root flags surviving in any position
  (`--config x import`, `import --config x`, even `--config import import`); it does
  **not** rewrite a named source (`import wispr`), a source flag (`import
  --dry-run`), a stray positional (`import bogus`/`import import`), a dangling
  value-taking root flag (`import --config`), an empty default-source registry, or
  a non-interactive stream (redirected stdout / injected non-tty); outside tmux or
  without a tty it is left to print help

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
- Open a tmux pane with a shell prompt.
- Launch the TUI through the documented popup binding.
- Select a prompt and confirm it lands after the TUI closes.
- Repeat with `tprompt paste`.
- Repeat with paste mode and type mode.
- Close the target pane before delivery and confirm failure is surfaced.
- Confirm success remains silent by default.
- Confirm TUI cancellation exits 0.
