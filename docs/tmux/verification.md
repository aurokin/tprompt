# Verification

This file defines what “verified delivery” means for MVP.

## Goal

Avoid timer-based delivery for TUI-flow jobs.

## Pre-injection checks

Before injecting a deferred TUI-flow job, the daemon should verify as much of the following as practical:

### 1. Target pane still exists

If the target pane disappeared, fail the job.

### 2. TUI process has exited

The daemon should only deliver after the TUI command is gone.

When the TUI is launched inside a tmux popup, this can be approximated indirectly via the client returning to the target pane (tmux only returns focus after the popup command exits).

### 3. The target pane is again the selected or intended target

Preferred check:

- originating client/session is back on the target pane

Fallback acceptable for MVP if client-scoped verification is difficult:

- verify that the target pane is now the selected pane in its session/window context

Document exactly which verification level was implemented.

## Post-injection check

Optional but recommended for MVP:

- capture pane tail before injection
- inject content
- capture pane tail after injection
- verify the captured text changed

This does **not** prove semantic success. It only proves that the pane output changed after delivery.

## Timeout guidance

Verification should use polling with a bounded timeout, not a blind sleep.

Example strategy:

- poll tmux state every 50–150 ms
- stop when verification passes or timeout is hit

## Failure cases that must be explicit

- target pane vanished
- verification timed out
- tmux command failed
- selected pane did not return to the intended target

## Important contract

MVP verification is about **tmux state readiness**, not target-application readiness.
