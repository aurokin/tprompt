package lifecycle

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestProductionSpawnerChildSurvivesContextCancellation guards the
// detached-spawn contract: the daemon child is meant to outlive the
// launcher invocation that started it. Earlier versions used
// exec.CommandContext, which installs a kill-on-ctx-done goroutine —
// so a caller-side cancellation (Ctrl-C while polling readiness, a
// cobra command deadline) would tear down the freshly-bound daemon.
//
// We spawn /bin/sleep, immediately cancel the ctx that was passed to
// Spawn, then assert the child is still alive after a brief grace
// period. If the bug regresses, processAlive returns false because
// CommandContext's goroutine SIGKILLed the child.
func TestProductionSpawnerChildSurvivesContextCancellation(t *testing.T) {
	t.Parallel()
	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep binary on PATH: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	handle, err := ProductionSpawner{}.Spawn(ctx, bin, []string{"5"}, "")
	if err != nil {
		cancel()
		t.Fatalf("Spawn: %v", err)
	}
	if handle.PID <= 0 {
		cancel()
		t.Fatalf("PID = %d, want > 0", handle.PID)
	}
	pid := handle.PID
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	})

	cancel()

	// Brief grace period so any kill-on-ctx-done goroutine (if the bug
	// were present) would have fired by now. 200ms is well above
	// scheduler latency on loaded CI hosts and well below the 5s sleep.
	time.Sleep(200 * time.Millisecond)

	if !processAlive(pid) {
		t.Fatal("detached child died after ctx cancel — Spawn is still binding child lifetime to caller's ctx")
	}
}

// TestProductionSpawnerHonorsPreForkContextCancellation confirms that
// a ctx already canceled before Spawn is called short-circuits without
// forking. ctx is no longer attached to the child's lifetime, but the
// pre-fork check still respects it so callers can abort cleanly.
func TestProductionSpawnerHonorsPreForkContextCancellation(t *testing.T) {
	t.Parallel()
	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep binary on PATH: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handle, err := ProductionSpawner{}.Spawn(ctx, bin, []string{"5"}, "")
	if err == nil {
		// Defensive: kill any child that did slip through so this test
		// doesn't leak processes if the contract is later relaxed.
		if handle.PID > 0 {
			_ = syscall.Kill(handle.PID, syscall.SIGKILL)
		}
		t.Fatal("Spawn returned nil err for a pre-canceled ctx; expected ctx.Err()")
	}
	if handle.PID != 0 {
		t.Errorf("PID = %d, want 0 on pre-canceled ctx", handle.PID)
	}
}
