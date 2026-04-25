# Verification

This file defines what "verified delivery" means for `tprompt`.

## Goal

Avoid timer-based delivery for TUI-flow jobs.

## Pre-injection checks

Before injecting a deferred TUI-flow job, the daemon should verify as much of the following as practical:

### 1. Target pane still exists

If the target pane disappeared, fail the job.

### 2. TUI process has exited

The daemon should only deliver after the TUI command is gone.

When the TUI is launched inside a tmux popup, this can be approximated indirectly via the client returning to the target pane (tmux only returns focus after the popup command exits).
When the submitting process PID is known, the daemon should prefer that direct signal over pane-selection heuristics and wait for the submitting process to exit before delivering.

### 3. The target pane is again the selected or intended target

Preferred check:

- originating client/session is back on the target pane

Fallback acceptable when the submitter PID is unavailable:

- verify that the target pane is now the selected pane in its session/window context

Implemented contract:

- always verify the target pane still exists
- if the job includes the submitter PID, wait for that process to exit
- then verify the target pane is selected before delivery

## Post-injection check

The daemon can run an opt-in diagnostic check after injection:

- capture pane tail before injection
- inject content
- capture pane tail after injection
- warn if the captured text appears unchanged

This is disabled by default and enabled with `post_injection_verification = true`.

This does **not** prove semantic success. A changed tail only proves that pane output changed after delivery. An unchanged tail is a warning, not a delivery failure, and capture failures do not turn an otherwise successful delivery into a failed delivery.

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

Verification is about **tmux state readiness**, not target-application readiness.
