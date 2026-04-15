// Package clipboard reads the host clipboard, auto-detecting the right tool
// (pbpaste / wl-paste / xclip / xsel) with an optional user override
// (DECISIONS.md §22).
package clipboard

import "errors"

// ErrNotImplemented is returned by the Phase 0 stubs so callers fail loud
// instead of receiving an empty Reader or silently-empty content.
var ErrNotImplemented = errors.New("clipboard: not implemented")

// Reader is the interface defined in docs/implementation/interfaces.md.
type Reader interface {
	Read() ([]byte, error)
}

// NewAutoDetect returns a Reader that probes the host for a working clipboard
// tool. Phase 3.5 implements this.
func NewAutoDetect() (Reader, error) { return nil, ErrNotImplemented }

// NewCommand returns a Reader that shells out to the given argv. Phase 3.5
// implements the actual exec; the stub returns a Reader whose Read fails
// loud so a premature caller is caught immediately.
func NewCommand([]string) Reader { return notImplementedReader{} }

// NewStatic returns a Reader that yields fixed bytes. Intended for tests.
func NewStatic(content []byte) Reader { return staticReader(content) }

type staticReader []byte

func (s staticReader) Read() ([]byte, error) { return []byte(s), nil }

type notImplementedReader struct{}

func (notImplementedReader) Read() ([]byte, error) { return nil, ErrNotImplemented }
