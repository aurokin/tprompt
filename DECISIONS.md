# Locked Decisions

These decisions are already made and should be treated as constraints unless the user explicitly reopens them.

## Terminology

- **tmux popup** — a floating window created by `tmux display-popup`. Tmux's feature.
- **TUI** — tprompt's built-in interactive terminal UI (the `tprompt tui` subcommand). Runs inside any terminal context; typically a tmux popup.
- **TUI flow** — the end-to-end path from TUI selection through daemon-verified injection into the target pane.

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

### 6. Deferred send must be daemon-backed

The TUI process should not sleep and then try to inject directly. It should hand off a job to a daemon, then exit.

### 7. Verification must use tmux state, not timers

The daemon should only inject after confirming that the original target pane is available and back in the intended active state.

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

Clipboard reader, daemon, and tmux pane all run on the same host. Cross-host clipboard (laptop → remote via OSC-52 read or similar) is an explicit non-goal.

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

The TUI process reads the clipboard, captures the content, and submits it as part of the daemon job payload before exiting. The daemon never reads the clipboard itself.

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
- append-only daemon log at `~/.local/state/tprompt/daemon.log`

No success banner by default.

### 27. Pending jobs are replaced, not queued

When a new deferred job arrives for the **same target pane** as one already pending, the new job **replaces** the old one. Matches "I changed my mind" intent. Different targets are independent.

### 28. TUI singletons are not enforced

Multiple TUI instances (including multiple tmux popups) can be open simultaneously — across clients or even on the same client. The simpler "any TUI may submit a job" rule applies unless a future Linear-scoped change introduces singleton behavior.

### 29. Direct sends bypass the daemon queue entirely

`tprompt send <id>` and `tprompt paste` (invoked outside the TUI flow) always deliver synchronously through the tmux adapter. They do not touch the daemon queue and cannot be affected by pending TUI jobs.

### 30. Bare `tprompt` defaults to `tprompt tui` in tmux + tty

When invoked with no subcommand, `tprompt` dispatches to `tprompt tui` if stdin is a tty **and** `$TMUX` is set, so users can write `tprompt --target-pane '#{pane_id}' ...` in their binding instead of `tprompt tui --target-pane '#{pane_id}' ...`. Outside tmux (or without a tty), it prints help, preserving the no-args → usage convention in a regular shell. The TUI is the signature workflow, so this default matches user intent when the environment supports it.

The dispatch is a pure arg rewrite at the top of `RunCLI` — cobra still parses flags and enforces `tui`'s required `--target-pane`, so bare `tprompt` with no flags inside tmux+tty errors clearly (exit 2). This is intentional: inside a `display-popup -E` popup, `$TMUX_PANE` resolves to the popup's own pane, not the originating one, so the binding must pass `#{pane_id}` at trigger time for delivery to target the correct pane. See `examples/tmux-bindings.md` for the canonical binding.

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

### 33. Daemon lifecycle architecture

The daemon lifecycle is owned by two layered packages and four
file-system primitives:

- **Run lock** (`<socket>.lock`, `flock(LOCK_EX|LOCK_NB)`, `O_CLOEXEC`) — held by the live daemon for its full lifetime. Cross-process exclusivity.
- **Start lock** (`<socket>.start.lock`, blocking flock) — serializes concurrent cold starts.
- **Identity sidecar** (`<socket>.identity.json`) — `(pid, start_time, version)`, written atomically (tmp+rename) and removed on graceful shutdown only when the live daemon still owns it. Defends against PID reuse via the start-time match.
- **Cooldown marker** (`<socket>.start.cooldown`) — recorded after an implicit (TUI) start failure; gates subsequent implicit starts for `DefaultCooldownTTL` (10 s). Explicit starts always bypass and clear the marker on success.

The launcher lives in `internal/app/lifecycle/`; the primitives live in `internal/daemon/lifecycle/`. The launcher is wired with three pluggable seams:

- `StatusProber` returns `ProbeOK | ProbeUnreachable | ProbeReachableBroken`. The launcher refuses to spawn over a `ProbeReachableBroken` socket — operator recovery is required (`daemon stop` or kill).
- `Spawner` returns a `SpawnHandle{PID}` so the readiness loop can detect child early-exit via `kill(pid, 0)` and report `ReasonChildExitedEarly` instead of timing out.
- `TrustAssessor` runs on the explicit-start path on darwin (AUR-327): the launcher fires the assessor for `IntentExplicitStart`, and the foreground `daemon run` entrypoint runs its own preflight before binding the socket. Implicit is short-circuited by the platform policy before the launcher reaches the assessor; `daemon stop` and `daemon status` never preflight. The non-darwin assessor remains a no-op. The `TPROMPT_UNSAFE_TRUST_PREFLIGHT_BYPASS` env var allows local development and the testscript suite to short-circuit the assessor; it is honored only when set to a known-positive value.

