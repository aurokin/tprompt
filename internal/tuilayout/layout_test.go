package tuilayout

import "testing"

func TestRowsPerFrame(t *testing.T) {
	tests := []struct {
		name        string
		height      int
		headerLines int
		footerLines int
		want        int
	}{
		{"fits", 10, 2, 1, 7},
		{"zero height", 0, 0, 1, 0},
		{"chrome exceeds height", 2, 2, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RowsPerFrame(tt.height, tt.headerLines, tt.footerLines)
			if got != tt.want {
				t.Fatalf("RowsPerFrame(%d, %d, %d) = %d, want %d",
					tt.height, tt.headerLines, tt.footerLines, got, tt.want)
			}
		})
	}
}

func TestClampScrollOffset(t *testing.T) {
	cases := []struct {
		name     string
		cursor   int
		offset   int
		rowCount int
		rpf      int
		want     int
	}{
		{"pre-WindowSize rpf zero collapses", 2, 5, 10, 0, 0},
		{"empty row set collapses", 0, 3, 0, 2, 0},
		{"cursor inside window is noop", 2, 1, 10, 3, 1},
		{"cursor above window pulls offset down", 0, 3, 10, 2, 0},
		{"cursor below window pulls offset up", 5, 0, 10, 2, 4},
		{"offset past tail clamps to len-rpf", 3, 7, 4, 2, 2},
		{"negative offset clamped to zero", 0, -5, 4, 2, 0},
		{"rpf exceeds row count collapses to zero", 1, 3, 4, 10, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClampScrollOffset(tc.cursor, tc.offset, tc.rowCount, tc.rpf)
			if got != tc.want {
				t.Fatalf("ClampScrollOffset(%d, %d, %d, %d) = %d, want %d",
					tc.cursor, tc.offset, tc.rowCount, tc.rpf, got, tc.want)
			}
		})
	}
}

func TestVisibleRange(t *testing.T) {
	tests := []struct {
		name     string
		offset   int
		rowCount int
		rpf      int
		start    int
		end      int
	}{
		{"pre-size renders all", 3, 10, 0, 0, 10},
		{"all rows fit", 3, 4, 10, 0, 4},
		{"middle window", 2, 10, 3, 2, 5},
		{"tail clamps end", 8, 10, 5, 8, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := VisibleRange(tt.offset, tt.rowCount, tt.rpf)
			if start != tt.start || end != tt.end {
				t.Fatalf("VisibleRange(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tt.offset, tt.rowCount, tt.rpf, start, end, tt.start, tt.end)
			}
		})
	}
}

func TestPadRight(t *testing.T) {
	if got := PadRight("abc", 5); got != "abc  " {
		t.Fatalf("PadRight = %q, want %q", got, "abc  ")
	}
	if got := PadRight("abcdef", 3); got != "abcdef" {
		t.Fatalf("PadRight over-wide = %q, want unchanged", got)
	}
}

func TestTruncateToWidth(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{"fits as-is", "abc", 10, "abc"},
		{"zero width blank", "abc", 0, ""},
		{"negative width blank", "abc", -1, ""},
		{"truncates with ellipsis", "abcdefghij", 5, "abcd…"},
		{"single ellipsis at width 1", "abcdef", 1, "…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TruncateToWidth(tc.in, tc.width); got != tc.want {
				t.Fatalf("TruncateToWidth(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
			}
		})
	}
}
