package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLockHeld reports that a lifecycle lock is currently held by another
// process. Callers detect this with errors.Is.
var ErrLockHeld = errors.New("lifecycle: lock held by another process")

// RunLock is an exclusive flock on Paths.RunLock that the daemon process
// holds for its lifetime. Two daemons cannot acquire it simultaneously, so
// the second `tprompt daemon run` for the same socket fails fast instead
// of racing on socket bind.
type RunLock struct {
	f *os.File
}

// AcquireRunLock opens p.RunLock with O_CREATE|O_RDWR|O_CLOEXEC at 0o600
// and grabs an exclusive non-blocking flock. If another process already
// holds it, returns an error wrapping ErrLockHeld. Parent directories are
// created at 0o700.
func AcquireRunLock(p Paths) (*RunLock, error) {
	if p.RunLock == "" {
		return nil, fmt.Errorf("lifecycle: empty run lock path")
	}
	if err := os.MkdirAll(filepath.Dir(p.RunLock), 0o700); err != nil {
		return nil, fmt.Errorf("lifecycle: ensure parent: %w", err)
	}
	f, err := os.OpenFile(p.RunLock, os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: open run lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", ErrLockHeld, p.RunLock)
		}
		return nil, fmt.Errorf("lifecycle: flock run lock: %w", err)
	}
	return &RunLock{f: f}, nil
}

// Release drops the flock and closes the underlying file. Idempotent.
func (l *RunLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	// Closing the fd releases flock per POSIX; explicit unlock is belt-
	// and-suspenders for early-failure paths.
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}

// IsRunLockHeld probes whether some other process currently holds the run
// lock. The probe is non-mutating: a missing lockfile reports held=false
// without creating one, and a successful test acquire is immediately
// released so the caller does not inadvertently take ownership. Used by
// callers (e.g. doctor, the future launcher's compatibility check) that
// want to know "is a daemon alive" without touching ownership state.
func IsRunLockHeld(p Paths) (bool, error) {
	if p.RunLock == "" {
		return false, fmt.Errorf("lifecycle: empty run lock path")
	}
	f, err := os.OpenFile(p.RunLock, os.O_RDWR|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("lifecycle: probe run lock: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return true, nil
		}
		return false, fmt.Errorf("lifecycle: probe flock: %w", err)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}
