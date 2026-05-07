package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCooldownActiveWithinTTL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	cd := Cooldown{Until: now.Add(DefaultCooldownTTL), Reason: "spawn_failed", LogPath: "/tmp/d.log"}
	if err := RecordCooldown(p, cd); err != nil {
		t.Fatalf("RecordCooldown: %v", err)
	}
	got, active, err := ReadCooldown(p, func() time.Time { return now.Add(time.Second) })
	if err != nil {
		t.Fatalf("ReadCooldown: %v", err)
	}
	if !active {
		t.Fatal("cooldown should be active within TTL")
	}
	if got.Reason != cd.Reason || got.LogPath != cd.LogPath {
		t.Fatalf("cooldown roundtrip differs: got %+v want %+v", got, cd)
	}
}

func TestCooldownExpiredReadsAsInactive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	now := time.Now().UTC()
	if err := RecordCooldown(p, Cooldown{Until: now, Reason: "x"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	_, active, err := ReadCooldown(p, func() time.Time { return now.Add(time.Hour) })
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if active {
		t.Fatal("expired cooldown reports active=true")
	}
}

func TestCooldownMissingFileInactive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	_, active, err := ReadCooldown(p, time.Now)
	if err != nil {
		t.Fatalf("read missing: %v", err)
	}
	if active {
		t.Fatal("missing cooldown reports active=true")
	}
}

func TestClearCooldownIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	if err := ClearCooldown(p); err != nil {
		t.Fatalf("clear missing: %v", err)
	}
	if err := RecordCooldown(p, Cooldown{Until: time.Now().Add(time.Hour), Reason: "x"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := ClearCooldown(p); err != nil {
		t.Fatalf("clear present: %v", err)
	}
	if _, err := os.Stat(p.CooldownMark); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cooldown should be removed; stat err=%v", err)
	}
	if err := ClearCooldown(p); err != nil {
		t.Fatalf("second clear: %v", err)
	}
}

func TestCooldownFilePermissions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := PathsFor(filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	if err := RecordCooldown(p, Cooldown{Until: time.Now().Add(time.Hour), Reason: "x"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	info, err := os.Stat(p.CooldownMark)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("perm %o, want %o", got, want)
	}
}
