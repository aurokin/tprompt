package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrIdentityNotOwned reports that the on-disk identity sidecar exists but
// does not match the caller (different pid OR different start_time). The
// caller did NOT clean up the sidecar.
var ErrIdentityNotOwned = errors.New("lifecycle: identity sidecar not owned by caller")

// Identity is the per-daemon ownership record written next to the socket.
// (pid, start_time) together defend against PID reuse: even if a future
// process happens to recycle the daemon's PID, its start time will not
// match.
type Identity struct {
	PID       int       `json:"pid"`
	StartTime time.Time `json:"start_time"`
	Exec      string    `json:"exec"`
	Socket    string    `json:"socket"`
	Log       string    `json:"log"`
	Version   string    `json:"version"`
}

// WriteIdentity writes id to p.Identity atomically: tmp file + rename into
// place. Mode 0o600. Parent dir created at 0o700. StartTime is normalized
// to UTC.
func WriteIdentity(p Paths, id Identity) error {
	if p.Identity == "" {
		return fmt.Errorf("lifecycle: empty identity path")
	}
	if err := os.MkdirAll(filepath.Dir(p.Identity), 0o700); err != nil {
		return fmt.Errorf("lifecycle: ensure parent: %w", err)
	}
	id.StartTime = id.StartTime.UTC()
	body, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return fmt.Errorf("lifecycle: marshal identity: %w", err)
	}
	body = append(body, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(p.Identity), filepath.Base(p.Identity)+".tmp-*")
	if err != nil {
		return fmt.Errorf("lifecycle: create identity tmp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("lifecycle: chmod identity tmp: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("lifecycle: write identity tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("lifecycle: close identity tmp: %w", err)
	}
	if err := os.Rename(tmpPath, p.Identity); err != nil {
		cleanup()
		return fmt.Errorf("lifecycle: rename identity tmp: %w", err)
	}
	return nil
}

// ReadIdentity reads and decodes the identity sidecar. Returns
// os.ErrNotExist (wrappable via errors.Is) when the file is missing.
func ReadIdentity(p Paths) (Identity, error) {
	if p.Identity == "" {
		return Identity{}, fmt.Errorf("lifecycle: empty identity path")
	}
	data, err := os.ReadFile(p.Identity)
	if err != nil {
		return Identity{}, err
	}
	var id Identity
	if err := json.Unmarshal(data, &id); err != nil {
		return Identity{}, fmt.Errorf("lifecycle: decode identity %s: %w", p.Identity, err)
	}
	id.StartTime = id.StartTime.UTC()
	return id, nil
}

// RemoveIdentityIfOwned removes the sidecar only when the on-disk identity
// matches owner by (pid, start_time). Missing file → nil. Mismatch →
// ErrIdentityNotOwned (the caller did NOT clean up).
func RemoveIdentityIfOwned(p Paths, owner Identity) error {
	current, err := ReadIdentity(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if current.PID != owner.PID || !current.StartTime.Equal(owner.StartTime.UTC()) {
		return fmt.Errorf("%w: on-disk pid=%d start=%s, owner pid=%d start=%s",
			ErrIdentityNotOwned, current.PID, current.StartTime.Format(time.RFC3339Nano),
			owner.PID, owner.StartTime.UTC().Format(time.RFC3339Nano))
	}
	if err := os.Remove(p.Identity); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("lifecycle: remove identity: %w", err)
	}
	return nil
}
