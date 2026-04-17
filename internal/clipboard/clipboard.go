// Package clipboard reads the host clipboard, auto-detecting the right tool
// (pbpaste / wl-paste / xclip / xsel) with an optional user override
// (DECISIONS.md §22).
package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"unicode/utf8"
)

// Reader is the interface defined in docs/implementation/interfaces.md.
type Reader interface {
	Read() ([]byte, error)
}

// ErrNoReaderAvailable is returned from NewAutoDetect / Detect when no
// clipboard tool is installed and no override is configured. The message is
// the install-hint verbatim from docs/storage/clipboard.md.
//
//nolint:staticcheck // multi-line install hint is intentional, surfaced verbatim to the user
var ErrNoReaderAvailable = errors.New(
	"No clipboard reader available on this host.\n\n" +
		"Install one of:\n" +
		"  - pbpaste (macOS, built in)\n" +
		"  - wl-paste (part of wl-clipboard, Wayland)\n" +
		"  - xclip or xsel (X11)\n\n" +
		"Or set `clipboard_read_command` in your tprompt config.",
)

// Detect runs the auto-detection strategy documented in
// docs/storage/clipboard.md and returns the resolved argv, a short reason
// (e.g. "Wayland", "X11"), and ok=true when a reader was selected. Callers
// like `doctor` use it for reporting; NewAutoDetect uses it to build a
// Reader. Detect does not exec anything — it only probes env and $PATH.
func Detect(getenv func(string) string, lookPath func(string) (string, error)) (argv []string, reason string, ok bool) {
	if runtime.GOOS == "darwin" {
		return []string{"pbpaste"}, "macOS", true
	}
	if getenv("WAYLAND_DISPLAY") != "" {
		if _, err := lookPath("wl-paste"); err == nil {
			// -n suppresses wl-paste's default trailing newline, matching
			// pbpaste/xclip/xsel so all auto-detected readers behave uniformly.
			return []string{"wl-paste", "-n"}, "Wayland", true
		}
	}
	if getenv("DISPLAY") != "" {
		if _, err := lookPath("xclip"); err == nil {
			return []string{"xclip", "-selection", "clipboard", "-o"}, "X11", true
		}
		if _, err := lookPath("xsel"); err == nil {
			return []string{"xsel", "-b", "-o"}, "X11", true
		}
	}
	return nil, "", false
}

// NewAutoDetect returns a Reader backed by the tool Detect selects. It
// returns ErrNoReaderAvailable if nothing is usable on this host.
func NewAutoDetect(getenv func(string) string, lookPath func(string) (string, error)) (Reader, error) {
	argv, _, ok := Detect(getenv, lookPath)
	if !ok {
		return nil, ErrNoReaderAvailable
	}
	return NewCommand(argv), nil
}

// NewCommand returns a Reader that execs argv. Argv[0] is resolved via $PATH
// by os/exec; callers that want early $PATH validation should use LookPath
// separately (see doctor).
func NewCommand(argv []string) Reader { return &commandReader{argv: argv} }

// NewStatic returns a Reader that yields fixed bytes. Intended for tests.
func NewStatic(content []byte) Reader { return staticReader(content) }

type staticReader []byte

func (s staticReader) Read() ([]byte, error) { return []byte(s), nil }

type commandReader struct{ argv []string }

func (c *commandReader) Read() ([]byte, error) {
	if len(c.argv) == 0 {
		return nil, errors.New("clipboard: empty argv")
	}
	var stdout, stderr bytes.Buffer
	// The argv is user-configured (clipboard_read_command) or auto-detected
	// from a short allowlist (pbpaste/wl-paste/xclip/xsel); exec with
	// user-supplied args is the whole point of the reader.
	cmd := exec.Command(c.argv[0], c.argv[1:]...) //nolint:gosec // G204: intentional exec of user-configured reader
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := bytes.TrimRight(stderr.Bytes(), "\n")
		if len(msg) == 0 {
			return nil, fmt.Errorf("clipboard reader %q failed: %w", c.argv[0], err)
		}
		return nil, fmt.Errorf("clipboard reader %q failed: %w: %s", c.argv[0], err, msg)
	}
	return stdout.Bytes(), nil
}

// Validate applies the content checks shared by `tprompt paste` and the TUI
// clipboard row. Error strings are fixed by docs/implementation/error-handling.md.
func Validate(content []byte, limit int64) error {
	if len(content) == 0 {
		return errors.New("clipboard is empty")
	}
	if !utf8.Valid(content) {
		return errors.New("clipboard content is not valid UTF-8 text")
	}
	if limit > 0 && int64(len(content)) > limit {
		return fmt.Errorf("clipboard content exceeds max_paste_bytes (%d > %d)", len(content), limit)
	}
	return nil
}
