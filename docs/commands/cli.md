# CLI Commands

This file describes the current command surface.

## Commands

### `tprompt` (no subcommand)

Default dispatch: when stdin is a tty **and** `$TMUX` is set, the invocation is rewritten to `tprompt tui` before cobra parses flags, so a tmux binding can use `tprompt --target-pane '#{pane_id}' ...` instead of `tprompt tui --target-pane '#{pane_id}' ...`. Outside tmux (or without a tty), bare `tprompt` prints help.

Without `--target-pane`, `tui` enters **direct mode**: it delivers to the current pane and shows a banner nudging popup setup — but only when it can confirm the pane is not a popup (`$TMUX_PANE` resolves and appears in `tmux list-panes -a`). Inside a popup, or on any ambiguity, it exits 2 with a usage error pointing at `tprompt init`. The canonical popup binding passes `#{pane_id}` explicitly and bypasses direct mode. See DECISIONS.md §30 and `examples/tmux-bindings.md`.

`tprompt --version` (or the `-v` shorthand) prints the build-stamped version and exits 0 — release builds report the release tag (e.g. `0.3.0`), unstamped dev builds report `dev`. Inside tmux+tty the version flags are recognized before the default-`tui` rewrite, so they behave the same in and out of a popup.

### `tprompt new <id>`

Scaffolds a new prompt markdown file with every supported frontmatter field
stubbed empty, then prints the absolute path of the created file. Empty
frontmatter values are tolerated at load time (see
`docs/storage/prompt-store.md`), so a freshly scaffolded file loads cleanly
without further edits.

Output is tty-aware. When stdout is a terminal, `new` prints a single
`Created <path>` line. When stdout is piped or redirected (e.g.
`p=$(tprompt new foo)`), it prints exactly the bare created path and nothing
else — the scripting contract. In an interactive terminal `new` also opens the
created file in your editor (see `--editor`/`--edit` below); piped, redirected,
and CI runs never launch an editor.

Flags:

- `--project` — scaffold into `<gitroot>/tprompt/` instead of the primary
  global prompts directory. Outside any git tree the command fails with a
  clear `no project root found` error so users do not accidentally create a
  stray `tprompt/` folder somewhere unexpected.
- `--force` — overwrite the target file if it already exists, instead of
  refusing. Only the exact file `new` would write is overwritten (atomically,
  via a temp file + rename, so a failed write never loses the original); a
  same-id prompt in a subdirectory or another source is a duplicate `--force`
  cannot resolve in place, so the command still refuses those. A `--force`
  create of a not-yet-existing id behaves like a normal create.
- `--editor <cmd>` — editor command to open the new file with for this run.
  Implies editing and overrides `$VISUAL`/`$EDITOR`. Run as a shell command (a
  path containing spaces must be quoted, as with git), so `code --wait` works.
- `--edit` — force-open the created file in your editor after scaffolding.
- `--no-edit` — never open an editor, even in an interactive terminal. Mutually
  exclusive with `--edit` and `--editor` (pairing them is a usage error).

  Editor behavior: by default, an interactive `new` (stdin and stdout both ttys)
  opens the file in the first editor that resolves from `--editor`, then
  `$VISUAL`, then `$EDITOR`. There is no built-in `vi`/nano fallback, so nothing
  opens when none is set. Set `edit_on_new = false` in config to disable
  auto-open by default. Opening is a clean no-op for piped/non-tty/CI runs and is
  best-effort: a failing editor is reported on stderr but does not fail the
  command, since the file was already created.

ID validation (rejected up front, exit 2):

