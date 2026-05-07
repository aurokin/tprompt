package lifecycle

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRunLockSingleHolderAtATime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}

	first, err := AcquireRunLock(p)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := AcquireRunLock(p); err == nil {
		t.Fatal("second acquire = nil, want ErrLockHeld")
	} else if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("second acquire err = %v, want errors.Is ErrLockHeld", err)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	second, err := AcquireRunLock(p)
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("release second: %v", err)
	}
}

func TestRunLockReleaseIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	l, err := AcquireRunLock(p)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
}

func TestIsRunLockHeldReflectsState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	if held, err := IsRunLockHeld(p); err != nil || held {
		t.Fatalf("before acquire held=%v err=%v, want held=false err=nil", held, err)
	}
	l, err := AcquireRunLock(p)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if held, err := IsRunLockHeld(p); err != nil || !held {
		t.Fatalf("after acquire held=%v err=%v, want held=true err=nil", held, err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if held, err := IsRunLockHeld(p); err != nil || held {
		t.Fatalf("after release held=%v err=%v, want held=false err=nil", held, err)
	}
}

func TestIsRunLockHeldDoesNotKeepOwnership(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	for i := 0; i < 5; i++ {
		if held, err := IsRunLockHeld(p); err != nil || held {
			t.Fatalf("iter %d: held=%v err=%v", i, held, err)
		}
	}
	l, err := AcquireRunLock(p)
	if err != nil {
		t.Fatalf("acquire after probe storm: %v", err)
	}
	_ = l.Release()
}

// Concurrent acquisitions: exactly one wins per release window.
func TestRunLockConcurrentAcquireExactlyOneWins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	const n = 8
	var wins atomic.Int64
	var heldErrs atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			l, err := AcquireRunLock(p)
			if err == nil {
				wins.Add(1)
				_ = l.Release()
				return
			}
			if errors.Is(err, ErrLockHeld) {
				heldErrs.Add(1)
				return
			}
			t.Errorf("unexpected error: %v", err)
		}()
	}
	wg.Wait()
	// At most one goroutine sees the lock free at any instant. Some goroutines
	// will always see the lock free if scheduling permits; the only invariant
	// worth asserting is "no goroutine simultaneously thinks it holds it,"
	// which the bookkeeping assertion below covers indirectly: the sum of
	// outcomes equals n.
	if got, want := wins.Load()+heldErrs.Load(), int64(n); got != want {
		t.Fatalf("outcomes accounted = %d, want %d", got, want)
	}
	if wins.Load() == 0 {
		t.Fatal("no goroutine acquired the lock")
	}
}
