package tmux

import "unicode/utf8"

// chunkByRunes splits s into substrings of at most maxBytes, cutting only on
// UTF-8 rune boundaries so multi-byte characters are never split. An empty
// input returns nil. Degenerate case: if a single rune exceeds maxBytes (or
// the input contains invalid UTF-8), the emitted chunk may exceed maxBytes by
// up to one rune so forward progress is guaranteed.
func chunkByRunes(s string, maxBytes int) []string {
	if s == "" {
		return nil
	}
	if maxBytes <= 0 || len(s) <= maxBytes {
		return []string{s}
	}

	var out []string
	i := 0
	for i < len(s) {
		if len(s)-i <= maxBytes {
			out = append(out, s[i:])
			return out
		}
		end := i + maxBytes
		for end > i && !utf8.RuneStart(s[end]) {
			end--
		}
		if end == i {
			// Degenerate case: a single rune wider than maxBytes, or invalid
			// UTF-8. Advance by one rune to make forward progress.
			_, size := utf8.DecodeRuneInString(s[i:])
			if size == 0 {
				size = 1
			}
			end = i + size
		}
		out = append(out, s[i:end])
		i = end
	}
	return out
}
