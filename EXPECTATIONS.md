# Expectations

This file defines the MVP contract for the coding agent.

## Success criteria

A correct MVP implementation should satisfy all of the following.

### Prompt resolution

- reads markdown files from a configured prompts directory
- derives ID from filename stem only
- rejects duplicate filename-stem IDs with a clear error
- can list prompts and show which file each ID maps to

### CLI behavior

- supports non-interactive send by ID
- supports interactive selection
- supports a tmux-popup-oriented command path
- returns non-zero exit codes on failure
- emits human-readable errors

### Popup delivery behavior

- popup flow hands work to a daemon
- popup process exits before delivery occurs
- daemon verifies target pane readiness using tmux state
- daemon injects only after verification passes
- daemon fails cleanly if the original pane vanished or became invalid

### Delivery behavior

- default mode is paste
- optional mode is type
- can optionally send Enter after injection
- injects the prompt body, not frontmatter

### Reliability

- does not depend on fixed sleeps for popup correctness
- can detect tmux pane disappearance
- can detect duplicate prompt IDs
- can surface daemon connectivity problems clearly

### Testing

- unit tests for prompt discovery and duplicate detection
- unit tests for frontmatter/body parsing
- unit tests for job validation
- integration-ish tests for tmux command construction
- test coverage for error conditions

## Non-goals for MVP

Do not expand scope into these features during MVP:

- prompt templating variables
- snippets/composition
- clipboard sync outside tmux
- per-application readiness adapters
- remote targets
- distributed daemon
- editing UI
- history browser
- analytics dashboard
- multi-user support

## Behavioral contract

`tprompt` guarantees **verified tmux-targeted delivery**, not semantic confirmation that the target application interpreted the input as intended.

Examples:

- If the target pane is a shell prompt, delivery is likely to behave as expected.
- If the target pane is Vim in normal mode, the injection may technically succeed but not semantically do what the user wanted.

That is acceptable for MVP.

## Preferred operator experience

### For direct use

```bash
tprompt send code-review
```

### For popup use

- user opens popup via tmux key binding
- user chooses a prompt
- popup closes
- daemon injects into original pane

## Packaging expectation

Prefer a single binary plus a per-user local daemon.

## Platform expectation

Primary support target for MVP:

- Linux
- macOS

Windows support is not required unless tmux workflow is explicitly re-scoped.
