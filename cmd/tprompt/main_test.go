package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/app"
)

func TestRunCLIConfigErrorFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := app.RunCLI([]string{"list"}, &stdout, &stderr, strings.NewReader(""))
	if code != app.ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, app.ExitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}

	got := strings.TrimSpace(stderr.String())
	if !strings.HasPrefix(got, "tprompt - error:") {
		t.Fatalf("stderr = %q, want tprompt - error: prefix", got)
	}
	if !strings.Contains(got, "prompts_dir") {
		t.Fatalf("stderr should mention prompts_dir: %q", got)
	}
}
