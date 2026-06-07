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
- `tprompt tui` launches the built-in TUI and submits delivery through a short-lived handoff worker.
- Bare `tprompt` dispatches to `tprompt tui` when stdin is a tty and `$TMUX` is set.
- Bare `tprompt` outside tmux or without a tty prints help.
- With no `--target-pane`, the TUI enters direct mode and delivers to the current pane (showing a banner that nudges popup setup) — but only when the origin pane is confirmed real (`$TMUX_PANE` is present in `tmux list-panes -a`). Inside a popup, or on any ambiguity, it exits 2 with a usage error pointing at `tprompt init`.
- Operational failures return non-zero exit codes.
- User cancellation in `pick` or the TUI exits with status 0.
- Errors should be human-readable and specific enough to fix the local environment or prompt data.

## Import

- `tprompt import wispr` imports local Wispr Flow snippets as prompts. It opens
  Wispr's local `flow.sqlite` **read-only** and never writes to it.
- It is not macOS-only, but **macOS is the only zero-config platform**: the DB
  path is resolved automatically there. On Linux, and on Windows via WSL2 (there
  is no native Windows build), pass `--db-path` (from WSL2:
  `/mnt/c/Users/<you>/AppData/Roaming/Wispr Flow/flow.sqlite`); omitting it where
  there is no default is a usage error (exit 2) that names the fix. On macOS,
  reading the database may require granting your terminal **Full Disk Access** — a
  DB that exists but cannot be read is a general error (exit 1) that names the fix.
- `tprompt import` on its own prints help, **except** inside tmux with an
  interactive terminal (stdin and stdout are ttys), where bare `tprompt import`
  opens the default source's interactive picker (`import wispr -i`) — the same
  convenience as bare `tprompt` opening the TUI. Naming a source, passing a source
  flag, or redirecting output runs as typed (or falls back to help) instead.
- It reads **only snippets** (Wispr `Dictionary` rows that are snippets and not
  deleted); it never reads dictation History.
- Imported prompts carry the same frontmatter fields that `tprompt new`
  scaffolds (`title`, `description`, `tags`, `key`, `mode`, `enter`): `title` and
  `tags` come from the snippet and the rest are empty stubs, so an imported prompt
  is as ready to edit (add a keybind, set the delivery mode) as a scaffolded one.
- Import is idempotent and **skip-by-default**: a snippet whose id already
  exists as a prompt is skipped, so re-runs never create duplicates. `--overwrite`
  refreshes existing prompts from Wispr; `--dry-run` previews without writing;
  `--project` writes to the project overlay; `--tag` overrides the provenance tag.
