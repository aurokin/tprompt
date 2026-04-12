# Tmux Integration

This file describes the tmux-facing responsibilities of `tprompt`.

## Tmux assumptions for MVP

- target workflow is inside tmux
- popup workflow uses tmux popups
- pane IDs are stable enough to identify the intended target during a short verification window

## Required tmux operations

The tmux adapter should centralize operations like:

- determine current pane ID
- determine current session/window when helpful
- inspect whether a pane still exists
- inspect whether a pane is selected
- capture pane output
- load buffer for paste
- paste buffer into pane
- send literal keys to pane

## Why centralization matters

Tmux command invocation is a major source of fragility. One adapter reduces duplicated shell command logic and makes tests easier.

## Delivery modes

### Paste mode

Recommended default.

Expected behavior:

- create/load tmux buffer
- paste buffer into target pane
- optionally send Enter

### Type mode

Fallback behavior.

Expected behavior:

- send literal text in a safe way
- chunk if necessary
- optionally send Enter

## Notes on post-delivery behavior

`tprompt` should not assume that a successful tmux send means the target application interpreted the prompt meaningfully. That is outside MVP scope.
