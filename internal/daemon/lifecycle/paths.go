// Package lifecycle owns daemon ownership primitives that enforce
// one-daemon-per-socket: the run lock the daemon process holds, the start
// lock that serializes parent-side cold starts, the identity sidecar that
// records who owns the socket, and a short-lived cooldown marker for
// repeated implicit-start failures.
//
// All sidecar files live next to the configured socket using deterministic
// suffixes derived from the canonical socket path. The package is pure: it
// does not import internal/daemon or internal/app, and it does not know
// about cobra, CLI flags, or tmux. Callers wire it from internal/daemon
// (run lock + identity write/remove during Listen/Close) and from the
// future internal/app/lifecycle launcher (start lock + cooldown).
package lifecycle

import (
	"fmt"
	"path/filepath"
)

// Paths records the lifecycle sidecar paths derived from a configured socket
// path. The fields are deterministic suffixes on the canonical absolute
// socket path so that two callers using equivalent relative/absolute forms
// agree on the same sidecar set.
type Paths struct {
	Socket       string
	RunLock      string
	StartLock    string
	Identity     string
	CooldownMark string
}

// PathsFor canonicalizes socket via filepath.Abs+Clean and derives the
// sidecar paths. Symlinks are not resolved — sidecars may be created in
// directories that do not yet exist, and lstat would mask the parent
// missing case. Callers that need symlink-stable paths must canonicalize
// upstream.
func PathsFor(socket string) (Paths, error) {
	if socket == "" {
		return Paths{}, fmt.Errorf("lifecycle: socket path is empty")
	}
	abs, err := filepath.Abs(socket)
	if err != nil {
		return Paths{}, fmt.Errorf("lifecycle: canonicalize socket %q: %w", socket, err)
	}
	abs = filepath.Clean(abs)
	return Paths{
		Socket:       abs,
		RunLock:      abs + ".lock",
		StartLock:    abs + ".start.lock",
		Identity:     abs + ".identity.json",
		CooldownMark: abs + ".start.cooldown",
	}, nil
}
