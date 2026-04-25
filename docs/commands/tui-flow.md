# TUI Flow

The signature workflow of `tprompt`: TUI → daemon → injection. For the TUI rendering, keybind layout, and search specifics, see `docs/commands/tui.md`.

The TUI typically runs inside a tmux popup (the signature launch path), but nothing in this flow requires that. The same flow works if the TUI is launched from any other terminal context, so long as the target pane is passed in.

## Goal

Allow the user to select a prompt **or** paste the clipboard from the TUI, then have the content delivered into the target pane after the TUI exits.

## Required behavior

1. `tprompt tui` (or bare `tprompt` when in tmux + tty) starts the TUI. In the signature launch, tmux has opened a popup around it.
2. It captures the original target context passed in from tmux (pane id, client tty, session id).
3. The built-in TUI presents the keybind board (pinned clipboard row + prompts).
4. On selection:
   - **Prompt row** — TUI resolves the prompt body and submits a `DeliveryRequest` with `source = "prompt"`.
   - **Clipboard row** — TUI reads the clipboard, validates it, and submits a `DeliveryRequest` with `source = "clipboard"` and the captured bytes in `body`. If validation fails, the TUI shows an inline error and stays open.
   - **Search match** — same as the corresponding row above.
5. TUI submits the job to the daemon and exits.
6. Daemon waits until tmux state indicates the target pane is again the intended active target.
7. Daemon runs the sanitizer over the request body.
8. Daemon injects via the tmux adapter.

Daemon auto-start is opt-in. By default, the TUI exits with a daemon/IPC error
when the configured daemon socket is unreachable. When `daemon_auto_start =
true` is configured, or `--daemon-auto-start` is passed to `tprompt tui`, the
TUI attempts one daemon start, waits briefly for readiness, then retries the
daemon preflight before rendering.

## Cancellation

If the user cancels (`Esc` or equivalent):

- no job is submitted
- TUI exits with **status 0**
- no delivery occurs

## Why the daemon is required

In the signature tmux-popup launch, tmux won't return focus to the target pane until the TUI process exits — so the TUI can't do the injection itself. A daemon accepts the job, waits for verified focus, then injects. A fixed sleep is brittle and should be avoided. See `docs/tmux/verification.md` for verification conditions.

## Required input context

The TUI flow should receive enough context to target the correct pane reliably.

Minimum recommended context:

- target pane ID
- client TTY if available
- session ID if available

## Clipboard in the TUI flow

The TUI process is responsible for reading the clipboard — not the daemon. Reasons:

- The user's clipboard can change between TUI exit and verification success (they might copy something else while waiting for focus to return). Capturing at keypress pins the content to the moment of intent.
- Keeps the daemon simpler: one code path for delivery regardless of source.

The TUI passes the captured bytes inside the `DeliveryRequest.body` field. The daemon treats clipboard-sourced and prompt-sourced jobs identically from that point on.

## Recommended tmux binding style

See `examples/tmux-bindings.md`.

## Failure behavior

If job submission fails:

- the TUI exits non-zero through the command error path
- the CLI surfaces the submit error on stderr with the normal exit-code mapping
- background retry logic after submission is outside the current contract

Inline TUI footer errors are reserved for recoverable, pre-submit choices such as empty clipboard content or an oversized prompt body. Once submission to the daemon has started, failures are not recoverable from inside the TUI.

## Concurrency and replacement

- Multiple TUI instances may be open simultaneously (singleton is not enforced — see DECISIONS.md §27).
- When a newly submitted job targets the same `pane_id` as a pending job, the pending job is **replaced**. See `docs/commands/daemon.md`.

## Verification before injection

See `docs/tmux/verification.md` for the exact expectations.

## Post-delivery failure feedback

If the daemon's verification or injection fails, the daemon surfaces the error via `tmux display-message` on the originating client plus the daemon log. The TUI is already gone at that point; in-tmux banner is the only user-facing feedback channel. See `docs/commands/daemon.md`.
