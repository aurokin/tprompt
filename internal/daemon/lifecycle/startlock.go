package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// StartLock is a blocking exclusive flock on Paths.StartLock used by parent
// processes (`daemon start`, TUI implicit auto-start) to serialize cold
// starts. The daemon child does NOT hold this lock; it grabs the run lock
// instead.
type StartLock struct {
	f *os.File
}

// AcquireStartLock opens p.StartLock with O_CREATE|O_RDWR|O_CLOEXEC at
// 0o600 and grabs an exclusive blocking flock. Returns when the lock is
// owned by this caller. Parent directories are created at 0o700.
func AcquireStartLock(p Paths) (*StartLock, error) {
	if p.StartLock == "" {
		return nil, fmt.Errorf("lifecycle: empty start lock path")
	}
	if err := os.MkdirAll(filepath.Dir(p.StartLock), 0o700); err != nil {
		return nil, fmt.Errorf("lifecycle: ensure parent: %w", err)
	}
	f, err := os.OpenFile(p.StartLock, os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: open start lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lifecycle: flock start lock: %w", err)
	}
	return &StartLock{f: f}, nil
}

// Release drops the flock and closes the underlying file. Idempotent.
func (l *StartLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}
