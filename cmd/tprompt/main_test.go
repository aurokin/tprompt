package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/app"
)

func TestRunCLIConfigErrorFormat(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`default_mode = "yolo"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer

	code := app.RunCLI([]string{"--config", cfgPath, "list"}, &stdout, &stderr, strings.NewReader(""))
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
	if !strings.Contains(got, "default_mode") {
		t.Fatalf("stderr should mention default_mode: %q", got)
	}
}
