# Multi-source Prompts and Scaffolder (archived PRD)

Historical. Live contract: DECISIONS §18, §31, §36; `docs/storage/config.md`;
`docs/storage/prompt-store.md`.

## Problem

- A fresh install could not run any authoring command: `prompts_dir` was
  required and validation failed until config was hand-edited.
- Authoring meant hand-writing YAML frontmatter with no way to discover the
  schema.
- One directory gave no clean way to mix personal, shared, and project prompts.
- Any duplicate ID was a hard error, even an intentional project override.

## Solution

1. **Default global path with auto-create.** `prompts_dir` became optional,
   defaulting to `$XDG_CONFIG_HOME/tprompt/prompts` (fallback
   `~/.config/tprompt/prompts`). Only that default is auto-created; an explicit
   path, additional folders, and project overlays are never created on read.
   `additional_prompts_dirs` layers extra global folders.
2. **Project overlay.** A `tprompt/` directory found by walking up from the
   working directory overlays the global store. The walk stops at the first
   `tprompt/` or `.git`, and is bounded at the home directory and `/` so a
   `tprompt/` folder in `~` is never treated as a project. `prompt_priority`
   (`"global"` default, or `"project"`) decides cross-tier collisions.
   Default-global-wins keeps keybind muscle memory stable when an overlay is
   added. Shadowed prompts stay visible in `list` (with a marker), in `show`
   (with the winning path), and in TUI search, but get no board key.
3. **`tprompt new <id>`.** Writes a template with every frontmatter field
   stubbed; `.md` is implied; `--project` writes `<gitroot>/tprompt/<id>.md`
   (auto-creating the directory, failing clearly outside a git tree); refuses to
   overwrite and names the existing path; rejects IDs that are empty, contain
   path separators, end in `.md`, or have non-printable runes; prints the
   absolute path. Writes only to the primary global directory, never to an
   additional folder.
4. **Empty-value tolerance.** `key:`, `mode:`, `tags:`, and `enter:` with no
   value decode as absent, so scaffold stubs defer to config. Strictly a
   relaxation: a non-empty invalid `mode` is still rejected.

Within-tier duplicates (inside one source, or across global folders) stay a hard
error: they are configuration bugs, not intentional overrides.

## Design

A pure path resolver over (config, env getter, cwd, stat predicate) producing
ordered sources; a multi-source store that validates each source and then
applies a pure conflict resolver (winner map plus shadow list); `doctor` lists
every source, its scope, existence, and the active policy.

## Out of scope then

Project config files, nested overlays, `new --force` / `--edit` (added later),
stdin bodies, file watching, per-prompt scope overrides, glob or symlink
expansion of additional folders, nested IDs like `category/name`, body
templating (added later).
