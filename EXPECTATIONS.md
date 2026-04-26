# Behavior Contract

This file defines the current product contract for `tprompt`. It is not a work
tracker; planned work lives in Linear.

## Prompt Resolution

- Prompt files are markdown files under the resolved prompt directory
  (`prompts_dir` when set, otherwise the default
  `$XDG_CONFIG_HOME/tprompt/prompts` with `~/.config/tprompt/prompts` as a
  fallback; the default directory is auto-created on first access).
- Prompt IDs are derived from the filename stem only.
- Directories are organizational only and do not namespace IDs.
- Duplicate filename-stem IDs are invalid and must fail with clear conflicting paths.
- Optional YAML frontmatter may define metadata such as `title`,
  `description`, `tags`, delivery defaults, and `key`.
- Frontmatter is metadata only. Delivery injects the markdown body, not the frontmatter.
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
- `/` enters fuzzy search over prompt ID, title, description, and tags.
- Overflow prompts are not shown on the board but are reachable through search.
- The TUI reads clipboard content only when the clipboard row is selected.
- The TUI submits prompt or clipboard content to the daemon, then exits.
- The daemon verifies target pane readiness using tmux state before injection.
- The daemon fails cleanly if the target pane vanishes or becomes invalid.
- A newer deferred job for the same pane replaces the older pending job.

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
- Default mode is `off`.
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
