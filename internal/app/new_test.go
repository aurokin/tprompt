package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/store"
)

func TestValidateNewID(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		wantValid bool
		wantSub   string
	}{
		{name: "simple", id: "code-review", wantValid: true},
		{name: "underscores", id: "deep_review", wantValid: true},
		{name: "alphanumeric", id: "review42", wantValid: true},
		{name: "empty", id: "", wantSub: "must not be empty"},
		{name: "forward-slash", id: "team/review", wantSub: "path separators"},
		{name: "back-slash", id: `team\review`, wantSub: "path separators"},
		{name: "md-suffix", id: "review.md", wantSub: ".md suffix"},
		{name: "leading-dot", id: ".foo", wantSub: "start with a dot"},
		{name: "single-dot", id: ".", wantSub: "start with a dot"},
		{name: "double-dot", id: "..", wantSub: "start with a dot"},
		{name: "non-printable-tab", id: "review\t1", wantSub: "printable"},
		{name: "non-printable-newline", id: "review\n1", wantSub: "printable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNewID(tc.id)
			if tc.wantValid {
				if err != nil {
					t.Fatalf("validateNewID(%q) = %v, want nil", tc.id, err)
				}
				return
			}
			var invalid *InvalidNewIDError
			if !errors.As(err, &invalid) {
				t.Fatalf("validateNewID(%q) error = %T %v, want *InvalidNewIDError", tc.id, err, err)
			}
			if !strings.Contains(invalid.Reason, tc.wantSub) {
				t.Errorf("Reason %q missing %q", invalid.Reason, tc.wantSub)
			}
		})
	}
}

func TestNew_HappyPathWritesScaffoldTemplate(t *testing.T) {
	dir := t.TempDir()
	deps := newCmdDeps(t, dir)

	stdout, stderr, err := executeRootWith(t, deps, "new", "code-review")
	if err != nil {
		t.Fatalf("executeRootWith: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	target := filepath.Join(dir, "code-review.md")
	abs, err := filepath.Abs(target)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got := strings.TrimRight(stdout, "\n"); got != abs {
		t.Errorf("stdout = %q, want %q", got, abs)
	}

	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read scaffolded file: %v", err)
	}
	if string(body) != scaffoldTemplate {
		t.Errorf("scaffolded body mismatch\n--- got ---\n%s--- want ---\n%s", body, scaffoldTemplate)
	}
}

func TestNew_RefusesToOverwriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "code-review.md")
	original := []byte("untouched body\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	deps := newCmdDeps(t, dir)
	_, _, err := executeRootWith(t, deps, "new", "code-review")

	var existsErr *PromptFileExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError", err, err)
	}
	if existsErr.ID != "code-review" {
		t.Errorf("PromptFileExistsError.ID = %q, want %q", existsErr.ID, "code-review")
	}
	if existsErr.Path != target {
		t.Errorf("PromptFileExistsError.Path = %q, want %q", existsErr.Path, target)
	}
	if ExitCode(err) != ExitPrompt {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitPrompt)
	}

	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != string(original) {
		t.Errorf("file body changed: got %q, want %q", body, original)
	}
}

func TestNew_RefusesCrossSubdirIDCollision(t *testing.T) {
	dir := t.TempDir()
	// Prompt store walks subdirs and uses filename stem as id; an existing
	// nested file with the same stem would collide on the next list/show.
	subdir := filepath.Join(dir, "team")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := filepath.Join(subdir, "code-review.md")
	if err := os.WriteFile(existing, []byte("# team override\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	deps := newCmdDeps(t, dir)
	_, _, err := executeRootWith(t, deps, "new", "code-review")

	var existsErr *PromptFileExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError", err, err)
	}
	if existsErr.ID != "code-review" {
		t.Errorf("ID = %q, want %q", existsErr.ID, "code-review")
	}
	if existsErr.Path != existing {
		t.Errorf("Path = %q, want %q (the colliding file)", existsErr.Path, existing)
	}

	// Top-level target file must not be created.
	if _, err := os.Stat(filepath.Join(dir, "code-review.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("top-level code-review.md should not exist, stat err = %v", err)
	}
}

func TestNew_IgnoresHiddenSiblingsWhenScanningCollisions(t *testing.T) {
	// Hidden basenames (and files inside hidden dirs) are skipped by the
	// store's discovery, so they must not block scaffolding.
	dir := t.TempDir()
	hiddenDir := filepath.Join(dir, ".cache")
	if err := os.MkdirAll(hiddenDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "code-review.md"), []byte("hidden\n"), 0o600); err != nil {
		t.Fatalf("seed cached: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".code-review.md"), []byte("dotfile\n"), 0o600); err != nil {
		t.Fatalf("seed dotfile: %v", err)
	}

	deps := newCmdDeps(t, dir)
	if _, _, err := executeRootWith(t, deps, "new", "code-review"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "code-review.md")); err != nil {
		t.Errorf("scaffold not written: %v", err)
	}
}

func TestNew_RejectsBadID(t *testing.T) {
	dir := t.TempDir()
	deps := newCmdDeps(t, dir)

	_, _, err := executeRootWith(t, deps, "new", "team/review")
	var invalid *InvalidNewIDError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %T %v, want *InvalidNewIDError", err, err)
	}
	if ExitCode(err) != ExitUsage {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitUsage)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dir should remain empty, got %d entries", len(entries))
	}
}

func TestNew_ExplicitPromptsDirMissingIsHardError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	deps := newCmdDeps(t, missing)

	_, _, err := executeRootWith(t, deps, "new", "alpha")
	var dirMissing *store.PromptsDirMissingError
	if !errors.As(err, &dirMissing) {
		t.Fatalf("err = %T %v, want *store.PromptsDirMissingError", err, err)
	}
	if ExitCode(err) != ExitUsage {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitUsage)
	}
}

func TestNew_RequiresExactlyOneArg(t *testing.T) {
	dir := t.TempDir()
	deps := newCmdDeps(t, dir)

	for _, args := range [][]string{
		{"new"},
		{"new", "a", "b"},
	} {
		_, _, err := executeRootWith(t, deps, args...)
		if err == nil {
			t.Errorf("args %v: expected error", args)
			continue
		}
		if ExitCode(err) != ExitUsage {
			t.Errorf("args %v: ExitCode = %d, want %d (%v)", args, ExitCode(err), ExitUsage, err)
		}
	}
}

func TestEnsureScaffoldDir_AutoCreateMakesNestedDir(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "nested", "deeper", "prompts")
	if err := ensureScaffoldDir(target, true); err != nil {
		t.Fatalf("ensureScaffoldDir: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected directory, got %s", info.Mode())
	}
}

func TestEnsureScaffoldDir_NoAutoCreateRejectsMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	err := ensureScaffoldDir(missing, false)
	var dirMissing *store.PromptsDirMissingError
	if !errors.As(err, &dirMissing) {
		t.Fatalf("err = %T %v, want *store.PromptsDirMissingError", err, err)
	}
	if dirMissing.Path != missing {
		t.Errorf("Path = %q, want %q", dirMissing.Path, missing)
	}
}

// newCmdDeps builds a Deps suitable for `tprompt new` tests. The supplied dir
// becomes an explicit prompts_dir so the path resolver returns it verbatim
// without consulting XDG/home and without auto-creating it.
func newCmdDeps(t *testing.T, promptsDir string) Deps {
	t.Helper()
	deps := workingDeps(t, &fakeStore{})
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{PromptsDir: promptsDir}, nil
	}
	return deps
}
