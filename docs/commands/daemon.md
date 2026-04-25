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

Current IPC mechanism:

- Unix domain socket

This is simple and appropriate for per-user local communication.

## Lifecycle

Current lifecycle behavior:

- daemon can be started explicitly (`tprompt daemon start`)
- daemon status can be checked explicitly (`tprompt daemon status`)
- daemon can be stopped gracefully (`tprompt daemon stop`)
- CLI auto-start is outside the current contract

`tprompt daemon stop` sends an explicit shutdown request over the daemon's
local socket. If no daemon is reachable, it reports `daemon not running`. If
shutdown is acknowledged but the socket remains reachable past the bounded
graceful wait, the command exits with a daemon/IPC error.

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

## Post-injection warning

By default, daemon delivery success is still defined by pre-injection verification plus successful tmux injection. Users can opt in to a post-injection diagnostic with:

```toml
post_injection_verification = true
```

When enabled, the daemon captures the target pane tail before and after successful delivery. If the tail appears unchanged, the daemon emits a warning via `tmux display-message` and the append-only log. This is warning-only: it does not prove the target application interpreted or ignored the input, and it does not turn a successful tmux delivery into a failure.

Capture failures also produce only warning diagnostics after otherwise successful delivery. The warning text does not include prompt bodies or clipboard bytes.

## Error feedback

Two channels, both always on:

### `tmux display-message`

On failure, the daemon runs `tmux display-message -c <originating-client-tty> "tprompt: <error>"` when `client_tty` is known. Otherwise it uses explicit `-t <target-pane|window|session>` fallback when a safe scope remains so the detached daemon does not rely on tmux's implicit current-client resolution. Example messages:

```text
tprompt: target pane %12 no longer exists
tprompt: verification timed out after 5s
tprompt: content rejected by sanitizer (mode=strict)
tprompt: replaced by a newer job — this delivery was dropped
tprompt: warning: post-injection verification: pane output appeared unchanged after delivery; this is a diagnostic warning, not proof that the target application ignored the input
```

The banner appears in the tmux status area of the originating client when `client_tty` is known. If `client_tty` is unavailable, the daemon targets the pane explicitly when it still exists; pane-missing failures without any remaining client/window/session scope are logged without broadcasting an unscoped banner.

### Append-only log

Default path: `~/.local/state/tprompt/daemon.log`.

Log entries are single-line logfmt records. Failure entries include timestamp, job ID, target pane, source, prompt ID when the source is a prompt, outcome, and message. Payload bodies are **never** logged — prompt bodies and clipboard bytes stay out of the log, and sanitizer rejections record only class + byte offset.

Example failure log entries:

```text
time=2026-04-16T12:30:45Z job_id=j-1 pane=%5 source=prompt prompt_id=code-review outcome=timeout msg="verification timed out after 5000ms"
time=2026-04-16T12:31:03Z job_id=j-2 pane=%5 source=clipboard outcome=delivery_error msg="tmux paste-buffer into %5 failed: tmux server died"
time=2026-04-16T12:31:19Z job_id=j-3 pane=%5 source=prompt prompt_id=code-review outcome=warning msg="post-injection verification: pane output appeared unchanged after delivery; this is a diagnostic warning, not proof that the target application ignored the input"
```

`tprompt daemon status` reports the running daemon in a deterministic, scan-friendly block:

```text
tprompt daemon
  pid:          12345
  socket:       /run/user/1000/tprompt/daemon.sock
  log:          /home/user/.local/state/tprompt/daemon.log
  uptime:       1h2m3s
  version:      0.1.0
  pending jobs: 0
```

Success is not logged by default. Post-injection verification is a warning-only diagnostic, not a confirmation mode.

## Persistence

No persisted queue is part of the current contract.

If the daemon dies, queued jobs may be lost. Document this clearly.

## Observability

At minimum, the daemon should provide enough output or logs to explain:

- why a job failed
- whether the target pane no longer existed
- whether timeout occurred before readiness
- whether tmux command execution failed
- whether the job was replaced by a newer one
- whether the sanitizer rejected content (mode + class + offset, not content)
