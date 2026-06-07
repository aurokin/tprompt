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
	"github.com/hsadler/tprompt/internal/delivery"
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
	// $TMUX is set above, so checkPopupBinding runs; give it an adapter whose
	// list-keys output contains a tprompt binding so the healthy path reports ok.
	deps.NewTmux = func() (tmux.Adapter, error) {
		return &fakeAdapter{listKeys: "bind-key -T prefix g run-shell \"tprompt tui\""}, nil
	}
	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 10 {
		t.Fatalf("want 10 lines, got %d:\n%s", len(lines), stdout)
	}
	assertPrefix(t, lines[0], "ok")
	assertContains(t, lines[0], "config loaded")
	assertContains(t, lines[0], "/etc/tprompt/config.toml")
	assertPrefix(t, lines[1], "ok")
	assertContains(t, lines[1], "prompt priority: global")
	assertPrefix(t, lines[2], "ok")
	assertContains(t, lines[2], "prompts directory exists")
	assertPrefix(t, lines[3], "ok")
	assertContains(t, lines[3], "project overlay: no project overlay")
	assertPrefix(t, lines[4], "ok")
	assertContains(t, lines[4], "2 prompts discovered")
	assertPrefix(t, lines[5], "ok")
	assertContains(t, lines[5], "inside tmux")
	assertPrefix(t, lines[6], "ok")
	assertContains(t, lines[6], "tmux key binding runs tprompt")
	assertPrefix(t, lines[7], "ok")
	assertContains(t, lines[7], "clipboard reader: custom-paste (override)")
	assertPrefix(t, lines[8], "ok")
	assertContains(t, lines[8], "picker command: fzf")
	assertPrefix(t, lines[9], "ok")
	assertContains(t, lines[9], "TUI handoff ready")
}

func TestDoctorNoTmux(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir}, nil
	}
	deps.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertContains(t, stdout, "warn not inside tmux")
	// Outside tmux the binding check is skipped entirely (list-keys needs a
	// running server); it must not emit any binding line.
	if strings.Contains(stdout, "key binding runs tprompt") {
		t.Errorf("binding check should be skipped outside tmux, got:\n%s", stdout)
	}
}

// doctorInTmuxDeps builds doctor Deps with $TMUX set and a healthy store, so
// checkPopupBinding runs. The caller supplies the tmux adapter.
func doctorInTmuxDeps(t *testing.T, adapter tmux.Adapter, adapterErr error) Deps {
	t.Helper()
	dir := t.TempDir()
	deps := workingDeps(t, &fakeStore{summaries: []store.Summary{{ID: "a"}}})
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir}, nil
	}
	deps.Env = func(key string) string {
		if key == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}
	deps.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	deps.NewTmux = func() (tmux.Adapter, error) { return adapter, adapterErr }
	return deps
}

func TestDoctorPopupBindingMissingWarns(t *testing.T) {
	// list-keys output with no tprompt-invoking binding.
	deps := doctorInTmuxDeps(t, &fakeAdapter{listKeys: "bind-key -T prefix c new-window\nbind-key -T prefix d detach-client\n"}, nil)

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("binding check must not affect exit code: %v", err)
	}
	assertContains(t, stdout, "warn no tmux key binding runs tprompt")
	assertContains(t, stdout, "tprompt init")
	// The binding check is independent of the $TMUX presence check.
	assertContains(t, stdout, "ok   inside tmux")
}

func TestDoctorPopupBindingPresentOK(t *testing.T) {
	deps := doctorInTmuxDeps(t, &fakeAdapter{listKeys: "bind-key -T prefix g run-shell \"tprompt tui\"\n"}, nil)

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "ok   tmux key binding runs tprompt")
	if strings.Contains(stdout, "no tmux key binding") {
		t.Errorf("present binding should report ok, got:\n%s", stdout)
	}
}

