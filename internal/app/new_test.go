package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/promptsource"
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

func TestNew_PrintsAddBodyHintWhenStderrIsTTY(t *testing.T) {
	dir := t.TempDir()
	deps := newCmdDeps(t, dir)

	forceStreamsTTY(t)

	stdout, stderr, err := executeRootWith(t, deps, "new", "code-review")
	if err != nil {
		t.Fatalf("executeRootWith: %v", err)
	}

	// stdout stays exactly the created path (scripting contract).
	abs, err := filepath.Abs(filepath.Join(dir, "code-review.md"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got := strings.TrimRight(stdout, "\n"); got != abs {
		t.Errorf("stdout = %q, want %q (hint must not pollute stdout)", got, abs)
	}
	// The hint is emitted on stderr and points at the created file's path
	// (self-contained, so it survives stdout redirection).
	if !strings.Contains(stderr, "add your prompt body") || !strings.Contains(stderr, abs) {
		t.Errorf("stderr = %q, want add-a-body hint naming the created path %q", stderr, abs)
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

func TestNew_AdditionalPromptsDirThatIsFileIsHardError(t *testing.T) {
	primary := t.TempDir()
	fileSource := filepath.Join(t.TempDir(), "team-prompts")
	if err := os.WriteFile(fileSource, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	deps := newCmdDepsWithAdditional(t, primary, []string{fileSource})

	_, _, err := executeRootWith(t, deps, "new", "alpha")
	var dirMissing *store.PromptsDirMissingError
	if !errors.As(err, &dirMissing) {
		t.Fatalf("err = %T %v, want *store.PromptsDirMissingError", err, err)
	}
	if dirMissing.Path != fileSource {
		t.Fatalf("Path = %q, want %q", dirMissing.Path, fileSource)
	}
	if _, statErr := os.Stat(filepath.Join(primary, "alpha.md")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("primary alpha.md should not exist, stat err = %v", statErr)
	}
}

func TestNew_ProjectWritesAtGitRootAndAutoCreatesDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "cmd", "tool")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	globalPrompts := filepath.Join(t.TempDir(), "global-prompts")
	if err := os.Mkdir(globalPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	deps := newCmdDeps(t, globalPrompts)
	stdout, stderr, err := executeRootWith(t, deps, "new", "project-review", "--project")
	if err != nil {
		t.Fatalf("executeRootWith: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	target := filepath.Join(root, "tprompt", "project-review.md")
	abs, err := filepath.Abs(target)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	abs = mustEvalSymlinks(t, abs)
	if got := strings.TrimRight(stdout, "\n"); got != abs {
		t.Errorf("stdout = %q, want %q", got, abs)
	}

	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read scaffolded project file: %v", err)
	}
	if string(body) != scaffoldTemplate {
		t.Errorf("scaffolded body mismatch\n--- got ---\n%s--- want ---\n%s", body, scaffoldTemplate)
	}
}

func TestNew_ProjectOutsideGitTreeFailsClearly(t *testing.T) {
	t.Chdir(t.TempDir())

	deps := newCmdDeps(t, filepath.Join(t.TempDir(), "global-prompts"))
	_, _, err := executeRootWith(t, deps, "new", "project-review", "--project")

	var notFound *promptsource.ProjectRootNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %T %v, want *promptsource.ProjectRootNotFoundError", err, err)
	}
	if ExitCode(err) != ExitUsage {
		t.Errorf("ExitCode = %d, want %d", ExitCode(err), ExitUsage)
	}
}

func TestNew_ProjectIgnoresHomeGitRoot(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(home, "scratch", "work")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Chdir(cwd)

	globalPrompts := filepath.Join(t.TempDir(), "global-prompts")
	if err := os.Mkdir(globalPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	deps := newCmdDeps(t, globalPrompts)
	_, _, err := executeRootWith(t, deps, "new", "home-leak", "--project")

	var notFound *promptsource.ProjectRootNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %T %v, want *promptsource.ProjectRootNotFoundError", err, err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "tprompt", "home-leak.md")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("home project prompt should not exist, stat err = %v", statErr)
	}
}

func TestNew_ProjectIgnoresExplicitMissingGlobalDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	missingGlobal := filepath.Join(t.TempDir(), "missing-global")
	deps := newCmdDeps(t, missingGlobal)
	stdout, _, err := executeRootWith(t, deps, "new", "project-review", "--project")
	if err != nil {
		t.Fatalf("new --project: %v", err)
	}

	target := filepath.Join(root, "tprompt", "project-review.md")
	wantPath := mustEvalSymlinks(t, target)
	if got := strings.TrimRight(stdout, "\n"); got != wantPath {
		t.Errorf("stdout = %q, want %q", got, wantPath)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("stat project prompt: %v", err)
	}
}

func TestNew_ProjectDoesNotResolveDefaultGlobalDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Chdir(root)

	deps := workingDeps(t, &fakeStore{})
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{}, nil
	}
	stdout, _, err := executeRootWith(t, deps, "new", "project-review", "--project")
	if err != nil {
		t.Fatalf("new --project: %v", err)
	}
	target := filepath.Join(root, "tprompt", "project-review.md")
	wantPath := mustEvalSymlinks(t, target)
	if got := strings.TrimRight(stdout, "\n"); got != wantPath {
		t.Errorf("stdout = %q, want %q", got, wantPath)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("stat project prompt: %v", err)
	}
}

func TestNew_ProjectRefusesExistingProjectFile(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	projectPrompts := filepath.Join(root, "tprompt")
	if err := os.Mkdir(projectPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projectPrompts, "project-review.md")
	original := []byte("project body\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	globalPrompts := filepath.Join(t.TempDir(), "global-prompts")
	if err := os.Mkdir(globalPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	deps := newCmdDeps(t, globalPrompts)
	_, _, err := executeRootWith(t, deps, "new", "project-review", "--project")

	var existsErr *PromptFileExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError", err, err)
	}
	wantPath := mustEvalSymlinks(t, target)
	if existsErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", existsErr.Path, wantPath)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != string(original) {
		t.Errorf("file body changed: got %q, want %q", body, original)
	}
}

func TestNew_ProjectAllowsGlobalIDShadow(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	globalPrompts := filepath.Join(t.TempDir(), "global-prompts")
	if err := os.Mkdir(globalPrompts, 0o700); err != nil {
		t.Fatal(err)
	}
	globalPrompt := filepath.Join(globalPrompts, "code-review.md")
	if err := os.WriteFile(globalPrompt, []byte("global body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	deps := newCmdDeps(t, globalPrompts)
	stdout, _, err := executeRootWith(t, deps, "new", "code-review", "--project")
	if err != nil {
		t.Fatalf("new --project: %v", err)
	}
	target := filepath.Join(root, "tprompt", "code-review.md")
	wantPath := mustEvalSymlinks(t, target)
	if got := strings.TrimRight(stdout, "\n"); got != wantPath {
		t.Errorf("stdout = %q, want %q", got, wantPath)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read project prompt: %v", err)
	}
	if string(body) != scaffoldTemplate {
		t.Errorf("project body mismatch\n--- got ---\n%s--- want ---\n%s", body, scaffoldTemplate)
	}
	globalBody, err := os.ReadFile(globalPrompt)
	if err != nil {
		t.Fatalf("read global prompt: %v", err)
	}
	if string(globalBody) != "global body\n" {
		t.Errorf("global prompt changed: %q", globalBody)
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

func TestNew_ForceOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "code-review.md")
	if err := os.WriteFile(target, []byte("stale body\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	deps := newCmdDeps(t, dir)
	stdout, _, err := executeRootWith(t, deps, "new", "code-review", "--force")
	if err != nil {
		t.Fatalf("executeRootWith: %v", err)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got := strings.TrimRight(stdout, "\n"); got != abs {
		t.Errorf("stdout = %q, want %q", got, abs)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != scaffoldTemplate {
		t.Errorf("--force should reset the file to a fresh scaffold\n--- got ---\n%s--- want ---\n%s", body, scaffoldTemplate)
	}
}

func TestNew_ForceStillRefusesCrossSubdirCollision(t *testing.T) {
	// --force overwrites the exact target only. A same-stem file in a
	// subdirectory cannot be overwritten in place, and writing the top-level
	// target anyway would leave a duplicate-ID store, so --force still refuses
	// and names the colliding file.
	dir := t.TempDir()
	subdir := filepath.Join(dir, "team")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	nested := filepath.Join(subdir, "code-review.md")
	if err := os.WriteFile(nested, []byte("# nested\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	deps := newCmdDeps(t, dir)
	_, _, err := executeRootWith(t, deps, "new", "code-review", "--force")
	var existsErr *PromptFileExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError even with --force", err, err)
	}
	if existsErr.Path != nested {
		t.Errorf("Path = %q, want %q (the colliding nested file)", existsErr.Path, nested)
	}
	// Top-level target must not be created, and the nested file is untouched.
	if _, err := os.Stat(filepath.Join(dir, "code-review.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("top-level code-review.md should not exist, stat err = %v", err)
	}
	if got, err := os.ReadFile(nested); err != nil || string(got) != "# nested\n" {
		t.Errorf("nested same-stem file changed: got %q err %v", got, err)
	}
}

func TestNew_ForceCreatesAbsentTarget(t *testing.T) {
	// --force on a non-existent target is an ordinary create (nothing to
	// overwrite): it must not error and writes the scaffold. The overwrite
	// (rename) path is reserved for an actually-existing target, so a fresh
	// create still goes through the O_EXCL guard.
	dir := t.TempDir()
	deps := newCmdDeps(t, dir)
	stdout, _, err := executeRootWith(t, deps, "new", "code-review", "--force")
	if err != nil {
		t.Fatalf("executeRootWith: %v", err)
	}
	abs, err := filepath.Abs(filepath.Join(dir, "code-review.md"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got := strings.TrimRight(stdout, "\n"); got != abs {
		t.Errorf("stdout = %q, want %q", got, abs)
	}
	body, err := os.ReadFile(filepath.Join(dir, "code-review.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != scaffoldTemplate {
		t.Errorf("body mismatch\n--- got ---\n%s--- want ---\n%s", body, scaffoldTemplate)
	}
}

func TestNew_ForceRefusesHardlinkedDuplicate(t *testing.T) {
	// A same-id file hard-linked into a subdirectory shares the target's inode
	// but is a distinct path, so the store sees two same-stem files: a real
	// duplicate ID. --force must refuse (collision detection is path-based, not
	// inode-based).
	dir := t.TempDir()
	target := filepath.Join(dir, "code-review.md")
	if err := os.WriteFile(target, []byte("body\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	subdir := filepath.Join(dir, "team")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(subdir, "code-review.md")
	if err := os.Link(target, link); err != nil {
		t.Skipf("hardlink unsupported on this filesystem: %v", err)
	}

	deps := newCmdDeps(t, dir)
	_, _, err := executeRootWith(t, deps, "new", "code-review", "--force")
	var existsErr *PromptFileExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError", err, err)
	}
	if existsErr.Path != link {
		t.Errorf("Path = %q, want %q (the hard-linked duplicate)", existsErr.Path, link)
	}
}

func TestNew_ForceRefusesWhenTargetAndDuplicateBothExist(t *testing.T) {
	// If the exact target AND another same-stem file both exist, --force must
	// still refuse: overwriting the target would leave the other file as a
	// lingering duplicate ID. (Regression guard: the collision scan must
	// inspect every match, not stop at the first.)
	dir := t.TempDir()
	target := filepath.Join(dir, "code-review.md")
	if err := os.WriteFile(target, []byte("top-level body\n"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	subdir := filepath.Join(dir, "team")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	nested := filepath.Join(subdir, "code-review.md")
	if err := os.WriteFile(nested, []byte("# nested\n"), 0o600); err != nil {
		t.Fatalf("seed nested: %v", err)
	}

	deps := newCmdDeps(t, dir)
	_, _, err := executeRootWith(t, deps, "new", "code-review", "--force")
	var existsErr *PromptFileExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError even with --force", err, err)
	}
	if existsErr.Path != nested {
		t.Errorf("Path = %q, want %q (the lingering duplicate)", existsErr.Path, nested)
	}
	// Neither file is modified.
	if got, err := os.ReadFile(target); err != nil || string(got) != "top-level body\n" {
		t.Errorf("target changed: got %q err %v", got, err)
	}
	if got, err := os.ReadFile(nested); err != nil || string(got) != "# nested\n" {
		t.Errorf("nested changed: got %q err %v", got, err)
	}
}

func TestNew_ForceRefusesCollisionInAdditionalSource(t *testing.T) {
	// A same-id prompt living in an additional global source is a real
	// duplicate-ID collision that --force cannot overwrite in place; --force
	// must still refuse rather than create a second global prompt.
	primary := t.TempDir()
	additional := t.TempDir()
	other := filepath.Join(additional, "code-review.md")
	if err := os.WriteFile(other, []byte("# other source\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	deps := newCmdDepsWithAdditional(t, primary, []string{additional})
	_, _, err := executeRootWith(t, deps, "new", "code-review", "--force")
	var existsErr *PromptFileExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError even with --force", err, err)
	}
	if existsErr.Path != other {
		t.Errorf("Path = %q, want %q (the additional-source file)", existsErr.Path, other)
	}
	if _, err := os.Stat(filepath.Join(primary, "code-review.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("primary code-review.md should not exist, stat err = %v", err)
	}
}

func TestNew_EditLaunchesEditorAndSuppressesHint(t *testing.T) {
	dir := t.TempDir()
	editor := writeFakeEditor(t)

	deps := newCmdDeps(t, dir)
	// $EDITOR may carry leading args; the scaffold path must be appended last.
	// The fake editor edits whichever path lands in its final argv slot, so a
	// "--wait"-style prefix exercises both the shell parse and the
	// append-target-last behavior.
	deps.Env = func(k string) string {
		if k == "EDITOR" {
			return editor + " --wait"
		}
		return ""
	}

	// Editor launch is gated on the command's stdin AND stdout being ttys; force
	// all streams on (the blanket gate would also fire the add-a-body hint, so a
	// suppressed hint proves the editor branch took over).
	forceStreamsTTY(t)

	_, stderr, err := executeRootWith(t, deps, "new", "code-review", "--edit")
	if err != nil {
		t.Fatalf("executeRootWith: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, "code-review.md"))
	if err != nil {
		t.Fatalf("read scaffolded file: %v", err)
	}
	if !strings.Contains(string(body), "EDITED-BY-FAKE-EDITOR") {
		t.Errorf("editor did not run on the scaffold path: body = %q", body)
	}
	if strings.Contains(stderr, "add your prompt body") {
		t.Errorf("hint must be suppressed once the editor launches, stderr = %q", stderr)
	}
}

func TestNew_EditNotLaunchedWhenStdoutRedirected(t *testing.T) {
	// The editor inherits the command's stdout, so launching while stdout is
	// redirected (not a tty) would corrupt the stdout-is-the-path contract.
	// With stdin a tty but stdout not, --edit must skip the editor entirely.
	dir := t.TempDir()
	editor := writeFakeEditor(t)
	deps := newCmdDeps(t, dir)
	deps.Env = func(k string) string {
		if k == "EDITOR" {
			return editor
		}
		return ""
	}

	// Mark only the command's stdin as a tty (stdout/stderr are "redirected").
	// runNew is called directly so the streams under test are the ones the gate
	// and the editor wiring actually consult.
	orig := streamIsTTY
	streamIsTTY = func(v any) bool { return v == deps.Stdin }
	t.Cleanup(func() { streamIsTTY = orig })

	if err := runNew(deps, "code-review", newFlags{edit: true}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "code-review.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != scaffoldTemplate {
		t.Errorf("editor must not run when stdout is redirected; body = %q", body)
	}
}

func TestNew_EditEditorFailureIsNonFatal(t *testing.T) {
	// A non-zero editor exit (or an unstartable $EDITOR) must not abort `new`:
	// the file was already created, so the command succeeds (exit 0), still
	// prints the path, and reports the editor failure on stderr.
	dir := t.TempDir()
	deps := newCmdDeps(t, dir)
	deps.Env = func(k string) string {
		if k == "EDITOR" {
			return "tprompt-no-such-editor-binary"
		}
		return ""
	}

	forceStreamsTTY(t)

	stdout, stderr, err := executeRootWith(t, deps, "new", "code-review", "--edit")
	if err != nil {
		t.Fatalf("editor failure must not abort new: %v", err)
	}
	abs, err := filepath.Abs(filepath.Join(dir, "code-review.md"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got := strings.TrimRight(stdout, "\n"); got != abs {
		t.Errorf("stdout = %q, want %q", got, abs)
	}
	if !strings.Contains(stderr, "editor exited with error") {
		t.Errorf("stderr = %q, want editor-failure note", stderr)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Errorf("scaffold should exist despite editor failure: %v", err)
	}
}

func TestNew_EditNoopWhenEditorUnsetStillPrintsHint(t *testing.T) {
	dir := t.TempDir()
	deps := newCmdDeps(t, dir) // Env returns "" for EDITOR.

	forceStreamsTTY(t)

	stdout, stderr, err := executeRootWith(t, deps, "new", "code-review", "--edit")
	if err != nil {
		t.Fatalf("executeRootWith: %v", err)
	}
	abs, err := filepath.Abs(filepath.Join(dir, "code-review.md"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got := strings.TrimRight(stdout, "\n"); got != abs {
		t.Errorf("stdout = %q, want %q", got, abs)
	}
	// With no $EDITOR the launch is a clean no-op, so the add-a-body hint still
	// fires (and the scaffold is left at the template).
	if !strings.Contains(stderr, "add your prompt body") || !strings.Contains(stderr, abs) {
		t.Errorf("stderr = %q, want add-a-body hint naming %q", stderr, abs)
	}
	body, err := os.ReadFile(filepath.Join(dir, "code-review.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != scaffoldTemplate {
		t.Errorf("scaffold should be untouched when no editor runs")
	}
}

// --- writePromptFile: the shared collision-check + atomic write seam (AUR-523).
// These lock the "accepts arbitrary content" contract that `import` relies on,
// independently of the scaffold template that `new` happens to pass through it.

func globalSources(dir string) []promptsource.Source {
	return []promptsource.Source{{Path: dir, Scope: promptsource.ScopeGlobal}}
}

func TestWritePromptFile_CreatesArbitraryContent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "imported.md")
	content := []byte("---\ntitle: hi\n---\narbitrary body\n")

	if err := writePromptFile(target, globalSources(dir), promptsource.ScopeGlobal, "imported", content, false); err != nil {
		t.Fatalf("writePromptFile: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch\n got: %q\nwant: %q", got, content)
	}
}

func TestWritePromptFile_RefusesExistingTargetWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "imported.md")
	original := []byte("original\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := writePromptFile(target, globalSources(dir), promptsource.ScopeGlobal, "imported", []byte("new\n"), false)
	var existsErr *PromptFileExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError", err, err)
	}
	if existsErr.Path != target {
		t.Errorf("Path = %q, want %q", existsErr.Path, target)
	}
	if got, _ := os.ReadFile(target); string(got) != string(original) {
		t.Errorf("file changed: %q", got)
	}
}

func TestWritePromptFile_OverwriteReplacesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "imported.md")
	if err := os.WriteFile(target, []byte("stale\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	fresh := []byte("refreshed body\n")

	if err := writePromptFile(target, globalSources(dir), promptsource.ScopeGlobal, "imported", fresh, true); err != nil {
		t.Fatalf("writePromptFile overwrite: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != string(fresh) {
		t.Errorf("overwrite did not refresh: %q", got)
	}
}

func TestWritePromptFile_OverwriteAbsentTargetUsesExclCreate(t *testing.T) {
	// overwrite=true on a not-yet-existing target must still take the O_EXCL
	// create path (not the rename path), so a concurrent creator racing between
	// the scan and the write is refused rather than silently clobbered. Proven
	// indirectly: the write succeeds and the file lands with exactly the content.
	dir := t.TempDir()
	target := filepath.Join(dir, "imported.md")
	content := []byte("created under overwrite\n")

	if err := writePromptFile(target, globalSources(dir), promptsource.ScopeGlobal, "imported", content, true); err != nil {
		t.Fatalf("writePromptFile: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != string(content) {
		t.Errorf("content mismatch: %q", got)
	}
}

func TestWritePromptFile_OverwriteStillRefusesOtherPathCollision(t *testing.T) {
	// overwrite replaces the exact target only; a same-stem file at a different
	// path is a duplicate ID overwrite cannot resolve in place, so it refuses
	// even with overwrite=true and names the colliding file.
	dir := t.TempDir()
	target := filepath.Join(dir, "imported.md")
	subdir := filepath.Join(dir, "team")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	nested := filepath.Join(subdir, "imported.md")
	if err := os.WriteFile(nested, []byte("# nested\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := writePromptFile(target, globalSources(dir), promptsource.ScopeGlobal, "imported", []byte("body\n"), true)
	var existsErr *PromptFileExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %T %v, want *PromptFileExistsError even with overwrite", err, err)
	}
	if existsErr.Path != nested {
		t.Errorf("Path = %q, want %q (the colliding nested file)", existsErr.Path, nested)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("target must not be created on refusal, stat err = %v", err)
	}
}

// forceStreamsTTY makes every command stream report as a terminal for the
// duration of the test, satisfying the --edit launch gate and the hint gate
// without a real pty.
func forceStreamsTTY(t *testing.T) {
	t.Helper()
	orig := streamIsTTY
	streamIsTTY = func(any) bool { return true }
	t.Cleanup(func() { streamIsTTY = orig })
}

// writeFakeEditor writes an executable script that appends a fixed marker to
// its final argument, then returns its path. It lets `--edit` tests assert the
// editor ran against the scaffold path without a real interactive editor.
func writeFakeEditor(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-editor.sh")
	script := "#!/bin/sh\nfor f in \"$@\"; do last=\"$f\"; done\nprintf 'EDITED-BY-FAKE-EDITOR\\n' >> \"$last\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	return path
}

// newCmdDeps builds a Deps suitable for `tprompt new` tests. The supplied dir
// becomes an explicit prompts_dir so the path resolver returns it verbatim
// without consulting XDG/home and without auto-creating it.
func newCmdDeps(t *testing.T, promptsDir string) Deps {
	t.Helper()
	return newCmdDepsWithAdditional(t, promptsDir, nil)
}

func newCmdDepsWithAdditional(t *testing.T, promptsDir string, additional []string) Deps {
	t.Helper()
	deps := workingDeps(t, &fakeStore{})
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			PromptsDir:            promptsDir,
			AdditionalPromptsDirs: additional,
		}, nil
	}
	return deps
}

func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", path, err)
	}
	return evaluated
}
