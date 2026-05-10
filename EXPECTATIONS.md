# Behavior Contract

This file defines the current product contract for `tprompt`. It is not a work
tracker; planned work lives in Linear.

## Prompt Resolution

- Prompt files are markdown files under the resolved prompt directory
  (`prompts_dir` when set, otherwise the default
  `$XDG_CONFIG_HOME/tprompt/prompts` with `~/.config/tprompt/prompts` as a
  fallback; the default directory is auto-created on first access), any
  configured `additional_prompts_dirs`, and an active project `tprompt/`
  overlay discovered from the current working directory.
- Prompt IDs are derived from the filename stem only.
- Directories are organizational only and do not namespace IDs.
- Duplicate filename-stem IDs within the same tier are invalid and must fail
  with clear conflicting paths.
- Cross-tier global/project ID collisions are resolved by `prompt_priority`
  (`global` by default). The losing prompt is shadowed, remains visible in
  `list`, and remains searchable in the TUI.
- Optional YAML frontmatter may define metadata such as `title`,
  `description`, `tags`, delivery defaults, and `key`.
- Frontmatter is metadata only. Delivery injects the markdown body, not the frontmatter.
- The parser strips one leading line break after the closing frontmatter fence and one trailing line break (`\n` or `\r\n`) from the body, so the canonical blank-line-after-fence and POSIX EOF newline that editors enforce do not become extra blank lines at paste time.
- Duplicate, reserved, or malformed `key:` values are invalid.

## CLI Behavior

- `tprompt send <id>` performs direct prompt delivery.
- `tprompt paste` performs direct clipboard delivery.
- `tprompt pick` invokes the configured external picker and prints the selected prompt ID.
- `tprompt tui` launches the built-in TUI and submits delivery through the daemon.
- Bare `tprompt` dispatches to `tprompt tui` when stdin is a tty and `$TMUX` is set.
- Bare `tprompt` outside tmux or without a tty prints help.
- Operational failures return non-zero exit codes.
- User cancellation in `pick` or the TUI exits with status 0.
- Errors should be human-readable and specific enough to fix the local environment or prompt data.

## TUI Delivery Behavior

- The TUI is built in; it is separate from the external `pick` command.
- The board shows single-key prompt shortcuts plus a pinned clipboard row when enabled.
- A row is delivered either by pressing its single-key shortcut, or by moving the cursor with `↑`/`↓` and pressing the **Select** key (default `Enter`). Both paths submit through the same flow.
- `/` enters fuzzy search over prompt ID, title, description, and tags.
- Overflow prompts are not shown on the board but are reachable through search.
- The TUI reads clipboard content only when the clipboard row is selected.
- The TUI submits prompt or clipboard content to the daemon, then exits.
- The daemon verifies target pane readiness using tmux state before injection.
- The daemon fails cleanly if the target pane vanishes or becomes invalid.
- A newer deferred job for the same pane replaces the older pending job.

## Daemon Lifecycle

