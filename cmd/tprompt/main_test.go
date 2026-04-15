package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunPrintsCommandErrorOnce(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"list"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}

	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("stderr lines = %d, want 1: %q", len(lines), stderr.String())
	}
	if lines[0] != "Error: not implemented" {
		t.Fatalf("stderr = %q, want %q", lines[0], "Error: not implemented")
	}
}
