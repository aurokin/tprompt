package lifecycle

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPathsForRelativeAndAbsoluteAgree(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rel := filepath.Join("relative", "daemon.sock")
	abs := filepath.Join(dir, "daemon.sock")

	relP, err := PathsFor(rel)
	if err != nil {
		t.Fatalf("PathsFor relative: %v", err)
	}
	absP, err := PathsFor(abs)
	if err != nil {
		t.Fatalf("PathsFor abs: %v", err)
	}

	if !filepath.IsAbs(relP.Socket) {
		t.Fatalf("relative socket not canonicalized: %q", relP.Socket)
	}
	if !filepath.IsAbs(absP.Socket) {
		t.Fatalf("abs socket not canonicalized: %q", absP.Socket)
	}
	if !strings.HasSuffix(relP.RunLock, ".sock.lock") {
		t.Fatalf("RunLock suffix wrong: %q", relP.RunLock)
	}
	if !strings.HasSuffix(absP.StartLock, ".sock.start.lock") {
		t.Fatalf("StartLock suffix wrong: %q", absP.StartLock)
	}
	if !strings.HasSuffix(absP.Identity, ".sock.identity.json") {
		t.Fatalf("Identity suffix wrong: %q", absP.Identity)
	}
	if !strings.HasSuffix(absP.CooldownMark, ".sock.start.cooldown") {
		t.Fatalf("CooldownMark suffix wrong: %q", absP.CooldownMark)
	}
}

func TestPathsForStableAcrossExtensionShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		socket  string
		runLock string
	}{
		{"extension", "daemon.sock", "daemon.sock.lock"},
		{"no-extension", "daemonsocket", "daemonsocket.lock"},
		{"already-suffixed", "daemon.sock.lock", "daemon.sock.lock.lock"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			abs, err := filepath.Abs(tc.socket)
			if err != nil {
				t.Fatalf("filepath.Abs: %v", err)
			}
			p, err := PathsFor(tc.socket)
			if err != nil {
				t.Fatalf("PathsFor: %v", err)
			}
			if got, want := p.RunLock, abs+".lock"; got != want {
				t.Errorf("RunLock = %q, want %q", got, want)
			}
		})
	}
}

func TestPathsForRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := PathsFor(""); err == nil {
		t.Fatal("PathsFor(\"\") = nil error, want error")
	}
}

func TestPathsForCleansDoubleSlashes(t *testing.T) {
	t.Parallel()
	p1, err := PathsFor("/tmp//tprompt/daemon.sock")
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	p2, err := PathsFor("/tmp/tprompt/daemon.sock")
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("PathsFor did not canonicalize: %+v vs %+v", p1, p2)
	}
}
