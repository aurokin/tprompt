package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Primary assertion: locks open with O_CLOEXEC so the FD_CLOEXEC flag is
// set on the underlying fd. fcntl(F_GETFD) reports the flag directly.
//
// Why this matters: when the daemon parent (holding the run lock during a
// future inline-spawn path, or holding the start lock during a parent-side
// auto-start) forks-execs a child, the kernel must close the inherited fd
// on exec. Without O_CLOEXEC, the child would still hold an OFD reference
// to the lock, and dropping the parent's ref would not release the kernel
// flock.
func TestRunLockOpensCLOEXEC(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	l, err := AcquireRunLock(p)
	if err != nil {
		t.Fatalf("AcquireRunLock: %v", err)
	}
	defer func() { _ = l.Release() }()
	assertCLOEXEC(t, l.f.Fd())
}

func TestStartLockOpensCLOEXEC(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	l, err := AcquireStartLock(p)
	if err != nil {
		t.Fatalf("AcquireStartLock: %v", err)
	}
	defer func() { _ = l.Release() }()
	assertCLOEXEC(t, l.f.Fd())
}

// assertCLOEXEC reads FD_CLOEXEC via fcntl and fails the test if it is
// missing. We bypass syscall.GetsockoptInt-style helpers because
// SYS_FCNTL is the portable way to read FD_CLOEXEC and Go's stdlib does
// not expose F_GETFD.
func assertCLOEXEC(t *testing.T, fd uintptr) {
	t.Helper()
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, uintptr(syscall.F_GETFD), 0)
	if errno != 0 {
		t.Fatalf("fcntl(F_GETFD) errno=%v", errno)
	}
	if flags&syscall.FD_CLOEXEC == 0 {
		t.Fatalf("fd %d does not have FD_CLOEXEC set: flags=%#x", fd, flags)
	}
}

// Sanity check: the lockfile is created with mode 0o600 (modulo umask).
// Run after AcquireRunLock so the file definitely exists.
func TestRunLockFilePermissions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	l, err := AcquireRunLock(p)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = l.Release() }()
	info, err := os.Stat(p.RunLock)
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lockfile missing after acquire: %v", err)
	}
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("perm %o, want %o", got, want)
	}
}
