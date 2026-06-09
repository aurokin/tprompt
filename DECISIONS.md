# Locked Decisions

These decisions are already made and should be treated as constraints unless the user explicitly reopens them.

## Terminology

- **tmux popup** — a floating window created by `tmux display-popup`. Tmux's feature.
- **TUI** — tprompt's built-in interactive terminal UI (the `tprompt tui` subcommand). Runs inside any terminal context; typically a tmux popup.
- **TUI flow** — the end-to-end path from TUI selection through handoff-worker-verified injection into the target pane.

## Product

### 1. `tprompt` is both interactive and non-interactive

It must support:

- direct send by ID
- interactive prompt selection
- tmux-popup-first workflows (TUI launched from a tmux popup)

### 2. The source of truth is markdown files on disk

Prompts live as markdown files in a configured prompts directory.

### 3. Prompt ID is the file name stem

This is locked.

Examples:

- `code-review.md` -> `code-review`
- `bug-hunt.md` -> `bug-hunt`
- `agents/deep-review.md` -> `deep-review`

Directories do **not** contribute to the ID.

### 4. Duplicate IDs are invalid

Because directories do not namespace IDs, the following is invalid:

- `agents/code-review.md`
- `reviews/code-review.md`

Both produce the same ID: `code-review`

The tool must detect this and fail clearly.

### 5. Tmux-popup workflow is first-class

The tool should be designed around reliable use when the TUI is launched from a tmux popup, not treat that launch path as a bolt-on extra.

### 6. Deferred send must use a short-lived handoff worker

The TUI process should not sleep and then try to inject directly. It should write a private handoff job, spawn a short-lived worker, then exit. The worker owns verified delivery after tmux returns focus to the target pane. The TUI path must not require a long-running daemon or implicit daemon auto-start.

### 7. Verification must use tmux state, not timers

The deferred delivery worker should only inject after confirming that the original target pane is available and back in the intended active state.

### 8. Delivery modes

Two delivery modes are required:

- `paste` (default)
- `type` (fallback)

### 9. Prompt body is what gets injected

If markdown files include YAML frontmatter, only the markdown body is injected. Frontmatter is metadata only.

The parser strips one leading and one trailing line break from the body so the
canonical layout (a blank line after the closing fence, a POSIX EOF newline)
does not introduce extra blank lines at paste time. See
[docs/storage/prompt-store.md](docs/storage/prompt-store.md) "Body trimming".

### 10. `tprompt` is tmux-first

Outside-tmux support is not part of the current product contract.

## Rationale for filename-stem IDs

This makes prompt invocation fast and memorable.

Benefits:

- short IDs
- easy shell usage
- easy TUI selection
- easier to remember than path-based IDs

Tradeoff:

- duplicate filenames become invalid across the whole prompt store

This tradeoff is accepted for the current product contract.

## Required duplicate-ID behavior

On startup, list, send, or doctor, the tool should detect collisions and return a clear error with all conflicting paths.

Example:

```text
Duplicate prompt ID detected: code-review
- /home/user/.config/tprompt/prompts/agents/code-review.md
- /home/user/.config/tprompt/prompts/reviews/code-review.md
```

Implementations must not silently disambiguate duplicate IDs.

## Command And Interaction Locks

These decisions extend the product contract. They are constraints, not suggestions.

### 11. Clipboard is a separate command, not a flag

A dedicated top-level command `tprompt paste` reads the host clipboard and delivers it. Clipboard content is **not** a prompt ID and does **not** appear in `tprompt send`.

Flag surface mirrors `send` for uniformity: `--target-pane`, `--mode paste|type`, `--enter`, `--sanitize strict|safe|off`.

### 12. Delivery mechanism is bracketed paste by default

Default paste mode is implemented as:

- `tmux load-buffer -b <name> -` (content fed via stdin)
- `tmux paste-buffer -d -p -b <name> -t <target>` (`-p` enables bracketed paste; `-d` deletes the buffer after)

Fallback `type` mode uses `tmux send-keys -l -- "<body>"` with chunking for large payloads.

### 13. No trailing Enter by default

Default behavior is **no** automatic Enter. Users finish and submit themselves. `--enter` is an opt-in that fires Enter as a separate `send-keys` call **outside** the bracketed-paste wrapper.

### 14. Same-host scope only

Clipboard reader, handoff worker, and tmux pane all run on the same host. Cross-host clipboard (laptop → remote via OSC-52 read or similar) is an explicit non-goal.

