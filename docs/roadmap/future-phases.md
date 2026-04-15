# Future Phases

These ideas are intentionally deferred.

## Near-term post-MVP

- recent prompt history
- favorites/pinned prompts
- aliases
- richer `list` and `show` output
- auto-start daemon if missing
- opt-in success banner (`confirm_delivery = true`) for users who want positive confirmation

## Medium-term

- prompt templating variables
- prompt composition/snippets
- shell completions
- **project-specific prompt folders** — when invoked inside a project, also load prompts from a per-project directory (e.g. `.tprompt/` in CWD or nearest ancestor) merged with the global store. Needs rules for precedence on stem/`key:` collisions.
- **modifier-key combos for TUI keybinds** (`ctrl+x`, `alt+p`, etc.) — MVP is single printable char only
- **live clipboard preview inside the TUI** (read on TUI open, show size/first-line) — MVP is read-on-select with no preview
- per-prompt sanitize override via frontmatter
- tmux-popup singleton enforcement (reject a second tmux popup running `tprompt` if one is already open)

## Advanced

- application-aware adapters for agent CLIs
- stronger readiness checks for specific terminal apps
- **cross-host clipboard** (laptop → remote via OSC-52 read or a custom relay) — MVP reads the tmux host's clipboard only
- remote multi-host delivery
- optional job persistence/replay
- web or desktop management UI
- stdin as a source for `tprompt send -` (pipe arbitrary content)

## Explicit caution

Do not implement these during MVP unless the user explicitly asks for a scope expansion.
