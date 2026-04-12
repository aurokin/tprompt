# Daemon

The daemon exists to support deferred and verified delivery after popup exit.

## Role

It is a local per-user process that:

- accepts delivery jobs over local IPC
- validates job payloads
- waits for verification conditions
- executes injection into tmux

## IPC

Recommended MVP IPC mechanism:

- Unix domain socket

This is simple and appropriate for per-user local communication.

## Lifecycle

Recommended MVP behavior:

- daemon can be started explicitly
- CLI can optionally auto-start it later, but this is not required for initial MVP

## Job handling

Each queued job should contain:

- job ID
- prompt ID
- prompt path
- prompt body
- mode
- press-enter flag
- target pane/session/client info
- verification policy

## Timeout behavior

The daemon should not wait forever by default.

Recommended behavior:

- bounded wait for verification
- clear timeout error if readiness never occurs

## Persistence

Not required for MVP.

If the daemon dies, queued jobs may be lost. Document this clearly.

## Observability

At minimum, the daemon should provide enough output or logs to explain:

- why a job failed
- whether the target pane no longer existed
- whether timeout occurred before readiness
- whether tmux command execution failed