### 15. TUI is built-in and first-class

`tprompt tui` launches a built-in interactive TUI, not an external picker. It features:

- a keybind "board" of single-keypress shortcuts for pinned prompts
- a pinned clipboard row (default keybind `P`)
- `/` for fuzzy search over `id + title + description + tags`
- `Esc` to cancel (exit 0)
- `Enter` to select the highlighted row

`picker_command` config is kept but only affects the separate `tprompt pick` scripting command.

### 16. Keybinds are declared in frontmatter

Frontmatter `key:` field assigns a single printable character to a prompt. Rules:

- **Case-insensitive** — `c` and `C` are the same key.
- **Any single printable character** is allowed (not restricted to the auto-assign pool).
- **Duplicate `key:` across prompts** is a **hard error**, same strictness as duplicate IDs.
- **Collision with a reserved key** (`P`, `/`, `Esc`, `Enter`) is a **hard error**.
- **Malformed key** (multi-char, empty, symbolic like `ctrl+x`) is a **hard error**.
- Modifier-key combinations are **not supported**.

### 17. Auto-assign pool for unbound prompts

Prompts without a frontmatter `key:` receive one from this pool in order:

```
1 2 3 4 5 q e r f g t z x c
```

Prompts are scanned **alphabetically by `id`** to assign from this pool. Once the pool is exhausted, remaining prompts are **overflow** — they are not shown on the board and are reachable only via `/`-search.

### 18. Project overlays and shadowing

Project prompt overlays are discovered by walking up from the current working
directory toward the git root. A `tprompt/` directory inside a git tree
activates a project source; reaching `.git` first means no overlay is active.
The walk stops at the user's home directory or filesystem root, so a stray
`~/tprompt` folder is never treated as project scope.

When a prompt ID exists in both the global and project tiers,
`prompt_priority` selects the winner. The default is `global` so adopting a
project overlay does not silently replace existing muscle-memory prompts.
`project` is an explicit opt-in. The losing prompt is shadowed, not deleted:
it appears in `tprompt list`, is reported by `tprompt show <id>` on the
winner, and remains selectable through TUI search. Shadowed prompts receive no
board key.

Cross-tier collisions are the only collisions resolved by `prompt_priority`.
Duplicate IDs **within** a single tier — two global sources that both expose
`code-review`, or two files inside the project overlay with the same stem —
remain hard errors per §4. The multi-source model only relaxes the original
single-source rule across tiers; it never silently picks between two prompts
of the same scope.

### 19. Reserved keys are reconfigurable

Default reserved keys are `P` (clipboard), `/` (search), `Esc` (cancel), `Enter` (select). All are overridable via `config.toml`.

### 20. TUI row layout

Rows are rendered as three columns: `[key]  id  description`.

- Description is **soft-truncated** with ellipsis to fit terminal width — never wrapped.
- When `description` is absent, fall through: `description → title → blank`.
- No body preview in rows.

### 21. Clipboard read on keypress, no preview

When the TUI opens, the clipboard is **not** read. It is read only when the user presses the clipboard key (`P` by default).

The TUI process reads the clipboard, captures the content, and submits it as part of the handoff job payload before exiting. Deferred workers never read the clipboard themselves.

### 22. Clipboard edge cases fail loudly

- **Empty clipboard** → inline error in TUI; TUI stays open.
- **Non-UTF-8 / binary clipboard** → reject with clear error.
- **Oversized clipboard** → hard cap via `max_paste_bytes` config; reject when over.

### 23. Clipboard reader is auto-detected, with override

The reader is chosen at runtime from platform/env signals:

- macOS → `pbpaste`
- Linux Wayland → `wl-paste`
- Linux X11 → `xclip` or `xsel`

Users can override via `clipboard_read_command` in `config.toml`. `doctor` reports which reader was chosen and whether it is installed.

### 24. Sanitization defaults to safe

`sanitize = "strict" | "safe" | "off"`, default `safe`. The rule applies uniformly to `tprompt paste` and `tprompt send <id>`. Both `strict` and `safe` require tested implementations before release.

Originally defaulted to `off` on a fail-open posture: assume the user has authored their own prompts and pasted their own clipboards, no sanitization needed. Revisited after AUR-162 demonstrated that an embedded bracketed-paste end terminator (`ESC[201~`) escapes the delivery wrapper and lets subsequent bytes execute as raw keystrokes — a real footgun, not a theoretical one. AUR-161 flipped the default so the standard install ships with the dangerous-class denylist active. `off` remains available for users who legitimately need raw passthrough.

