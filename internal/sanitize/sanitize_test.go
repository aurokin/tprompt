package sanitize

import (
	"strings"
	"testing"
)

func TestNewReportsMode(t *testing.T) {
	for _, m := range []Mode{ModeOff, ModeSafe, ModeStrict} {
		if got := New(m).Mode(); got != m {
			t.Errorf("New(%q).Mode() = %q", m, got)
		}
	}
}

func TestModeOffPassesContentThrough(t *testing.T) {
	input := []byte("hello\x1b[31mworld\x1b[0m")

	got, err := New(ModeOff).Process(input)
	if err != nil {
		t.Fatalf("New(%q).Process returned error: %v", ModeOff, err)
	}
	if string(got) != string(input) {
		t.Fatalf("New(%q).Process = %q, want %q", ModeOff, got, input)
	}
}

func TestSafeAndStrictFailClosedUntilImplemented(t *testing.T) {
	for _, mode := range []Mode{ModeSafe, ModeStrict} {
		t.Run(string(mode), func(t *testing.T) {
			got, err := New(mode).Process([]byte("hello"))
			if err == nil {
				t.Fatalf("New(%q).Process returned nil error", mode)
			}
			if got != nil {
				t.Fatalf("New(%q).Process returned content %q, want nil", mode, got)
			}
			if !strings.Contains(err.Error(), string(mode)) {
				t.Fatalf("error = %q, want reference to mode %q", err, mode)
			}
			if !strings.Contains(err.Error(), "not implemented") {
				t.Fatalf("error = %q, want not implemented", err)
			}
		})
	}
}