- `tprompt daemon start` is non-blocking: it spawns a detached daemon, waits briefly for readiness, and returns. When a compatible daemon is already running it is an idempotent success.
- `tprompt daemon run` is foreground: the daemon runs in the invoking terminal and is stopped by SIGINT/SIGTERM or `tprompt daemon stop` from another shell.
- `tprompt daemon stop` works the same way regardless of how the daemon was started: it dials the configured socket, issues the Stop RPC, and reports success when the socket disappears. With no daemon running it prints "daemon not running" and exits successfully.
- `tprompt daemon status` is read-only and never starts the daemon implicitly.
- On Linux and other non-macOS platforms, TUI flows (`tprompt tui` and bare `tprompt` dispatching into TUI inside tmux+tty) auto-start the daemon by default when the configured socket is unreachable. Pass `--no-daemon-auto-start` (or `--daemon-auto-start=false`) to opt out for one invocation; set `daemon_auto_start = false` in config to opt out permanently. Set `TPROMPT_NO_AUTO_START` in the environment to a truthy value (`1`, `true`, `yes`, `on`; case-insensitive, whitespace-trimmed) to opt out across every entry point — including tmux popups, scripts, and mise hooks — without retrofitting flags. Unrecognized values leave auto-start active so a typo cannot silently disable it. Combining the env var with `--daemon-auto-start` (explicit on) is rejected with a conflict error; combining it with `--no-daemon-auto-start` is allowed because both express the same intent. Surface state via `tprompt doctor`, which calls out the env var when set.
- On macOS, implicit TUI auto-start is hardcoded off. A TUI invocation that finds the daemon unreachable refuses with a recovery hint pointing at `tprompt daemon start` (background) and `tprompt daemon run` (foreground); neither config nor flag re-enables the implicit path. This is locked because the macOS launch evaluation path triggered kernel panics in `AppleSystemPolicy`/`AMFI`/`syspolicyd` during implicit auto-start; the platform owns the cost of restoring it.
- On macOS, `tprompt daemon start` and `tprompt daemon run` perform an executable-trust preflight against the current binary (codesign verify, ad-hoc detection, Gatekeeper assess with CLI bypass). Ad-hoc-signed, unsigned, tampered, or Gatekeeper-rejected binaries are refused before any socket is bound, with an error that names the binary, the failure category, and recovery options (install a signed release, run `scripts/sign-macos-binary.sh` for a local dev build). `tprompt daemon stop` and `tprompt daemon status` never preflight and work against any running daemon. The `TPROMPT_UNSAFE_TRUST_PREFLIGHT_BYPASS=1` env var disables the gate for local development and the testscript suite; it must not be set in normal release operation.
- `tprompt send`, `tprompt paste`, and `tprompt doctor` are direct-path commands. They never start, contact, or depend on the daemon.
- Concurrent cold starts are serialized: only one daemon process owns the configured socket at a time.
- A "compatible daemon" is one reachable at the configured socket whose `Status` RPC succeeds. A reachable-but-broken socket is reported with a manual-recovery message rather than respawned.

## Delivery Behavior

- Default mode is bracketed paste: `load-buffer` plus `paste-buffer -p`.
- Fallback `type` mode uses `send-keys -l` with rune-safe chunking.
- `--enter` sends Enter as a separate tmux command after content delivery.
- Default behavior is no trailing Enter.
- Direct sends never touch the daemon queue.
- A configurable `max_paste_bytes` limit rejects oversized prompt or clipboard content before tmux delivery.

## Clipboard Reader

- Clipboard scope is the host running `tprompt` and tmux.
- Auto-detection prefers platform and environment-specific tools:
  `pbpaste`, `wl-paste`, `xclip`, or `xsel`.
- `clipboard_read_command` overrides auto-detection.
- Empty, non-UTF-8, and oversized clipboard content is rejected before delivery.
- The daemon never reads the clipboard. Clipboard bytes are captured by the submitting process.

## Sanitization

- Supported modes are `off`, `safe`, and `strict`.
- Default mode is `safe`.
- `safe` strips dangerous terminal control classes while preserving cosmetic sequences.
- `strict` rejects any escape sequence and reports class plus byte offset.
- The same sanitization contract applies to `send`, `paste`, and daemon-executed TUI jobs.

## Error Feedback

- Deferred-job failures are shown through `tmux display-message` when there is a scoped target.
- Deferred-job failures are appended to the daemon log.
- Daemon logs must not include prompt bodies or clipboard bytes.
- Success is silent by default.

## Reliability

- TUI-flow correctness must not depend on fixed sleeps.
- Target readiness is based on tmux pane and selection state.
- Direct sends must not block on daemon state.
- Config, prompt, tmux, daemon, and delivery failures should remain distinguishable through exit-code mapping.

## Behavioral Boundary

`tprompt` guarantees verified tmux-targeted delivery. It does not guarantee that
the target application semantically interpreted the injected input as intended.

Examples:

- A shell prompt is likely to receive the text as expected.
- Vim in normal mode may receive the text but treat it as commands.

That boundary is intentional.

## Platform And Packaging

- Primary platforms are Linux and macOS.
- Packaging target is a single CLI binary plus a per-user local daemon.
- Windows is outside the current tmux-first workflow.

## Non-Goals

- Prompt templating variables.
- Prompt snippets or composition.
- Cross-host clipboard sync.
- Per-application readiness adapters.
- Remote targets.
- Distributed daemon behavior.
- Editing UI, history browser, analytics, or multi-user features.
- Modifier-key prompt keybinds such as `ctrl+x` or `alt+p`.
- Live clipboard preview inside the TUI.
