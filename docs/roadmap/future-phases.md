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
- **modifier-key combos for popup keybinds** (`ctrl+x`, `alt+p`, etc.) — MVP is single printable char only
- **live clipboard preview inside popup TUI** (read on popup open, show size/first-line) — MVP is read-on-select with no preview
- per-prompt sanitize override via frontmatter
- popup singleton enforcement (reject a second popup if one is already open)

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
