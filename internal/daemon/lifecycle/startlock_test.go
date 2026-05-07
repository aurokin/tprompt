package lifecycle

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartLockSerializesCallers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}

	const n = 6
	var inCritical atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			l, err := AcquireStartLock(p)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			cur := inCritical.Add(1)
			for {
				m := maxConcurrent.Load()
				if cur <= m || maxConcurrent.CompareAndSwap(m, cur) {
					break
				}
			}
			// Hold briefly so contention is observable but tests stay fast.
			time.Sleep(2 * time.Millisecond)
			inCritical.Add(-1)
			if err := l.Release(); err != nil {
				t.Errorf("release: %v", err)
			}
		}()
	}
	wg.Wait()
	if max := maxConcurrent.Load(); max != 1 {
		t.Fatalf("observed %d concurrent holders, want 1", max)
	}
}

func TestStartLockReacquireAfterRelease(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	first, err := AcquireStartLock(p)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	second, err := AcquireStartLock(p)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("release second: %v", err)
	}
}
