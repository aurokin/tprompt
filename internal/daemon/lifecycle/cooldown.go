package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultCooldownTTL is the recommended cooldown duration recorded after a
// failed implicit start. Callers may pass a different Until time.
const DefaultCooldownTTL = 10 * time.Second

// Cooldown is a short-lived marker recorded after a failed implicit
// auto-start so repeated TUI invocations do not retry the spawn or
// platform trust assessment until Until passes. Explicit lifecycle
// commands (`daemon start`, `daemon run`) bypass and must call
// ClearCooldown on success.
type Cooldown struct {
	Until   time.Time `json:"until"`
	Reason  string    `json:"reason"`
	LogPath string    `json:"log_path,omitempty"`
}

// RecordCooldown writes c atomically at 0o600 to p.CooldownMark.
func RecordCooldown(p Paths, c Cooldown) error {
	if p.CooldownMark == "" {
		return fmt.Errorf("lifecycle: empty cooldown path")
	}
	if err := os.MkdirAll(filepath.Dir(p.CooldownMark), 0o700); err != nil {
		return fmt.Errorf("lifecycle: ensure parent: %w", err)
	}
	c.Until = c.Until.UTC()
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("lifecycle: marshal cooldown: %w", err)
	}
	body = append(body, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(p.CooldownMark), filepath.Base(p.CooldownMark)+".tmp-*")
	if err != nil {
		return fmt.Errorf("lifecycle: create cooldown tmp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("lifecycle: chmod cooldown tmp: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("lifecycle: write cooldown tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("lifecycle: close cooldown tmp: %w", err)
	}
	if err := os.Rename(tmpPath, p.CooldownMark); err != nil {
		cleanup()
		return fmt.Errorf("lifecycle: rename cooldown tmp: %w", err)
	}
	return nil
}

// ReadCooldown returns the recorded cooldown and whether it is currently
// active (present and not yet expired against now()). A missing file
// reports active=false with err=nil.
func ReadCooldown(p Paths, now func() time.Time) (Cooldown, bool, error) {
	if p.CooldownMark == "" {
		return Cooldown{}, false, fmt.Errorf("lifecycle: empty cooldown path")
	}
	if now == nil {
		now = time.Now
	}
	data, err := os.ReadFile(p.CooldownMark)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Cooldown{}, false, nil
		}
		return Cooldown{}, false, fmt.Errorf("lifecycle: read cooldown: %w", err)
	}
	var c Cooldown
	if err := json.Unmarshal(data, &c); err != nil {
		return Cooldown{}, false, fmt.Errorf("lifecycle: decode cooldown %s: %w", p.CooldownMark, err)
	}
	c.Until = c.Until.UTC()
	active := now().UTC().Before(c.Until)
	return c, active, nil
}

// ClearCooldown removes the cooldown marker. Missing file → nil.
func ClearCooldown(p Paths) error {
	if p.CooldownMark == "" {
		return fmt.Errorf("lifecycle: empty cooldown path")
	}
	if err := os.Remove(p.CooldownMark); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("lifecycle: remove cooldown: %w", err)
	}
	return nil
}