- empty id
- contains a path separator (`/` or `\`)
- starts with a dot (would produce a hidden file the store skips)
- ends with `.md` (pass the bare id; the extension is implied)
- contains non-printable runes

Behavior:

- Without `--project`, writes to the primary global source. The default
  global path (`$XDG_CONFIG_HOME/tprompt/prompts` or
  `~/.config/tprompt/prompts`) is auto-created on first use; an explicit
  `prompts_dir` must already exist.
- With `--project`, writes to `<gitroot>/tprompt/`, auto-creating that
  directory if missing.
- Refuses to overwrite if any markdown file under the resolved tier already
  has the same filename stem — even nested in a subdirectory, since
  directories do not namespace IDs. Exits non-zero (exit 3) and names the
  conflicting path. `--force` overwrites the exact target file; a same-id file
  at a different path is still refused.

### `tprompt list`

Lists all available prompt IDs with their resolved TUI board key state.
The second column is the source scope (`global` or `project`). Shadowed
cross-tier collisions remain listed but are marked as shadowed and are not
assigned board keys.

Current output shape:

```text
code-review  global  key c (explicit)
bug-hunt  project  key 1 (auto)
deep-review  global  key none (overflow, not on board)
deploy  project  shadowed by /home/user/.config/tprompt/prompts/deploy.md
```

### `tprompt show <id>`

Shows the resolved prompt. Output order:

- `ID:` — prompt ID
- `Source:` — source file path
- `Scope:` — `global` or `project`
- `Title:`, `Description:`, `Tags:` — included only when the frontmatter sets them
- `Key:` — resolved board key state, formatted as `<key> (explicit)`,
  `<key> (auto)`, or `none (overflow, not on board)`
- `Shadowed counterpart:` — included when another cross-tier prompt with the
  same ID exists but lost the active `prompt_priority` policy
- a blank line, then the markdown body

Example:

```text
ID: code-review
Source: /home/user/.config/tprompt/prompts/code-review.md
Scope: global
Title: Code Review
Description: Deep review prompt focused on correctness, risk, tests
Tags: review, code
Key: c (explicit)

Review this code for correctness, risk, and missing tests.
```

### `tprompt send <id>`

Resolves the prompt and sends it to a tmux pane.

Flags:

- `--mode paste|type`
- `--enter`
- `--target-pane <pane-id>`
- `--sanitize strict|safe|off`

Behavior:

- if `--target-pane` is omitted, use current pane context when available
- if not inside tmux and no target pane supplied, fail clearly
- delivery settings resolve in this order:
  - CLI flags
  - prompt frontmatter defaults
  - config file
  - built-in defaults
- always a direct send; never writes a handoff job

### `tprompt paste`

Delivers the host system clipboard into a tmux pane.

Flags (mirror `send`):

- `--mode paste|type`
- `--enter`
- `--target-pane <pane-id>`
- `--sanitize strict|safe|off`

See `docs/commands/paste.md` for full behavior, exit codes, and failure modes.

### `tprompt pick`

Interactive prompt selection in the current process using the configured external picker (`picker_command`, default `fzf`).

Behavior:

- list prompts
- let user choose one via the external picker
- print the selected ID on stdout for shell composition

This is distinct from the built-in TUI, which is not configurable. `pick` is a scripting hook, not an end-user UX.

### `tprompt tui`

Launches the built-in interactive TUI, which submits a delivery job to a short-lived handoff worker for deferred injection into the target pane. Typically invoked from a tmux popup, but works in any terminal context. Pass `--target-pane` to name the destination; omit it to fall back to direct mode against the current pane (subject to the popup-safety rule in the no-subcommand section above). See `docs/commands/tui-flow.md` for the end-to-end flow and `docs/commands/tui.md` for the TUI details.

### `tprompt doctor`

Checks environment and configuration. Each line is prefixed `ok`, `warn`, or
`FAIL` so output is greppable.

Checks, in order:

1. **config loads and validates** — `FAIL` on any load or validation error.
2. **prompts directory exists** — `FAIL` when missing.
3. **prompt priority and project overlay** — reports `prompt_priority` and the
   project overlay path, or `no project overlay`.
4. **prompts discovered** — `FAIL` on duplicate IDs, malformed
   frontmatter, or duplicate/reserved/malformed `key:` values; reports the
   discovered prompt count on success.
5. **inside tmux** — `warn` when `$TMUX` is unset.
6. **tmux popup binding** — only when inside tmux: parses `tmux list-keys` and
   reports `ok` when a key binding's command contains `tprompt`, else `warn ...
   run 'tprompt init'`. Detection is heuristic (any tprompt-invoking binding
   counts). Skipped outside tmux, and never a `FAIL` — a missing binding is a
   nudge, not a blocker. Adapter/query failures degrade to a soft `warn`.
7. **clipboard reader** — `warn` when no reader is auto-detected and no
   override is configured, or when a configured override is missing on
   `$PATH`. `tprompt send`-only workflows do not need a reader.
8. **picker command** — `warn` when `picker_command` is empty or its binary is
   not on `$PATH`. Only `tprompt pick` needs this.
9. **TUI handoff ready** — `warn` when the handoff worker cannot be
   constructed. Direct `send`/`paste` are unaffected.

Only the first three checks affect the exit code. Tmux, popup-binding,
clipboard, picker, and handoff-readiness failures are reported as warnings so a
user who only runs `tprompt send` is not blocked by missing optional tooling.

Example output:

```text
ok   config loaded (/home/user/.config/tprompt/config.toml)
ok   prompt priority: global
ok   prompts directory exists (scope global, /home/user/.config/tprompt/prompts) [default]
ok   project overlay: no project overlay
ok   4 prompts discovered
ok   inside tmux
warn no tmux key binding runs tprompt (run 'tprompt init' to wire the popup)
ok   clipboard reader: pbpaste (auto-detected, darwin)
warn picker command: fzf not found on $PATH (tprompt pick unavailable)
ok   TUI handoff ready (/home/user/.local/state/tprompt/jobs)
```

### `tprompt init`

Prints the tmux configuration needed to launch the popup workflow. It never edits
your tmux config or touches tmux — it only prints, so you stay in control of your
files. The printed binding references the canonical `tprompt` command on `$PATH` and
omits `--config` (see DECISIONS.md §30); a user with a non-default config adds
`--config` to the printed line themselves.

By default it prints the popup binding plus the two steps to install it.

Flags:

- `--more` — print the full setup menu: the popup binding, the direct paste/send
  bindings, key customization, and a `tprompt doctor` verification step.
- `--snippet` — print only the raw binding line, e.g. to append to a config file.
  Mutually exclusive with `--more`.
- `--key <letter|digit>` — tmux key to bind the popup to (default `P`). Must be a
  single ASCII letter or digit; any other value is a usage error (exit 2).

Takes no positional arguments. See `examples/tmux-bindings.md` for the same bindings
documented for reference.

### `tprompt import`

A source-dispatch parent: `tprompt import <source>` runs a specific importer
(currently only `wispr`). On its own, `tprompt import` prints help — **except**
inside tmux with an interactive-capable terminal (stdin **and** stdout are ttys),
where bare `tprompt import` dispatches to `tprompt import wispr -i` (the default
source's interactive picker). This mirrors the bare `tprompt` → `tprompt tui`
default (top of this doc). Only a *truly bare* import dispatches: naming a source
(`import wispr`), passing a source flag (`import --dry-run`), or a stray argument
all run as typed, and a redirected stdout (`tprompt import >out.txt`) falls back
to help rather than failing the picker's tty preflight.

### `tprompt import wispr`

Imports your local [Wispr Flow](https://wisprflow.ai) snippets as markdown
prompts. Wispr stores snippets (trigger → expansion text) in a local
`flow.sqlite`; `import wispr` opens that database **read-only** — it never writes
to Wispr — and reads **only snippets**, never your dictation history. Each live
snippet becomes a prompt in the primary global prompts directory:

- `phrase` → `title:` frontmatter (verbatim) **and** the slug for the filename id
- `replacement` → the markdown body
- `tags: [wispr]` provenance tag (a starred snippet also gets `starred`)

Imported prompts carry the **same frontmatter fields `tprompt new` scaffolds**
(`title`, `description`, `tags`, `key`, `mode`, `enter`): `title`/`tags` are
filled in from the snippet and the rest are left as empty stubs, so an imported
prompt is as ready to edit (add a keybind, set the delivery mode) as a scaffolded
one. The snippet-controlled fields are YAML-marshaled (not string-templated), so
a phrase containing `:` or quotes cannot corrupt the written file.

Import is **idempotent**: a snippet whose id already exists as a prompt is
**skipped** by default, so re-running never creates duplicates (use `--overwrite`
to refresh from Wispr, or `--dry-run` to preview). Created file paths print to
stdout (one per line, for scripting); an `imported N, skipped M` summary prints
to stderr when stderr is a terminal (piped runs emit nothing on stderr).

Flags:

- `--db-path <path>` — path to Wispr's `flow.sqlite`. **macOS** is the only
  zero-config platform: it defaults to `~/Library/Application Support/Wispr
  Flow/flow.sqlite`. Everywhere else — **Linux**, and **Windows via WSL2** (there
  is no native Windows build, so tprompt runs as the Linux binary there) — there is
  no default, so pass `--db-path` explicitly or you get a usage error (exit 2).
  From WSL2, Wispr Flow's Windows database is reachable at
  `/mnt/c/Users/<you>/AppData/Roaming/Wispr Flow/flow.sqlite`.
- `--project` — write to the project overlay (`<gitroot>/tprompt`) instead of the
  primary global prompts directory. Must be run inside a git tree.
- `--dry-run` — preview without writing. `would create:`, `would overwrite:`,
  and `would skip:` lines go to stderr and nothing is created; stdout stays empty.
  It previews the import *plan* (which ids would be created, refreshed, or skipped,
  plus any id/duplicate errors); it does not verify write permissions or disk
  space, which the real import validates.
- `--overwrite` — refresh existing prompts from Wispr instead of skipping them
  (replaces the body and frontmatter of a prompt whose id already exists).
- `--tag <tag>` — provenance tag stamped on every imported prompt (default
  `wispr`). A starred snippet still also gets `starred`.
- `-i`, `--interactive` — open a checkbox picker to choose which snippets to
  import, with conflicts surfaced for review. Each row shows a status glyph:
  - `[ ]`/`[x]` — a **fresh** snippet (pre-checked; toggle to import).
  - `[=]` — the id already exists at the **exact target**: **skip-by-default**
    (§34); check it to **arm a per-item overwrite** that refreshes just that prompt
    (the row then shows `[x]`).
  - `[!]` — a same-id prompt exists at **another path** (a cross-path duplicate):
    **non-selectable**, shown with the conflicting path.

  Keys: `↑`/`↓` (or `j`/`k`) move, `Space` toggles/arms the current row, `a`
  resets to the safe default (all fresh checked, ad-hoc overwrites cleared,
  CLI-authorized refreshes kept), `Enter` writes, `Ctrl+C` cancels. `/` opens
  **fuzzy search** over each snippet's id, title, and tags (so `starred` finds
  starred snippets) — type to filter live, `Enter` applies the filter and returns
  to the list (the header then shows `filtered "<query>"`), `Esc` clears it. Once
  a filter is applied, `Space`/`a` act on the **visible** rows only (so select-all
  respects the filter), while the footer counts and `write N prompts?` always
  reflect the **global** selection across all snippets. `Esc` backs out one level:
  it clears an active filter first, and cancels the picker only when no filter is
  active. The footer counts `N selected · M overwrite · K blocked` and confirms
  `write N prompts?`. Confirming creates the checked fresh rows and overwrites the
  armed rows; **cancelling writes nothing and exits 0**. Per-item overwrite is
  **exact-target-only**: it routes through the same writer as `--overwrite`, which
  still refuses a cross-path duplicate as a **prompt-store error (exit 3)** — the
  picker surfaces the §34 policy, it cannot weaken it. `-i` needs an interactive
  terminal and cannot combine with `--dry-run` (both are usage errors — see below).
  `-i --overwrite` is allowed: the flag pre-arms every refresh (shown as `[x]`),
  the all-or-nothing counterpart to per-item arming.

When two snippets' phrases normalize to the same id, the first keeps the bare
slug and each later one gets a short `-<uuid-prefix>` suffix, so no snippet is
dropped. A phrase with no slug-able characters falls back to a
`wispr-<uuid-prefix>` id; the original phrase is still preserved verbatim as the
title.

Exit codes: a `--db-path` (or default) that does not exist, or an OS with no
default location and no `--db-path`, is a **usage error** (exit 2). A database
that exists but cannot be read (locked, or macOS Full Disk Access denied) is a
**general error** (exit 1). `-i` without an interactive terminal (e.g. piped
stdin/stdout), and `-i --dry-run`, are both **usage errors** (exit 2): the picker
fails obviously rather than silently falling back to a non-interactive import.

## Cancel semantics

When the user cancels an interactive flow (TUI `Esc`, `pick` external cancel, `import wispr -i` `Esc`), the command exits with **status 0**. Cancellation is a valid outcome, not an error. Scripts should not treat it as a failure.

## Exit code guidance

- `0` success **or** user cancellation
- `1` general error (e.g. a Wispr database that exists but cannot be read)
- `2` usage/config error
- `3` prompt resolution error / clipboard validation error / sanitizer strict-mode rejection
- `4` tmux environment error
- `5` local handoff/IPC error
- `6` delivery or verification error

These are the current command contract and should remain stable unless the
behavior contract is explicitly updated.
