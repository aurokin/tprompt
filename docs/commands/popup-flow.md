# Popup Flow

The signature workflow of `tprompt`. This file covers the deferred-delivery flow (popup → daemon → injection). For the TUI rendering, keybind layout, and search specifics, see `docs/commands/popup-ui.md`.

## Goal

Allow the user to select a prompt **or** paste the clipboard from a tmux popup, then have the content delivered into the pane they were using before the popup appeared.

## Required behavior

1. `tprompt popup` starts inside a tmux popup.
2. It captures the original target context passed in from tmux (pane id, client tty, session id).
3. The built-in TUI presents the keybind board (pinned clipboard row + prompts).
4. On selection:
   - **Prompt row** — TUI resolves the prompt body and submits a `DeliveryRequest` with `source = "prompt"`.
   - **Clipboard row** — TUI reads the clipboard, validates it, and submits a `DeliveryRequest` with `source = "clipboard"` and the captured bytes in `body`. If validation fails, the popup shows an inline error and stays open.
   - **Search match** — same as the corresponding row above.
5. Popup submits the job to the daemon and exits.
6. Daemon waits until tmux state indicates the original pane is again the intended active target.
7. Daemon runs the sanitizer over the request body.
8. Daemon injects via the tmux adapter.

## Cancellation

If the user cancels (`Esc` or equivalent):

- no job is submitted
- popup exits with **status 0**
- no delivery occurs

## Why the daemon is required

Without a daemon, the popup process would need to guess when tmux returned focus to the original pane. A fixed sleep is brittle and should be avoided. See `docs/tmux/verification.md` for verification conditions.

## Required input context

The popup flow should receive enough context to target the original pane reliably.

Minimum recommended context:

- original pane ID
- client TTY if available
- session ID if available

## Clipboard in the popup flow

The popup process is responsible for reading the clipboard — not the daemon. Reasons:

- The user's clipboard can change between popup close and verification success (they might copy something else while waiting for focus to return). Capturing at keypress pins the content to the moment of intent.
- Keeps the daemon simpler: one code path for delivery regardless of source.

The popup passes the captured bytes inside the `DeliveryRequest.body` field. The daemon treats clipboard-sourced and prompt-sourced jobs identically from that point on.

## Recommended tmux binding style

See `examples/tmux-bindings.md`.

## Failure behavior

If job submission fails:

- popup displays a clear error and exits non-zero
- no background retry logic is required for MVP

## Concurrency and replacement

- Multiple popups may be open simultaneously (singleton is not enforced — see DECISIONS.md §27).
- When a newly submitted job targets the same `pane_id` as a pending job, the pending job is **replaced**. See `docs/commands/daemon.md`.

## Verification before injection

See `docs/tmux/verification.md` for the exact expectations.

## Post-delivery failure feedback

If the daemon's verification or injection fails, the daemon surfaces the error via `tmux display-message` on the originating client plus the daemon log. The popup is already gone at that point; in-tmux banner is the only user-facing feedback channel. See `docs/commands/daemon.md`.
