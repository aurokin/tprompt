// Package sanitize implements the off / safe / strict content sanitizer
// (DECISIONS.md §23, docs/implementation/sanitization.md).
package sanitize

import "fmt"

// Mode represents the configured sanitization strictness.
type Mode string

const (
	ModeOff    Mode = "off"
	ModeSafe   Mode = "safe"
	ModeStrict Mode = "strict"
)

// Sanitizer is the interface defined in docs/implementation/interfaces.md.
type Sanitizer interface {
	Mode() Mode
	Process(content []byte) ([]byte, error)
}

// New returns a Sanitizer for the given mode. Phase 3.5 implements safe/strict.
func New(mode Mode) Sanitizer {
	switch mode {
	case ModeOff:
		return passthrough{mode: mode}
	case ModeSafe, ModeStrict:
		return unimplemented{mode: mode}
	default:
		return invalidMode{mode: mode}
	}
}

type passthrough struct{ mode Mode }

func (p passthrough) Mode() Mode                       { return p.mode }
func (p passthrough) Process(b []byte) ([]byte, error) { return b, nil }

type unimplemented struct{ mode Mode }

func (u unimplemented) Mode() Mode { return u.mode }

func (u unimplemented) Process(_ []byte) ([]byte, error) {
	return nil, fmt.Errorf("sanitize mode %q not implemented", u.mode)
}

type invalidMode struct{ mode Mode }

func (i invalidMode) Mode() Mode { return i.mode }

func (i invalidMode) Process(_ []byte) ([]byte, error) {
	return nil, fmt.Errorf("invalid sanitize mode %q", i.mode)
}