Command semantics:

- **`tprompt daemon start`** is non-blocking. It calls the launcher with `IntentExplicitStart`, which on darwin runs the trust preflight (AUR-327), then spawns `daemon run` detached, polls `Status` until ready, and prints either `tprompt daemon started on <socket>` or `tprompt daemon already running on <socket>`. Idempotent success when a compatible daemon is already running. Explicit intents bypass the macOS implicit-disabled policy and cooldown.
- **`tprompt daemon run`** is foreground. On darwin it runs its own trust preflight before binding the socket (AUR-327) — the launcher does not drive `daemon run`, so this preflight covers users who invoke `daemon run` directly. The same `Server.Listen → Serve → Close` lifecycle as the spawned child uses; `SIGINT`/`SIGTERM` and the `Stop` RPC both unwind through `Close`. Note: `daemon start` spawns a `daemon run` child, so on darwin the trust preflight runs twice on that path — once in the parent under `IntentExplicitStart`, once in the child's foreground entry. The cost is small (~100ms warm cache) and avoids a cross-process trust hand-off.
- **TUI auto-start** is default-on on Linux and other non-macOS platforms. It calls the same launcher with `IntentImplicitTUI`. Failure records a cooldown that gates subsequent implicit starts; explicit `daemon start`/`daemon run` always bypass the cooldown. The `TPROMPT_NO_AUTO_START` environment variable is a hard opt-out (AUR-328): set to a truthy value (`1`, `true`, `yes`, `on`; case-insensitive, whitespace-trimmed) it disables implicit auto-start across every entry point — tmux popups, scripts, mise hooks — without retrofitting flags on each call site. The opt-out check runs before config loading, so a malformed config cannot re-enable auto-start. Combining the env var with `--daemon-auto-start=true` is rejected with a conflict error so the operator's intent is unambiguous; combining it with `--no-daemon-auto-start` is allowed because both express the same intent. Unrecognized values leave auto-start active so a typo cannot silently disable the daemon. `tprompt doctor` flags both truthy and unrecognized values for visibility.
- **`tprompt daemon stop`** is mode-agnostic. It dials the configured socket, issues the `Stop` RPC, and waits (bounded) for the socket to disappear. Works for daemons spawned by any of the three modes because all converge on the same `Server.Close` cleanup.
- **`tprompt daemon status`** is a read-only probe. Never auto-starts the daemon.
- **`tprompt send`, `tprompt paste`, `tprompt doctor`** never contact or auto-start the daemon. Direct delivery and diagnostics are daemon-free.

A "compatible daemon" is one reachable at the configured socket whose `Status` RPC succeeds. Reachable-but-broken is its own classification with a manual-recovery message.

macOS implicit-disabled policy (`internal/app/lifecycle/policy_darwin.go`, AUR-326):

- `IntentImplicitTUI` is refused on darwin before the launcher reaches the cooldown, start lock, trust assessor, or spawn path. The launcher returns `OutcomeFailed` with `ReasonPolicyDisabled` and emits a `lifecycle_implicit_disabled` diagnostic.
- The TUI command path mirrors the refusal before constructing the launcher: `autoStartTUIDaemon` consults the same policy seam and surfaces the result through the standard daemon IPC error wrapper (`ExitDaemon` exit code).
- The refusal message names both recovery commands (`tprompt daemon start` and `tprompt daemon run`) so the user can act on the failure without consulting docs.
- `IntentExplicitStart` and `IntentExplicitRun` bypass the policy. There is no environment override that re-enables the implicit path; the policy is hardcoded.
- Rationale: macOS launch evaluation triggered repeated kernel panics in `AppleSystemPolicy` / `AMFI` / `syspolicyd` during implicit auto-start on real release binaries. The platform owns the cost of restoring implicit auto-start; until then, explicit intent is the contract.

The full narrative — including signing/notarization expectations and rejection-class behavior — lives in [docs/lifecycle/auto-start.md](docs/lifecycle/auto-start.md).

Rationale and library choices are detailed in `docs/implementation/tech-stack.md`.
