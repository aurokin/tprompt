package app

import (
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }

func assertPrefix(t *testing.T, line, prefix string) {
	t.Helper()
	if !strings.HasPrefix(line, prefix) {
		t.Errorf("want line to start with %q, got %q", prefix, line)
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("want %q to contain %q", s, substr)
	}
}
