# Popup Flow

This is the signature workflow of `tprompt`.

## Goal

Allow the user to select a prompt from a tmux popup, then inject it into the pane they were using before the popup appeared.

## Required behavior

1. `tprompt popup` starts inside a tmux popup.
2. It captures the original target context passed in from tmux.
3. It presents an interactive picker.
4. On selection, it submits a deferred job to the daemon.
5. It exits.
6. The daemon waits until tmux state indicates the original pane is again the intended active target.
7. The daemon injects the prompt.

## Why the daemon is required

Without a daemon, the popup process would need to guess when tmux returned focus to the original pane. A fixed sleep is brittle and should be avoided.

## Required input context

The popup flow should receive enough context to target the original pane reliably.

Minimum recommended context:

- original pane ID
- client TTY if available
- session ID if available

## Recommended tmux binding style

See `examples/tmux-bindings.md`.

## Cancellation behavior

If the user cancels prompt selection:

- no job is submitted
- popup exits cleanly
- no delivery occurs

## Failure behavior

If job submission fails:

- popup should display a clear error and exit non-zero
- no background retry logic is required for MVP

## Verification before injection

See `docs/tmux/verification.md` for the exact expectations.
