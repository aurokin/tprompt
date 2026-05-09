package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestWriteReadIdentityRoundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	id := Identity{
		PID:       4242,
		StartTime: time.Date(2026, 5, 6, 12, 30, 45, 0, time.UTC),
		Exec:      "/usr/local/bin/tprompt",
		Socket:    p.Socket,
		Log:       "/tmp/daemon.log",
		Version:   "0.1.0",
	}
	if err := WriteIdentity(p, id); err != nil {
		t.Fatalf("WriteIdentity: %v", err)
	}
	got, err := ReadIdentity(p)
	if err != nil {
		t.Fatalf("ReadIdentity: %v", err)
	}
	if got.PID != id.PID || got.Exec != id.Exec || got.Socket != id.Socket || got.Log != id.Log || got.Version != id.Version {
		t.Fatalf("Identity roundtrip differs: got %+v want %+v", got, id)
	}
	if !got.StartTime.Equal(id.StartTime) {
		t.Fatalf("StartTime mismatch: got %s want %s", got.StartTime, id.StartTime)
	}
}

func TestWriteIdentityIsAtomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	idA := Identity{PID: 1, StartTime: time.Now().UTC(), Exec: "a", Socket: p.Socket, Log: "la", Version: "v1"}
	idB := Identity{PID: 2, StartTime: time.Now().UTC().Add(time.Second), Exec: "b", Socket: p.Socket, Log: "lb", Version: "v2"}
	if err := WriteIdentity(p, idA); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			got, err := ReadIdentity(p)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				t.Errorf("concurrent read: %v", err)
				return
			}
			if got.PID != 1 && got.PID != 2 {
				t.Errorf("partial identity observed: %+v", got)
				return
			}
		}
	}()
	for i := 0; i < 100; i++ {
		var w Identity
		if i%2 == 0 {
			w = idA
		} else {
			w = idB
		}
		if err := WriteIdentity(p, w); err != nil {
			t.Fatalf("write iter %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}

func TestRemoveIdentityIfOwnedMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	owner := Identity{PID: 9, StartTime: time.Now().UTC().Truncate(time.Second), Exec: "x", Socket: p.Socket, Log: "l", Version: "v"}
	if err := WriteIdentity(p, owner); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := RemoveIdentityIfOwned(p, owner); err != nil {
		t.Fatalf("remove owned: %v", err)
	}
	if _, err := os.Stat(p.Identity); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("identity should be removed; stat err=%v", err)
	}
}

func TestRemoveIdentityIfOwnedMismatchPID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	onDisk := Identity{PID: 1, StartTime: time.Now().UTC().Truncate(time.Second), Exec: "x", Socket: p.Socket, Log: "l", Version: "v"}
	caller := Identity{PID: 2, StartTime: onDisk.StartTime, Exec: "x", Socket: p.Socket, Log: "l", Version: "v"}
	if err := WriteIdentity(p, onDisk); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := RemoveIdentityIfOwned(p, caller); !errors.Is(err, ErrIdentityNotOwned) {
		t.Fatalf("err = %v, want errors.Is ErrIdentityNotOwned", err)
	}
	// File must still be there — caller did NOT clean up.
	if _, err := os.Stat(p.Identity); err != nil {
		t.Fatalf("identity should remain on mismatch: %v", err)
	}
}

func TestRemoveIdentityIfOwnedMismatchStartTime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	onDisk := Identity{PID: 7, StartTime: time.Now().UTC().Truncate(time.Second), Exec: "x", Socket: p.Socket, Log: "l", Version: "v"}
	caller := Identity{PID: 7, StartTime: onDisk.StartTime.Add(time.Hour), Exec: "x", Socket: p.Socket, Log: "l", Version: "v"}
	if err := WriteIdentity(p, onDisk); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := RemoveIdentityIfOwned(p, caller); !errors.Is(err, ErrIdentityNotOwned) {
		t.Fatalf("err = %v, want errors.Is ErrIdentityNotOwned", err)
	}
}

func TestRemoveIdentityIfOwnedMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	owner := Identity{PID: 1, StartTime: time.Now().UTC().Truncate(time.Second)}
	if err := RemoveIdentityIfOwned(p, owner); err != nil {
		t.Fatalf("missing file should be nil, got %v", err)
	}
}

func TestIdentityFilePermissions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	id := Identity{PID: 1, StartTime: time.Now().UTC(), Exec: "x", Socket: p.Socket}
	if err := WriteIdentity(p, id); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(p.Identity)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("perm %o, want %o", got, want)
	}
}