- `-i`/`--interactive` opens a checkbox picker that surfaces conflicts so you can
  review them before writing; the glyphs, keys, footer, and search behavior are
  specified in
  [docs/commands/cli.md](docs/commands/cli.md#tprompt-import-wispr). Confirming
  creates the checked fresh rows and overwrites exactly the armed conflicts;
  cancelling writes nothing and exits 0; any no-op outcome (cancel, deselect-all,
  nothing selected) creates no files and no directories. Per-item overwrite is
  **exact-target-only** and routes through the same writer, which still refuses a
  cross-path duplicate as a hard error (exit 3) — the picker surfaces policy, it
  cannot weaken it. `-i` requires an interactive terminal and rejects `--dry-run`
  (both usage errors, exit 2); `-i --overwrite` is allowed and pre-arms every
  refresh (the all-or-nothing opt-in), while per-item arming is the finer choice
  when no flag is passed.
- The picker's `/` **fuzzy search** (over id, title, and tags such as `starred`)
  is a view filter: your checked fresh snippets stay selected across the whole
  library, so confirming imports your true selection — not just the filtered view,
  and the `write N prompts?` count reflects that global selection. The one
  visibility-scoped exception is a safety measure: an armed per-item overwrite is
  disarmed if a filter hides its row, so a filter can never leave a hidden
  destructive overwrite pending (its overwrite count drops accordingly).
- Importing never drops a snippet: two phrases that normalize to the same id are
  disambiguated with a short uuid suffix, and a phrase with no slug-able
  characters falls back to a `wispr-<uuid>` id (its phrase is still its title).
- Importing Wispr snippets produces standalone prompts — a one-way migration
  into the prompt store. This is unrelated to the "prompt snippets or
  composition" non-goal below: tprompt still has no in-tool snippet/composition
  system; it only ingests Wispr's snippets as ordinary prompt files.

## TUI Delivery Behavior

- The TUI is built in; it is separate from the external `pick` command.
- The board shows single-key prompt shortcuts plus a pinned clipboard row when enabled. The clipboard row is labeled `clipboard` in the id column.
- Explicitly keybound prompts render above auto-assigned ones on the board; each group stays alphabetical by id.
- A row is delivered either by pressing its single-key shortcut, or by moving the cursor with `↑`/`↓` and pressing the **Select** key (default `Enter`). Both paths submit through the same flow.
- `/` enters fuzzy search over prompt ID, title, description, and tags.
- Overflow prompts are not shown on the board but are reachable through search.
- The TUI reads clipboard content only when the clipboard row is selected.
- The TUI writes prompt or clipboard content to a private handoff job, spawns a short-lived worker, then exits.
- The handoff worker verifies target pane readiness using tmux state before injection.
- The worker fails cleanly if the target pane vanishes or becomes invalid.
- Handoff jobs are per selection; no long-running queue or daemon is required for the TUI path.

## Background Services

- `tprompt` has no user-facing daemon or background service.
- `tprompt send`, `tprompt paste`, `tprompt tui`, and `tprompt doctor` do not start, contact, or depend on a daemon.
- Deprecated `--daemon-auto-start` / `--no-daemon-auto-start` flags are accepted on `tprompt tui` for compatibility but have no effect.
- Release signing covers the CLI binary. It is not tied to detached daemon operation.

## Delivery Behavior

- Default mode is bracketed paste: `load-buffer` plus `paste-buffer -p`.
- Fallback `type` mode uses `send-keys -l` with rune-safe chunking.
- `--enter` sends Enter as a separate tmux command after content delivery.
- Default behavior is no trailing Enter.
- Direct sends never touch deferred-delivery state.
- A configurable `max_paste_bytes` limit rejects oversized prompt or clipboard content before tmux delivery.

## Clipboard Reader

- Clipboard scope is the host running `tprompt` and tmux.
- Auto-detection prefers platform and environment-specific tools:
  `pbpaste`, `wl-paste`, `xclip`, or `xsel`.
- `clipboard_read_command` overrides auto-detection.
- Empty, non-UTF-8, and oversized clipboard content is rejected before delivery.
- Handoff workers never read the clipboard. Clipboard bytes are captured by the submitting process.

## Sanitization

- Supported modes are `off`, `safe`, and `strict`.
- Default mode is `safe`.
- `safe` strips dangerous terminal control classes while preserving cosmetic sequences.
- `strict` rejects any escape sequence and reports class plus byte offset.
- The same sanitization contract applies to `send`, `paste`, and handoff-executed TUI jobs.

## Error Feedback

- Deferred-job failures are shown through `tmux display-message` when there is a scoped target.
- Deferred-job failures are appended to the configured log.
- Logs must not include prompt bodies or clipboard bytes.
- Success is silent by default.

## Reliability

- TUI-flow correctness must not depend on fixed sleeps.
- Target readiness is based on tmux pane and selection state.
- Direct sends must not block on handoff state.
- Config, prompt, tmux, handoff, and delivery failures should remain distinguishable through exit-code mapping.

## Behavioral Boundary

`tprompt` guarantees verified tmux-targeted delivery. It does not guarantee that
the target application semantically interpreted the injected input as intended.

Examples:

- A shell prompt is likely to receive the text as expected.
- Vim in normal mode may receive the text but treat it as commands.

That boundary is intentional.

## Platform And Packaging

- Primary platforms are Linux and macOS.
- Packaging target is a single CLI binary with no background daemon command surface.
- Windows is supported only through WSL2 (a Linux environment); there is no native Windows build.

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
