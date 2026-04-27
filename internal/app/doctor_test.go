package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
)

func isDarwin() bool { return runtime.GOOS == "darwin" }

func TestDoctorHealthy(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{{ID: "a"}, {ID: "b"}}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			PromptsDir:    dir,
			ConfigPath:    "/etc/tprompt/config.toml",
			SocketPath:    "/tmp/tprompt-test.sock",
			ClipboardArgv: []string{"custom-paste"},
			PickerArgv:    []string{"fzf"},
		}, nil
	}
	deps.Env = func(key string) string {
		if key == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}
	deps.LookPath = func(name string) (string, error) {
		switch name {
		case "custom-paste":
			return "/usr/bin/custom-paste", nil
		case "fzf":
			return "/usr/bin/fzf", nil
		}
		return "", exec.ErrNotFound
	}
	deps.NewDaemonClient = func(config.Resolved) (daemon.Client, error) {
		return &fakeDaemonClient{
			statusFn: func() (daemon.StatusResponse, error) {
				return daemon.StatusResponse{Socket: "/tmp/tprompt-test.sock"}, nil
			},
		}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("want 7 lines, got %d:\n%s", len(lines), stdout)
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
	assertPrefix(t, lines[4], "ok")
	assertContains(t, lines[4], "clipboard reader: custom-paste (override)")
	assertPrefix(t, lines[5], "ok")
	assertContains(t, lines[5], "picker command: fzf")
	assertPrefix(t, lines[6], "ok")
	assertContains(t, lines[6], "daemon reachable")
}

func TestDoctorNoTmux(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock"}, nil
	}
	deps.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertContains(t, stdout, "warn not inside tmux")
}

func TestDoctorDaemonReachable(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock"}, nil
	}
	deps.NewDaemonClient = func(config.Resolved) (daemon.Client, error) {
		return &fakeDaemonClient{
			statusFn: func() (daemon.StatusResponse, error) {
				return daemon.StatusResponse{Socket: "/tmp/tprompt-test.sock"}, nil
			},
		}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "ok   daemon reachable (/tmp/tprompt-test.sock)")
}

func TestDoctorDaemonMissingIsWarning(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock"}, nil
	}
	deps.NewDaemonClient = func(config.Resolved) (daemon.Client, error) {
		return &fakeDaemonClient{
			statusFn: func() (daemon.StatusResponse, error) {
				return daemon.StatusResponse{}, &daemon.SocketUnavailableError{
					Path:   "/tmp/tprompt-test.sock",
					Reason: "connection refused",
				}
			},
		}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "warn daemon not running (/tmp/tprompt-test.sock): connection refused")
}

func TestDoctorInvalidDaemonConfigFails(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir}, nil
	}
	deps.NewDaemonClient = func(config.Resolved) (daemon.Client, error) {
		t.Fatal("NewDaemonClient should not be called with invalid daemon config")
		return nil, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "socket_path" {
		t.Fatalf("Field = %q, want socket_path", ve.Field)
	}
	assertContains(t, stdout, "FAIL config: socket_path: must be set")
}

func TestDoctorPickerPresent(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock", PickerArgv: []string{"fzf"}}, nil
	}
	deps.LookPath = func(name string) (string, error) {
		if name == "fzf" {
			return "/usr/bin/fzf", nil
		}
		return "", exec.ErrNotFound
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "ok   picker command: fzf")
}

func TestDoctorPickerMissingIsWarning(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock", PickerArgv: []string{"fzf"}}, nil
	}
	deps.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "warn picker command: fzf not found on $PATH (tprompt pick unavailable)")
}

func TestDoctorClipboardAutoDetectWayland(t *testing.T) {
	if isDarwin() {
		t.Skip("darwin always auto-detects pbpaste")
	}
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock"}, nil
	}
	deps.Env = func(key string) string {
		if key == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	deps.LookPath = func(name string) (string, error) {
		if name == "wl-paste" {
			return "/usr/bin/wl-paste", nil
		}
		return "", exec.ErrNotFound
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "ok   clipboard reader: wl-paste (auto-detected, Wayland)")
}

func TestDoctorClipboardAutoDetectX11(t *testing.T) {
	if isDarwin() {
		t.Skip("darwin always auto-detects pbpaste")
	}
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock"}, nil
	}
	deps.Env = func(key string) string {
		if key == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	deps.LookPath = func(name string) (string, error) {
		if name == "xclip" {
			return "/usr/bin/xclip", nil
		}
		return "", exec.ErrNotFound
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "ok   clipboard reader: xclip (auto-detected, X11)")
}

func TestDoctorClipboardOverrideMissing(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock", ClipboardArgv: []string{"not-on-path"}}, nil
	}
	deps.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "warn clipboard reader: not-on-path (override) not found on $PATH")
}

func TestDoctorClipboardNoneAvailable(t *testing.T) {
	if isDarwin() {
		t.Skip("darwin always auto-detects pbpaste")
	}
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock"}, nil
	}
	// No env hints, no PATH hits.
	deps.Env = func(string) string { return "" }
	deps.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "warn clipboard reader: none available")
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
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock"}, nil
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
		return config.Resolved{PromptsDir: dir, SocketPath: "/tmp/tprompt-test.sock", ConfigPath: ""}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "defaults")
}

func TestDoctorPromptsDirAutoCreateFails(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocker)

	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{SocketPath: "/tmp/tprompt-test.sock"}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var createErr *store.PromptsDirCreateError
	if !errors.As(err, &createErr) {
		t.Fatalf("want *store.PromptsDirCreateError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("ExitCode = %d, want %d", ExitCode(err), ExitUsage)
	}
	assertContains(t, stdout, "create prompts directory")
}

func TestDoctorPromptsDirMissing(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "does-not-exist")
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: missingDir, SocketPath: "/tmp/tprompt-test.sock"}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err == nil {
		t.Fatal("want error, got nil")
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("want 6 lines (config ok, dir fail, tmux warn, clipboard, picker, daemon), got %d:\n%s", len(lines), stdout)
	}
	assertPrefix(t, lines[0], "ok")
	assertContains(t, lines[0], "config loaded")
	assertPrefix(t, lines[1], "FAIL")
	assertContains(t, lines[1], "prompts directory missing")
	assertPrefix(t, lines[2], "warn")
	assertContains(t, lines[2], "not inside tmux")
	assertContains(t, lines[3], "clipboard reader")
	assertContains(t, lines[4], "picker command")
	assertContains(t, lines[5], "daemon unreachable")
}
