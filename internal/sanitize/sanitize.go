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

// StrictRejectError is returned by strict-mode Process when an escape sequence
// is present. Offset is 0-based (matches Go slice indexing).
type StrictRejectError struct {
	Class  string
	Offset int
}

func (e *StrictRejectError) Error() string {
	return fmt.Sprintf("content rejected by sanitizer (mode=strict): escape sequence detected at byte %d (%s)", e.Offset, e.Class)
}

// New returns a Sanitizer for the given mode.
func New(mode Mode) Sanitizer {
	switch mode {
	case ModeOff:
		return passthrough{mode: mode}
	case ModeSafe:
		return safe{mode: mode}
	case ModeStrict:
		return strict{mode: mode}
	default:
		return invalidMode{mode: mode}
	}
}

type passthrough struct{ mode Mode }

func (p passthrough) Mode() Mode                       { return p.mode }
func (p passthrough) Process(b []byte) ([]byte, error) { return b, nil }

type invalidMode struct{ mode Mode }

func (i invalidMode) Mode() Mode { return i.mode }

func (i invalidMode) Process(_ []byte) ([]byte, error) {
	return nil, fmt.Errorf("invalid sanitize mode %q", i.mode)
}

type safe struct{ mode Mode }

func (s safe) Mode() Mode { return s.mode }

func (s safe) Process(content []byte) ([]byte, error) {
	if !hasESC(content) {
		return content, nil
	}
	out := make([]byte, 0, len(content))
	i := 0
	for i < len(content) {
		if content[i] != 0x1b {
			out = append(out, content[i])
			i++
			continue
		}
		_, end, dangerous := scanSequence(content, i)
		if !dangerous {
			out = append(out, content[i:end]...)
		}
		i = end
	}
	return out, nil
}

// StripAll removes every ESC-initiated sequence (OSC, DCS, CSI, KEYPAD, and
// bare ESC) from content. Unlike safe mode, cosmetic CSI sequences (SGR, cursor
// movement, erase) are also removed — intended for contexts like metadata
// display where any escape is a visual-manipulation vector.
func StripAll(content []byte) []byte {
	if !hasESC(content) {
		return content
	}
	out := make([]byte, 0, len(content))
	i := 0
	for i < len(content) {
		if content[i] != 0x1b {
			out = append(out, content[i])
			i++
			continue
		}
		_, end, _ := scanSequence(content, i)
		i = end
	}
	return out
}

type strict struct{ mode Mode }

func (s strict) Mode() Mode { return s.mode }

func (s strict) Process(content []byte) ([]byte, error) {
	for i := 0; i < len(content); i++ {
		if content[i] == 0x1b {
			class, _, _ := scanSequence(content, i)
			return nil, &StrictRejectError{Class: class, Offset: i}
		}
	}
	return content, nil
}

func hasESC(b []byte) bool {
	for _, c := range b {
		if c == 0x1b {
			return true
		}
	}
	return false
}

// scanSequence inspects content[i:] where content[i] == ESC. It returns the
// class tag, the index one past the end of the sequence, and whether the
// sequence is in the dangerous denylist (strip in safe mode).
func scanSequence(b []byte, i int) (class string, end int, dangerous bool) {
	if i+1 >= len(b) {
		return "ESC", len(b), true
	}
	switch b[i+1] {
	case ']':
		return "OSC", findStringTerm(b, i+2), true
	case 'P':
		return "DCS", findStringTerm(b, i+2), true
	case '[':
		end, danger := scanCSI(b, i+2)
		return "CSI", end, danger
	case '=', '>':
		return "KEYPAD", i + 2, true
	default:
		return "ESC", i + 2, true
	}
}

// findStringTerm returns one past the BEL or ESC\ that terminates an OSC/DCS
// string, or len(b) if the string runs unterminated to the end.
func findStringTerm(b []byte, from int) int {
	for j := from; j < len(b); j++ {
		if b[j] == 0x07 {
			return j + 1
		}
		if b[j] == 0x1b && j+1 < len(b) && b[j+1] == '\\' {
			return j + 2
		}
	}
	return len(b)
}

// scanCSI walks params (0x30-0x3F), intermediates (0x20-0x2F), final
// (0x40-0x7E). Returns one past the final byte and whether the sequence is
// dangerous (has '?' params, or final is 'h'/'l' mode set/reset).
func scanCSI(b []byte, from int) (end int, dangerous bool) {
	j := from
	hasPrivate := false
	for j < len(b) && b[j] >= 0x30 && b[j] <= 0x3F {
		if b[j] == '?' {
			hasPrivate = true
		}
		j++
	}
	for j < len(b) && b[j] >= 0x20 && b[j] <= 0x2F {
		j++
	}
	if j >= len(b) {
		return len(b), true
	}
	final := b[j]
	if final == 0x1b {
		// A nested ESC inside a CSI means the sequence is malformed; hand the
		// ESC back to the outer scanner so it starts a fresh sequence.
		return j, true
	}
	j++
	if final < 0x40 || final > 0x7E {
		return j, true
	}
	return j, hasPrivate || final == 'h' || final == 'l'
}