func TestDoctorPopupBindingErrorsWarnSoftly(t *testing.T) {
	cases := []struct {
		name    string
		adapter tmux.Adapter
		err     error
	}{
		{name: "adapter construction fails", adapter: nil, err: ErrNotImplemented},
		{name: "list-keys query fails", adapter: &fakeAdapter{listKeysErr: ErrNotImplemented}, err: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := doctorInTmuxDeps(t, tc.adapter, tc.err)
			stdout, _, err := executeRootWith(t, deps, "doctor")
			if err != nil {
				t.Fatalf("binding-check failure must not affect exit code: %v", err)
			}
			assertContains(t, stdout, "warn could not check tmux key bindings")
		})
	}
}

func TestDoctorNoAutoStartEnvIgnored(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir}, nil
	}
	deps.Env = func(key string) string {
		if key == "TPROMPT_NO_AUTO_START" {
			return "1"
		}
		return ""
	}
	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stdout, "TPROMPT_NO_AUTO_START") {
		t.Fatalf("doctor mentioned obsolete auto-start env:\n%s", stdout)
	}
}

func TestDoctorHandoffReady(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, LogPath: "/tmp/tprompt-test.log"}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "ok   TUI handoff ready (/tmp/jobs)")
}

func TestDoctorHandoffUnavailableIsWarning(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir}, nil
	}
	deps.NewTUIClient = func(config.Resolved) (delivery.Client, error) {
		return nil, errors.New("missing executable")
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "warn TUI handoff unavailable: missing executable")
}

func TestDoctorHandoffDoesNotRequireSocket(t *testing.T) {
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
	assertContains(t, stdout, "ok   TUI handoff ready")
}

func TestDoctorPickerPresent(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: dir, PickerArgv: []string{"fzf"}}, nil
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
		return config.Resolved{PromptsDir: dir, PickerArgv: []string{"fzf"}}, nil
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
		return config.Resolved{PromptsDir: dir}, nil
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
		return config.Resolved{PromptsDir: dir}, nil
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
		return config.Resolved{PromptsDir: dir, ClipboardArgv: []string{"not-on-path"}}, nil
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
		return config.Resolved{PromptsDir: dir}, nil
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

func TestDoctorPromptsDirAutoCreateFails(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocker)

	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{}, nil
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
		return config.Resolved{PromptsDir: missingDir}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err == nil {
		t.Fatal("want error, got nil")
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("want 7 lines (config ok, policy ok, dir fail, tmux warn, clipboard, picker, handoff), got %d:\n%s", len(lines), stdout)
	}
	assertPrefix(t, lines[0], "ok")
	assertContains(t, lines[0], "config loaded")
	assertPrefix(t, lines[1], "ok")
	assertContains(t, lines[1], "prompt priority: global")
	assertPrefix(t, lines[2], "FAIL")
	assertContains(t, lines[2], "prompts directory missing")
	assertPrefix(t, lines[3], "warn")
	assertContains(t, lines[3], "not inside tmux")
	assertContains(t, lines[4], "clipboard reader")
	assertContains(t, lines[5], "picker command")
	assertContains(t, lines[6], "TUI handoff ready")
}

func TestDoctorMissingAdditionalPromptsDirIsWarning(t *testing.T) {
	primary := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing-additional")
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			PromptsDir:            primary,
			AdditionalPromptsDirs: []string{missing},
		}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, stdout, "ok   prompts directory exists (scope global, "+primary+") [explicit]")
	assertContains(t, stdout, "warn prompts directory missing (scope global, "+missing+") [additional]")
	assertContains(t, stdout, "warn 0 prompts discovered (run 'tprompt new <id>' to create one)")
}

func TestDoctorAdditionalPromptsDirThatIsFileIsFailure(t *testing.T) {
	primary := t.TempDir()
	fileSource := filepath.Join(t.TempDir(), "team-prompts")
	if err := os.WriteFile(fileSource, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fs := &fakeStore{summaries: []store.Summary{}}
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			PromptsDir:            primary,
			AdditionalPromptsDirs: []string{fileSource},
		}, nil
	}

	stdout, _, err := executeRootWith(t, deps, "doctor")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var missingErr *store.PromptsDirMissingError
	if !errors.As(err, &missingErr) {
		t.Fatalf("want PromptsDirMissingError, got %T: %v", err, err)
	}
	assertContains(t, stdout, "ok   prompts directory exists (scope global, "+primary+") [explicit]")
	assertContains(t, stdout, "FAIL prompts directory missing: "+fileSource)
}
