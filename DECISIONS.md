# Locked Decisions

These decisions are already made and should be treated as constraints unless the user explicitly reopens them.

## Product

### 1. `tprompt` is both interactive and non-interactive

It must support:

- direct send by ID
- interactive prompt selection
- popup-first workflows in tmux

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

### 5. Popup workflow is first-class

The tool should be designed around reliable use from tmux popups, not treat popup usage as a bolt-on extra.

### 6. Deferred send must be daemon-backed

The popup process should not sleep and then try to inject directly. It should hand off a job to a daemon, then exit.

### 7. Verification must use tmux state, not timers

The daemon should only inject after confirming that the original target pane is available and back in the intended active state.

### 8. Delivery modes

Two delivery modes are required:

- `paste` (default)
- `type` (fallback)

### 9. Prompt body is what gets injected

If markdown files include YAML frontmatter, only the markdown body is injected. Frontmatter is metadata only.

### 10. MVP is tmux-first

Outside-tmux support is not required for MVP.

## Rationale for filename-stem IDs

This makes prompt invocation fast and memorable.

Benefits:

- short IDs
- easy shell usage
- easy popup selection
- easier to remember than path-based IDs

Tradeoff:

- duplicate filenames become invalid across the whole prompt store

This tradeoff is acceptable for MVP.

## Required duplicate-ID behavior

On startup, list, send, or doctor, the tool should detect collisions and return a clear error with all conflicting paths.

Example:

```text
Duplicate prompt ID detected: code-review
- /home/user/.config/tprompt/prompts/agents/code-review.md
- /home/user/.config/tprompt/prompts/reviews/code-review.md
```

The coding agent should not try to silently disambiguate duplicate IDs in MVP.
