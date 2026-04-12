# Architecture Overview

`tprompt` is composed of five major pieces.

## 1. CLI layer

Responsibilities:

- parse commands and flags
- load config
- talk to prompt store
- invoke tmux adapter directly for simple sends
- talk to daemon for deferred popup sends

## 2. Prompt store

Responsibilities:

- walk configured prompt directory
- find markdown files
- derive IDs from filename stems
- parse optional frontmatter
- expose prompt metadata and body
- detect duplicate IDs

## 3. Tmux adapter

Responsibilities:

- detect whether current execution is inside tmux
- obtain current pane/session/client context when available
- inspect pane existence and selection state
- capture pane output
- perform `paste` or `type` delivery

All tmux interaction should be centralized here rather than scattered through the CLI and daemon.

## 4. Daemon

Responsibilities:

- receive deferred delivery jobs over local IPC
- validate job shape
- verify target pane readiness
- inject only after verification passes
- return or log success/failure

## 5. Picker

Responsibilities:

- provide interactive prompt selection
- return selected prompt ID or cancellation

For MVP this can wrap an external tool such as `fzf`, or a small builtin selector if implementation cost stays low.

## Data flow summary

### Direct send

1. CLI resolves ID -> prompt body
2. CLI resolves target tmux pane
3. CLI delivers immediately using tmux adapter

### Popup send

1. popup command resolves selection
2. popup command creates daemon job with target context
3. popup exits
4. daemon verifies pane context
5. daemon injects prompt body

## Architectural priorities

1. reliability over cleverness
2. clear failure modes
3. small and understandable internals
4. tmux-specific logic isolated behind adapter interfaces