### 25. Search is fuzzy, scope-limited

Search uses fuzzy (fzf-style) matching over `id + title + description + tags`, ranked id-first. Body content is **not** searched.

### 26. Error feedback is in-tmux plus logs

Deferred-job failures surface in two channels:

- `tmux display-message` banner on the originating client at the moment of failure
- append-only delivery log at `~/.local/state/tprompt/delivery.log`

No success banner by default.

### 27. Pending jobs are replaced, not queued

When a new deferred job arrives for the **same target pane** as one already pending, the new job **replaces** the old one. Matches "I changed my mind" intent. Different targets are independent.

### 28. TUI singletons are not enforced

Multiple TUI instances (including multiple tmux popups) can be open simultaneously — across clients or even on the same client. The simpler "any TUI may submit a job" rule applies unless a future Linear-scoped change introduces singleton behavior.

### 29. Direct sends bypass deferred handoff entirely

`tprompt send <id>` and `tprompt paste` (invoked outside the TUI flow) always deliver synchronously through the tmux adapter. They do not write handoff jobs and cannot be affected by pending TUI jobs.

### 30. Bare `tprompt` defaults to `tprompt tui` in tmux + tty

When invoked with no subcommand, `tprompt` dispatches to `tprompt tui` if stdin is a tty **and** `$TMUX` is set, so users can write `tprompt --target-pane '#{pane_id}' ...` in their binding instead of `tprompt tui --target-pane '#{pane_id}' ...`. Outside tmux (or without a tty), it prints help, preserving the no-args → usage convention in a regular shell. The TUI is the signature workflow, so this default matches user intent when the environment supports it.

The dispatch is a pure arg rewrite at the top of `RunCLI`; cobra then parses flags for `tui`. `--target-pane` is **not** a hard-required flag (AUR-446): when it is omitted, `tui` falls back to **direct mode** and delivers to the current pane, showing a banner that nudges popup setup. Direct mode is gated on confidence — it triggers only when `$TMUX_PANE` resolves **and** appears in `tmux list-panes -a`. This keeps §30's original popup rationale intact: inside a `display-popup -E` popup, `$TMUX_PANE` is the popup's own pane, not the originating one, and a popup pane is absent from `list-panes -a`, so direct mode refuses there. On any uncertainty (no `$TMUX_PANE`, pane not listed, query error) bare `tprompt` still exits 2 — but now with a clear usage error pointing at `tprompt init`, not a generic required-flag message. The canonical popup binding passes `#{pane_id}` explicitly (see `examples/tmux-bindings.md`), which bypasses direct mode entirely so delivery targets the correct pane.

### 31. Default global prompts directory with auto-create

`prompts_dir` is optional. When unset, it resolves to
`$XDG_CONFIG_HOME/tprompt/prompts` (falling back to
`~/.config/tprompt/prompts` when XDG is unset). The default directory is
auto-created on first access, so a fresh install runs end-to-end without
hand-editing config.

An explicit `prompts_dir` is used verbatim and is **not** auto-created — a
missing explicit path remains a `PromptsDirMissingError`. This preserves the
existing contract for users who already point at a custom path.

Resolution is owned by a pure path-resolver module
(`internal/promptsource`) over (config, env getter, home directory). Later
slices extend it with `additional_prompts_dirs` and a project overlay; the
auto-create asymmetry stays — only the resolved primary global default is
created on access.

### 32. Implementation tech stack

The implementation language and toolchain are locked for v1:

- **Language:** Go 1.26 — single static binary, low startup latency (matters for popup-launched TUI), idiomatic subprocess handling, mature TUI ecosystem.
- **CLI framework:** `github.com/spf13/cobra`.
- **TUI framework:** `github.com/charmbracelet/bubbletea` + `github.com/charmbracelet/lipgloss`.
- **Config parsing:** `github.com/BurntSushi/toml`. Frontmatter parsing: `gopkg.in/yaml.v3`.
- **Format:** `gofumpt` + `goimports`, run via `golangci-lint v2 fmt`.
- **Lint:** `golangci-lint v2` with `govet`, `staticcheck`, `errcheck`, `revive`, `ineffassign`, `unused`, `gosec`, `misspell`, `nolintlint`.
- **Complexity:** `gocognit` + `funlen`, configured through `golangci-lint`.
- **Testing:** stdlib `testing` + `github.com/google/go-cmp` for diffs + `github.com/rogpeppe/go-internal/testscript` for CLI black-box tests. Coverage via `go test -covermode=atomic`.

