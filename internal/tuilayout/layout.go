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

// FieldCursor is the glyph drawn at the caret of an active input field. It is a
// plain rune (not a styled cell) so the rendered field stays assertable in
// view tests and never emits escape bytes.
const FieldCursor = "│"

// RenderField renders value as an active single-line input of the given width,
// drawing FieldCursor at the cursor rune offset and scrolling horizontally so
// the cursor stays visible. A leading/trailing "…" marks hidden text. cursor is
// clamped into [0, len([]rune(value))].
func RenderField(value string, cursor, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	switch {
	case cursor < 0:
		cursor = 0
	case cursor > len(runes):
		cursor = len(runes)
	}
	withCursor := string(runes[:cursor]) + FieldCursor + string(runes[cursor:])
	if lipgloss.Width(withCursor) <= width {
		return withCursor
	}
	if width < 3 {
		// Too narrow to window meaningfully; degrade without overflowing.
		return TruncateToWidth(withCursor, width)
	}

	start, end, left, right := fieldWindowWithEllipses(runes, cursor, width-1)
	var b strings.Builder
	if left {
		b.WriteString("…")
	}
	b.WriteString(string(runes[start:cursor]))
	b.WriteString(FieldCursor)
	b.WriteString(string(runes[cursor:end]))
	if right {
		b.WriteString("…")
	}
	return b.String()
}

// fieldWindowWithEllipses picks the [start, end) window around cursor and which
// sides are truncated, reserving a column for each ellipsis it will draw. It
// iterates to a fixed point because reserving for one ellipsis can shrink the
// window enough to truncate (and thus need an ellipsis on) the other side.
func fieldWindowWithEllipses(runes []rune, cursor, budget int) (start, end int, left, right bool) {
	for reserve := 0; ; {
		start, end = fieldWindow(runes, cursor, budget-reserve)
		left, right = start > 0, end < len(runes)
		need := 0
		if left {
			need++
		}
		if right {
			need++
		}
		if need <= reserve {
			return start, end, left, right
		}
		reserve = need
	}
}

// fieldWindow grows a [start, end) rune window outward from cursor, left first
// (so an end-of-field caret keeps its tail visible), until the rendered width
// of the included runes would exceed budget.
func fieldWindow(runes []rune, cursor, budget int) (int, int) {
	start, end, used := cursor, cursor, 0
	for start > 0 {
		w := runeWidth(runes[start-1])
		if used+w > budget {
			break
		}
		used += w
		start--
	}
	for end < len(runes) {
		w := runeWidth(runes[end])
		if used+w > budget {
			break
		}
		used += w
		end++
	}
	return start, end
}

func runeWidth(r rune) int {
	return lipgloss.Width(string(r))
}
