# Daemon

The daemon exists to support deferred and verified delivery after the TUI process exits.

## Role

It is a local per-user process that:

- accepts delivery jobs over local IPC
- validates job payloads
- waits for verification conditions
- executes injection into tmux
- surfaces failures via `tmux display-message` + append-only log

## IPC

Recommended MVP IPC mechanism:

- Unix domain socket

This is simple and appropriate for per-user local communication.

## Lifecycle

Recommended MVP behavior:

- daemon can be started explicitly (`tprompt daemon start`)
- CLI can optionally auto-start it later, but this is not required for initial MVP

## Job handling

Each queued job contains a `DeferredJob` with a `DeliveryRequest` inside. See `docs/architecture/data-model.md`.

Required per-job fields:

- job ID
- source (`prompt` | `clipboard`)
- prompt ID + path (when applicable)
- body (pre-sanitization)
- mode (`paste` | `type`)
- press-enter flag
- sanitize mode
- delivery pane ID
- originating session/window/client info (when known)
- verification policy

## Replacement semantics

When a new job arrives for the **same `pane_id`** as a job already pending verification, the daemon keeps only the newest pending work for that pane. A running worker is cancelled and allowed to exit before the replacement starts; a not-yet-started replacement is discarded immediately in favor of the newest arrival. The logged reason is `replaced by job <new-id>`.

Rationale: this matches user intent ("I changed my mind and picked something else"). Queuing both would deliver unwanted content.

Different-pane targets are independent and proceed in parallel (or serialized — implementation choice) without replacement.

## Multiple TUI instances

The daemon does **not** enforce a TUI singleton. Any TUI instance (including multiple tmux popups running `tprompt`) may submit a job. If two instances submit nearly simultaneously for the same pane, the last-arriving job wins via the replacement rule above.

## Direct sends

Direct sends (`tprompt send <id>` and `tprompt paste` invoked outside the TUI flow) do **not** use the daemon. They go straight through the tmux adapter in the CLI process. The daemon cannot block or interfere with them.

## Timeout behavior

The daemon does not wait forever by default.

Recommended behavior:

- bounded wait for verification (`verification_timeout_ms` from config, default 5000 ms)
- clear timeout error if readiness never occurs
- timed-out jobs surface via `display-message` + log

## Error feedback

Two channels, both always on:

### `tmux display-message`

On failure, the daemon runs `tmux display-message -c <originating-client-tty> "tprompt: <error>"` when `client_tty` is known. Otherwise it uses explicit `-t <target-pane|window|session>` fallback when a safe scope remains so the detached daemon does not rely on tmux's implicit current-client resolution. Example messages:

```text
tprompt: target pane %12 no longer exists
tprompt: verification timed out after 5s
tprompt: content rejected by sanitizer (mode=strict)
tprompt: replaced by a newer job — this delivery was dropped
```

The banner appears in the tmux status area of the originating client when `client_tty` is known. If `client_tty` is unavailable, the daemon targets the pane explicitly when it still exists; pane-missing failures without any remaining client/window/session scope are logged without broadcasting an unscoped banner.

### Append-only log

Default path: `~/.local/state/tprompt/daemon.log`.

Log entries include job ID, timestamp, target pane, failure class, and message. Payload bodies are **never** logged — sanitizer rejections record only class + byte offset.

Success is not logged by default. Users who want confirmation can enable a verbose flag or future `confirm_delivery = true` setting (not in MVP).

## Persistence

Not required for MVP.

If the daemon dies, queued jobs may be lost. Document this clearly.

## Observability

At minimum, the daemon should provide enough output or logs to explain:

- why a job failed
- whether the target pane no longer existed
- whether timeout occurred before readiness
- whether tmux command execution failed
- whether the job was replaced by a newer one
- whether the sanitizer rejected content (mode + class + offset, not content)