### 33. No daemon runtime

`tprompt` has no user-facing daemon or long-running background service. The
normal command surface is a single CLI binary:

- `tprompt send <id>` and `tprompt paste` deliver synchronously through tmux.
- `tprompt tui` writes a private handoff job and spawns
  `tprompt handoff --job <path>` as a detached, short-lived worker.
- `tprompt doctor` checks local config and tool availability; it does not probe
  a daemon socket.

The old daemon lifecycle (`daemon start`, `daemon run`, `daemon status`,
`daemon stop`) is not part of the product contract. Signing and notarization
apply to the CLI binary only, not to detached daemon operation.

Rationale and library choices are detailed in `docs/implementation/tech-stack.md`.

### 34. Wispr Flow snippet import

`tprompt import wispr` ingests local Wispr Flow snippets as ordinary prompt
files. The contract is locked:

- **Driver.** Wispr's `flow.sqlite` is read with `modernc.org/sqlite`, a pure-Go
  (CGO-free) driver, so the import path keeps §32's single-static-binary,
  no-toolchain-C property. The dependency is **isolated to `internal/wispr`**;
  no other package imports a SQL driver.
- **Read-only, snippets-only.** The database is opened `mode=ro` and is **never**
  written — tprompt is not a Wispr management tool. It reads **only** snippet rows
  (`Dictionary` where the snippet flag is set and the deleted flag is not); it
  never reads dictation history. The open must not leave a `-wal`/`-shm` sidecar.
