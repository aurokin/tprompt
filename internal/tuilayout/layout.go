// Package tuilayout holds pure row-window and width helpers shared by terminal
// renderers. It has no knowledge of prompt, import, clipboard, or Bubble Tea
// model state; callers pass primitive dimensions and row counts.
package tuilayout

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RowsPerFrame returns how many row lines fit after renderer chrome is
// subtracted. It returns 0 before a terminal size is known or when chrome exceeds
// the window; renderers use that as their "render all rows" fallback.
func RowsPerFrame(height, headerLines, footerLines int) int {
	rpf := height - headerLines - footerLines
	if rpf <= 0 {
		return 0
	}
	return rpf
}

// ClampScrollOffset returns the scroll offset that keeps cursor visible in the
// window [offset, offset+rpf) and prevents overscroll past the list tail.
// rpf <= 0 or an empty row set collapse to offset 0.
func ClampScrollOffset(cursor, offset, rowCount, rpf int) int {
	if rpf <= 0 || rowCount == 0 {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+rpf {
		offset = cursor - rpf + 1
	}
	maxOffset := rowCount - rpf
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// VisibleRange returns [start, end) for rows visible in the current viewport.
// rpf <= 0 or rowCount <= rpf returns the full range.
func VisibleRange(offset, rowCount, rpf int) (int, int) {
	if rpf <= 0 || rowCount <= rpf {
		return 0, rowCount
	}
	end := offset + rpf
	if end > rowCount {
		end = rowCount
	}
	return offset, end
}

// PadRight pads s with spaces until its rendered width reaches width.
func PadRight(s string, width int) string {
	pad := width - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// TruncateToWidth returns s trimmed so its rendered width does not exceed
// maxWidth, appending an ellipsis when trimming occurred.
func TruncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		candidate := string(runes) + "…"
		if lipgloss.Width(candidate) <= maxWidth {
			return candidate
		}
		runes = runes[:len(runes)-1]
	}
	return "…"
}
