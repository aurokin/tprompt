package app

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
)

func TestDoctorHealthy(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{{ID: "a"}, {ID: "b"}}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, ConfigPath: "/etc/tprompt/config.toml"}, nil
	}
	deps.Env = func(key string) string {
		if key == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 lines, got %d:\n%s", len(lines), stdout)
	}
	assertPrefix(t, lines[0], "ok")
	assertContains(t, lines[0], "config loaded")
	assertContains(t, lines[0], "/etc/tprompt/config.toml")
	assertPrefix(t, lines[1], "ok")
	assertContains(t, lines[1], "prompts directory exists")
	assertPrefix(t, lines[2], "ok")
	assertContains(t, lines[2], "2 prompts discovered")
	assertPrefix(t, lines[3], "ok")
	assertContains(t, lines[3], "inside tmux")
}

func TestDoctorNoTmux(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	last := lines[len(lines)-1]
	assertPrefix(t, last, "warn")
	assertContains(t, last, "not inside tmux")
}

func TestDoctorConfigFailure(t *testing.T) {
	configErr := &config.ValidationError{Field: "prompts_dir", Message: "must be set"}

	deps := Deps{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		Stdin:      &bytes.Buffer{},
		Env:        func(string) string { return "" },
		ConfigPath: strPtr(""),
		LoadConfig: func(string) (config.Resolved, error) {
			return config.Resolved{}, configErr
		},
		NewStore: func(config.Resolved) (store.Store, error) { return nil, nil },
		NewTmux:  func() (tmux.Adapter, error) { return nil, nil },
		NewClip:  func(config.Resolved) (clipboard.Reader, error) { return nil, nil },
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")

	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %T: %v", err, err)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	assertPrefix(t, lines[0], "FAIL")
	assertContains(t, lines[0], "prompts_dir")
	last := lines[len(lines)-1]
	assertPrefix(t, last, "warn")
	assertContains(t, last, "not inside tmux")
}

func TestDoctorDiscoveryFailure(t *testing.T) {
	dir := t.TempDir()
	dupErr := &store.DuplicatePromptIDError{
		ID:    "code-review",
		Paths: []string{"/a/code-review.md", "/b/code-review.md"},
	}
	fs := &fakeStore{discoverErr: dupErr}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")

	var de *store.DuplicatePromptIDError
	if !errors.As(err, &de) {
		t.Fatalf("want DuplicatePromptIDError, got %T: %v", err, err)
	}

	assertContains(t, stdout, "FAIL")
	assertContains(t, stdout, "duplicate prompt ID")
}

func TestDoctorDefaultsConfig(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, ConfigPath: ""}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "defaults")
}

func TestDoctorPromptsDirMissing(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "does-not-exist")
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: missingDir}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err == nil {
		t.Fatal("want error, got nil")
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (config ok, dir fail, tmux warn), got %d:\n%s", len(lines), stdout)
	}
	assertPrefix(t, lines[0], "ok")
	assertContains(t, lines[0], "config loaded")
	assertPrefix(t, lines[1], "FAIL")
	assertContains(t, lines[1], "prompts directory missing")
	assertPrefix(t, lines[2], "warn")
	assertContains(t, lines[2], "not inside tmux")
}