- **Mapping.** Per snippet: `phrase` → `title:` (verbatim) **and** the slug used
  for the filename id; `replacement` → the markdown body (byte-for-byte through
  the [§9 trim contract](#9-prompt-body-is-what-gets-injected), so a second source
  must not assume verbatim bytes); a starred snippet appends a `starred` tag. Every prompt
  carries a provenance tag (`wispr` by default, `--tag` overrides). Imported
  prompts carry the **full `tprompt new` frontmatter field set** (`title,
  description, tags, key, mode, enter`, in scaffold order) so an imported prompt
  is as editable as a scaffolded one; only `title`/`tags` are populated and the
  rest are empty stubs. The snippet-controlled fields (`title`/`tags`) are
  YAML-marshaled — never string-templated — so a `:`/quote in a phrase cannot
  corrupt the file; the empty fields are emitted as bare keys (matching the
  scaffold, and avoiding an `enter: ""` that would not parse back into
  promptmeta's `*bool` enter field). The `new`↔import field set is held in
  lockstep by a parity test (`internal/app`), not a shared abstraction.
- **Id minting is generation-time and deterministic.** The id is `slugify(phrase)`.
  A phrase that slugifies to empty falls back to `wispr-<first-8 of the snippet
  uuid>`. When two snippets in one run mint the same id, the first keeps the bare
  slug and each later one gets a `-<first-6 of its uuid>` suffix (extended with an
  incrementing counter in the rare case two suffixes also collide), so the in-batch
  id is always unique and no snippet is dropped because of a collision with another
  snippet in the same run, nor written as a hidden/undiscoverable file. Minting
  consults only the in-batch ids, never the on-disk store — a minted id that
  happens to match a prompt already on disk is governed by skip-existing below, not
  re-suffixed, because re-suffixing would make ids depend on filesystem state and
  break idempotent re-runs. Order is stable (snippets are read `ORDER BY id`).
- **Skip-existing is a write-refusal, not a §4/§18 relaxation.** A snippet whose id
  already exists at the **exact target path** is skipped so re-runs stay idempotent;
  this is the importer declining to write, **not** a softening of the duplicate-ID
  hard error. The collision guard prevents *creating* a duplicate: when a snippet
  *would* be written (the exact target is absent, or `--overwrite`) and a same-id
  prompt exists at **another path in the same scope** (a subdirectory or another
  global source), the write is refused as a §4/§18 **hard error** (exit 3) —
  `--overwrite` cannot resolve a duplicate in place. A duplicate that *already*
  coexists with the exact target is a pre-existing store-level §4 violation surfaced
  by `list`/`send`/`doctor`, not re-detected on import's no-op skip (which keeps
  idempotent re-runs a stat per snippet, not a full store walk). `--overwrite` is
  the explicit opt-in to refresh an existing prompt from Wispr; `--dry-run` previews
  without writing.
- **Interactive conflict review & per-item overwrite (`-i`).** The `-i` picker
  surfaces the §34 collision tuple per row — fresh, **exact-target**, and
  **cross-path** — so a conflict is reviewed before any write (the per-row glyphs,
  keys, and footer live in
  [docs/commands/cli.md](docs/commands/cli.md#tprompt-import-wispr); the guarantees
  in [EXPECTATIONS.md](EXPECTATIONS.md#import)). Per-item overwrite **widens the
  contract surface but not the policy**: it is finer than the all-or-nothing
  `--overwrite` flag, yet it is **exact-target-only** and routes through the *same*
  `writePromptContent` overwrite path. The writer stays authoritative — arming an
  id whose write would create a cross-path duplicate is still refused as the §4/§18
  hard error (exit 3); the TUI can surface the tuple but can neither invent nor
  weaken policy. Skip-by-default holds in
  the picker: an unarmed exact-target is skipped, so an idempotent re-run defaults to
  all-skip. `-i --overwrite` stays allowed (the flag is the all-or-nothing opt-in and
  pre-arms every refresh, shown checked); `-i --dry-run` and a non-tty `-i` are usage
  errors (exit 2). The race-safety from the non-interactive path carries over: a
  *non-armed* row that races to a conflict while the picker is open is skipped (never a
  surprise hard error), while an *explicitly armed* overwrite that resolves to a
  cross-path surfaces the exit-3 — only deliberate overwrite intent escalates.
- **Interactive `/`-search over a shared fuzzy core.** The `-i` picker has a `/`
  fuzzy filter over each snippet's id, title, and tags (so `starred` finds starred
  snippets) for large libraries. The scorer is **not forked** from the board TUI:
  it lives in a dependency-free `internal/searchindex` package (generic over the
  caller's row via `Fields` + a `tieKey`; `sahilm/fuzzy` confined there), which the
  board (`internal/tui`) and the import picker (`internal/importtui`) both adapt to.
  This fulfills the "promote a shared core" path anticipated when `importtui` was
  made a dependency-free sibling of `tui`: the picker gains search **without**
  importing `tui` (which would drag in store/clipboard/config), so its isolation
  contract holds. The board's clip-row pinning and `(PromptID, Scope)` tiebreak are
  preserved exactly by a thin adapter (zero behavior change; existing `tui` search
  tests are the regression guard). Search is a **view filter, not a selection
  mutation**: the selection set and the `write N prompts?` count stay **global**
  (over all snippets) while `Space`/`a` act on the **visible** rows. The one
  overwrite-safety invariant is that an ad-hoc per-item overwrite is armed only
  while its row is **visible** — filtering an armed exact-target out of view
  disarms it, so a filter can never strand a surprise destructive overwrite (the
  global overwrite count truthfully drops to 0). A CLI `--overwrite` refresh is an
  authorized bulk opt-in and survives while hidden.
- **Shared layout helpers.** The board TUI and import picker share pure
  row-window and width helpers through `internal/tuilayout`: rows-per-frame
  arithmetic, scroll clamping, visible range selection, right-padding, and
  rune-width truncation. Renderer-specific chrome accounting (`headerLines`,
  banner/title rendering, footer composition) stays local, so `internal/importtui`
  remains a sibling of `internal/tui` rather than importing it.
- **Additive and one-way.** Import only creates prompt files (or refreshes them
  under `--overwrite`). It never deletes prompts, never mutates Wispr, and does
  not establish any ongoing sync — the result is standalone prompt files. This is
  unrelated to the "no in-tool snippet/composition" non-goal.
- **Command shape.** `tprompt import` is a source-dispatch parent; `tprompt
  import wispr` carries `--db-path`, `--project`, `--dry-run`, `--overwrite`,
  `--tag`. Created paths print to stdout (one per line, scriptable); the human
  summary is tty-gated to stderr. **macOS is the only zero-config platform**
  (`~/Library/Application Support/Wispr Flow/flow.sqlite`); `DefaultDBPath` returns
  no default for every other platform — including Linux and Windows-via-WSL2, since
  tprompt ships **no native Windows binary** (Windows is WSL2 = the Linux build —
  see EXPECTATIONS "Platform And Packaging"). Those platforms require `--db-path`
  (from WSL2 the Windows DB is at
  `/mnt/c/Users/<you>/AppData/Roaming/Wispr Flow/flow.sqlite`); `--db-path` is
  checked before the OS lookup, so it is a universal escape hatch on every platform.
- **Source seam.** Sources implement a small `ImportSource` interface (`Name` +
  `NewCommand`) and self-register in a package-level, non-empty registry; the
  first entry is the bare-import dispatch default (`wispr`). Source commands
  produce `ImportRecord` values (`ToPrompt`/`Tags`/`Title`/`Disambiguator`) for
  the shared ingest engine, so classification, deterministic disambiguation, and
  picker labelling name no source-specific type. `wispr` is the sole built-in
  implementer today; the seam exists so a second source is **additive** —
  register an entry and supply records; the engine and dispatch are unchanged.
  (Adopted in AUR-531 with the dispatch below, ahead of a second source, by
  explicit decision.)
- **Bare-import dispatch (the §30 analog).** Outside tmux, without a tty, or when
  the invocation's streams cannot host the picker, bare `tprompt import` prints
  help (the source-dispatch parent's default). Inside tmux with an
  interactive-capable tty (stdin **and** stdout), bare `tprompt import`
  dispatches to the default source's interactive picker (`import wispr -i` in
  effect) — mirroring §30's bare `tprompt` → `tui`. Unlike root `tprompt` →
  `tui`, the import dispatch runs in the `import` parent's `RunE` after cobra has
  parsed root persistent flags and accepted the invocation as truly bare. So a
  named source (`import wispr`), a source flag (`import --dry-run`), a stray
  positional (`import bogus`), or a malformed root flag (`import --config`) stay
  cobra-owned and run or fail as typed, while valid root persistent flags
  (`--config x`) are allowed in any position. The stream check mirrors the
  picker's own preflight (both injected streams must be ttys), so a redirected
  stdout (`tprompt import >out.txt`) falls back to help rather than failing the
  `-i` tty preflight.
- **Error taxonomy → exit codes.** A missing DB path, or no default location and
  no `--db-path`, is a **usage error** (exit 2), mirroring a missing prompts
  directory. A DB that exists but cannot be opened/read (locked, or macOS Full
  Disk Access denied) is a **general error** (exit 1). Errors name the actionable
  fix (`--db-path`, quit Wispr Flow, grant Full Disk Access).

### 35. `tprompt new` auto-opens the editor when interactive

- **Auto-open is the default, deliberately against the create-vs-edit norm.**
  Surveyed scaffold commands (`cargo new`, `gh issue create`, `kubectl create`,
  `helm create`, `go mod init`) write the file and return; only *edit* commands
  (`git commit`, `crontab -e`, `kubectl edit`) auto-open an editor. `tprompt new`
  is a scaffold command, so the norm would be opt-in. We chose auto-open-by-default
  anyway because tprompt is a personal authoring tool where "create then fill in"
  is the dominant flow — but only after making it **safe by gating**, so it never
  surprises a script or CI. `edit_on_new = true` is the default; set it `false` to
  restore scaffold-and-stop.
- **The gate is what makes it safe.** The editor opens only when ALL hold: stdin
  **and** stdout are both ttys; an editor command resolves; and editing is not
  suppressed. So `p=$(tprompt new x)` (captured stdout = non-tty), pipelines, and
  CI never launch an editor and never leak one onto a stdout the scripting
  contract reserves for the path. `--no-edit` force-skips; `--edit` and
  `--editor <cmd>` force-open; `--editor` also selects the editor and implies edit.
- **Editor resolution follows the Unix chain, no fallback.** `--editor` flag ›
  config `editor`-less by design (we use the shell standards) › `$VISUAL` ›
  `$EDITOR`. There is **no** hardcoded `vi`/nano fallback: a scaffold command must
  not force-open an editor the user never configured. The resolved value is a
  shell command line run via `sh -c "<value> \"$@\""` (so `code --wait` works);
  do not field-split it. Editing stays best-effort — a failing editor is reported
  but never fails the create.
- **Output is tty-aware to de-duplicate.** stdout prints `Created <path>` when it
  is a terminal and the **bare path** when piped/redirected. The bare-path form is
  the unchanged scripting contract; the friendly form replaces the old
  path-on-stdout + hint-on-stderr pair, which repeated the path in a terminal.
