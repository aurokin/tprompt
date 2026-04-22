package daemon

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/hsadler/tprompt/internal/tmux"
)

// Verify polls the tmux adapter until the target pane exists and is again the
// foreground-selected pane for delivery, or the policy timeout elapses. When submitterPID
// is known (for example, the popup-hosted TUI process), the process must also
// have exited before delivery is considered ready. Each iteration runs:
//
//  1. PaneExists — a missing pane is a hard failure (PaneMissingError), not
//     a state we wait through.
//  2. submitterExited — when known, the submitting process must be gone so
//     delivery cannot race ahead of popup teardown.
//  3. IsTargetSelected — the tmux state signal that the target pane is again
//     the intended delivery target.
//
// Cancellation via ctx (used by replace-same-target) returns ctx.Err().
// On timeout returns *TimeoutError so the executor can surface a precise
// banner.
func Verify(
	ctx context.Context,
	adapter tmux.Adapter,
	target tmux.TargetContext,
	policy VerificationPolicy,
	submitterPID int,
) error {
	if err := policy.validate(); err != nil {
		return err
	}
	deadline := time.Now().Add(time.Duration(policy.TimeoutMS) * time.Millisecond)
	timer := newStoppedTimer()
	defer timer.Stop()

	for {
		if _, err := remainingUntil(deadline, policy.TimeoutMS); err != nil {
			return err
		}

		ready, err := probeReadyUntil(ctx, deadline, adapter, target, submitterPID, policy.TimeoutMS)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		remaining, err := remainingUntil(deadline, policy.TimeoutMS)
		if err != nil {
			return err
		}
		if err := policy.wait(ctx, timer, remaining); err != nil {
			return err
		}
	}
}

func newStoppedTimer() *time.Timer {
	// Reuse a single timer across iterations to avoid the per-iteration
	// allocation of time.After. policy.wait handles the stop+drain+reset
	// idiom so this loop stays flat.
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	return timer
}

func remainingUntil(deadline time.Time, timeoutMS int) (time.Duration, error) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, &TimeoutError{TimeoutMS: timeoutMS}
	}
	return remaining, nil
}

func probeReadyUntil(
	ctx context.Context,
	deadline time.Time,
	adapter tmux.Adapter,
	target tmux.TargetContext,
	submitterPID int,
	timeoutMS int,
) (bool, error) {
	probeCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	ready, err := pollReady(probeCtx, adapter, target, submitterPID)
	if err == nil {
		return ready, nil
	}
	return false, normalizeProbeError(ctx, err, timeoutMS)
}

func normalizeProbeError(ctx context.Context, err error, timeoutMS int) error {
	if !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &TimeoutError{TimeoutMS: timeoutMS}
}

// pollReady runs one verification iteration: the pane must exist (missing
// is a hard failure), the popup-driving submitter process must be gone when
// known, and the pane must be foreground-selected. Returns (true, nil) when
// the pane is ready to receive delivery.
func pollReady(ctx context.Context, adapter tmux.Adapter, target tmux.TargetContext, submitterPID int) (bool, error) {
	exists, err := adapter.PaneExists(ctx, target.PaneID)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, &tmux.PaneMissingError{PaneID: target.PaneID}
	}
	if submitterPID > 0 {
		alive, err := processRunning(submitterPID)
		if err != nil {
			return false, fmt.Errorf("check submitter pid %d: %w", submitterPID, err)
		}
		if alive {
			return false, nil
		}
	}
	return adapter.IsTargetSelected(ctx, target)
}

func (p VerificationPolicy) validate() error {
	if p.TimeoutMS <= 0 {
		return &InvalidPolicyError{Field: "timeout_ms", Value: p.TimeoutMS}
	}
	if p.PollIntervalMS <= 0 {
		return &InvalidPolicyError{Field: "poll_interval_ms", Value: p.PollIntervalMS}
	}
	return nil
}

// wait reuses timer to sleep for at most one poll interval, capped by the
// remaining verification deadline, or returns ctx.Err() on cancellation.
// Stop+drain before Reset is the documented idiom for a timer that may have
// already fired in a prior iteration.
func (p VerificationPolicy) wait(ctx context.Context, timer *time.Timer, remaining time.Duration) error {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	sleep := time.Duration(p.PollIntervalMS) * time.Millisecond
	if remaining < sleep {
		sleep = remaining
	}
	timer.Reset(sleep)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var processRunning = unixProcessRunning

func unixProcessRunning(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.ESRCH):
		return false, nil
	case errors.Is(err, syscall.EPERM):
		return true, nil
	default:
		return false, err
	}
}
