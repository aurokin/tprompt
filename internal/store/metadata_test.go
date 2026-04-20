package store

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hsadler/tprompt/internal/promptmeta"
)

func TestSanitizeMetaStripsDangerousEscapes(t *testing.T) {
	tests := []struct {
		name string
		in   promptmeta.Meta
		want promptmeta.Meta
	}{
		{
			name: "clean metadata is unchanged",
			in: promptmeta.Meta{
				Title:       "Code Review",
				Description: "Deep review",
				Tags:        []string{"review", "code"},
			},
			want: promptmeta.Meta{
				Title:       "Code Review",
				Description: "Deep review",
				Tags:        []string{"review", "code"},
			},
		},
		{
			name: "OSC in title stripped, other fields untouched",
			in: promptmeta.Meta{
				Title:       "evil\x1b]0;pwn\x07rest",
				Description: "Deep review",
				Tags:        []string{"review"},
			},
			want: promptmeta.Meta{
				Title:       "evilrest",
				Description: "Deep review",
				Tags:        []string{"review"},
			},
		},
		{
			name: "DCS in title stripped",
			in: promptmeta.Meta{
				Title: "a\x1bPpayload\x1b\\b",
			},
			want: promptmeta.Meta{
				Title: "ab",
			},
		},
		{
			name: "CSI private-mode in title stripped",
			in: promptmeta.Meta{
				Title: "hi\x1b[?1049hthere",
			},
			want: promptmeta.Meta{
				Title: "hithere",
			},
		},
		{
			name: "escape in description stripped",
			in: promptmeta.Meta{
				Description: "before\x1b]0;x\x07after",
			},
			want: promptmeta.Meta{
				Description: "beforeafter",
			},
		},
		{
			name: "escape in one tag stripped, siblings untouched",
			in: promptmeta.Meta{
				Tags: []string{"review", "ev\x1b]0;x\x07il", "code"},
			},
			want: promptmeta.Meta{
				Tags: []string{"review", "evil", "code"},
			},
		},
		{
			name: "multi-byte UTF-8 adjacent to escape survives",
			in: promptmeta.Meta{
				Title: "α\x1b]0;x\x07β",
			},
			want: promptmeta.Meta{
				Title: "αβ",
			},
		},
		{
			name: "mode, enter, key, KeyDeclared are not mutated",
			in: promptmeta.Meta{
				Title:       "evil\x1b]0;x\x07",
				Mode:        "paste",
				Enter:       boolPtr(true),
				Key:         stringPtr("c"),
				KeyDeclared: true,
			},
			want: promptmeta.Meta{
				Title:       "evil",
				Mode:        "paste",
				Enter:       boolPtr(true),
				Key:         stringPtr("c"),
				KeyDeclared: true,
			},
		},
		{
			name: "tag stripped down to empty stays in the slice",
			in: promptmeta.Meta{
				Tags: []string{"\x1b]0;x\x07", "keep"},
			},
			want: promptmeta.Meta{
				Tags: []string{"", "keep"},
			},
		},
		{
			name: "cosmetic SGR in description preserved (safe-mode parity)",
			in: promptmeta.Meta{
				Description: "red\x1b[31mword\x1b[0mend",
			},
			want: promptmeta.Meta{
				Description: "red\x1b[31mword\x1b[0mend",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in
			sanitizeMeta(&got)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("sanitizeMeta mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func boolPtr(v bool) *bool       { return &v }
func stringPtr(v string) *string { return &v }
