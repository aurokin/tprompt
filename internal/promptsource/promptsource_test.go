package promptsource

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hsadler/tprompt/internal/config"
)

func TestResolveTable(t *testing.T) {
	t.Parallel()

	const explicit = "/srv/prompts"
	const additionalA = "/srv/team-prompts"
	const additionalB = "/srv/org-prompts"
	const xdg = "/var/xdg"
	const home = "/users/jane"

	cases := []struct {
		name    string
		cfg     config.Resolved
		env     map[string]string
		homeDir string
		want    []Source
		wantErr bool
	}{
		{
			name: "explicit prompts_dir wins over XDG and additional dirs follow",
			cfg: config.Resolved{
				PromptsDir:            explicit,
				AdditionalPromptsDirs: []string{additionalA, additionalB},
			},
			env:     map[string]string{"XDG_CONFIG_HOME": xdg},
			homeDir: home,
			want: []Source{
				{
					Path:               explicit,
					Scope:              ScopeGlobal,
					AutoCreateOnAccess: false,
				},
				{
					Path:               additionalA,
					Scope:              ScopeGlobal,
					AutoCreateOnAccess: false,
					Optional:           true,
				},
				{
					Path:               additionalB,
					Scope:              ScopeGlobal,
					AutoCreateOnAccess: false,
					Optional:           true,
				},
			},
		},
		{
			name:    "explicit prompts_dir wins with no env",
			cfg:     config.Resolved{PromptsDir: explicit},
			homeDir: "",
			want: []Source{{
				Path:               explicit,
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: false,
			}},
		},
		{
			name: "default uses XDG when set and additional dirs follow",
			cfg: config.Resolved{
				AdditionalPromptsDirs: []string{additionalA},
			},
			env:     map[string]string{"XDG_CONFIG_HOME": xdg},
			homeDir: home,
			want: []Source{
				{
					Path:               filepath.Join(xdg, "tprompt", "prompts"),
					Scope:              ScopeGlobal,
					AutoCreateOnAccess: true,
				},
				{
					Path:               additionalA,
					Scope:              ScopeGlobal,
					AutoCreateOnAccess: false,
					Optional:           true,
				},
			},
		},
		{
			name:    "default falls back to home when XDG unset",
			cfg:     config.Resolved{},
			env:     map[string]string{},
			homeDir: home,
			want: []Source{{
				Path:               filepath.Join(home, ".config", "tprompt", "prompts"),
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: true,
			}},
		},
		{
			name:    "default falls back to home when XDG is whitespace",
			cfg:     config.Resolved{},
			env:     map[string]string{"XDG_CONFIG_HOME": "   "},
			homeDir: home,
			want: []Source{{
				Path:               filepath.Join(home, ".config", "tprompt", "prompts"),
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: true,
			}},
		},
		{
			name:    "default errors when XDG and home are both empty",
			cfg:     config.Resolved{},
			env:     map[string]string{},
			homeDir: "",
			wantErr: true,
		},
		{
			name:    "nil getenv falls back to home",
			cfg:     config.Resolved{},
			env:     nil,
			homeDir: home,
			want: []Source{{
				Path:               filepath.Join(home, ".config", "tprompt", "prompts"),
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: true,
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Resolve(tc.cfg, mapGetenv(tc.env), tc.homeDir, "", nil)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Resolve: want error, got nil (sources=%v)", got)
				}
				var unresolved *UnresolvedDefaultDirError
				if !errors.As(err, &unresolved) {
					t.Fatalf("Resolve: want *UnresolvedDefaultDirError, got %T: %v", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("Resolve mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolveProjectDiscovery(t *testing.T) {
	t.Parallel()

	const (
		home    = "/users/jane"
		global  = "/global/prompts"
		gitRoot = "/users/jane/work/repo"
	)

	tests := []struct {
		name  string
		cwd   string
		paths map[string]PathKind
		want  []Source
	}{
		{
			name: "project source activates from subdirectory",
			cwd:  filepath.Join(gitRoot, "cmd", "tool"),
			paths: map[string]PathKind{
				filepath.Join(gitRoot, ".git"):    PathDir,
				filepath.Join(gitRoot, "tprompt"): PathDir,
			},
			want: []Source{
				{Path: global, Scope: ScopeGlobal},
				{Path: filepath.Join(gitRoot, "tprompt"), Scope: ScopeProject},
			},
		},
		{
			name: "git marker before tprompt means no overlay",
			cwd:  filepath.Join(gitRoot, "cmd"),
			paths: map[string]PathKind{
				filepath.Join(gitRoot, ".git"): PathDir,
			},
			want: []Source{{Path: global, Scope: ScopeGlobal}},
		},
		{
			name: "tprompt outside git tree is ignored",
			cwd:  "/tmp/scratch/src",
			paths: map[string]PathKind{
				filepath.Join("/tmp/scratch", "tprompt"): PathDir,
			},
			want: []Source{{Path: global, Scope: ScopeGlobal}},
		},
		{
			name: "home bound ignores home tprompt",
			cwd:  filepath.Join(home, "scratch"),
			paths: map[string]PathKind{
				filepath.Join(home, "tprompt"): PathDir,
				filepath.Join(home, ".git"):    PathDir,
			},
			want: []Source{{Path: global, Scope: ScopeGlobal}},
		},
		{
			name: "git file worktree marker counts as git tree",
			cwd:  filepath.Join(gitRoot, "pkg"),
			paths: map[string]PathKind{
				filepath.Join(gitRoot, ".git"):    PathFile,
				filepath.Join(gitRoot, "tprompt"): PathDir,
			},
			want: []Source{
				{Path: global, Scope: ScopeGlobal},
				{Path: filepath.Join(gitRoot, "tprompt"), Scope: ScopeProject},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Resolve(config.Resolved{PromptsDir: global}, nil, home, tc.cwd, mapStat(tc.paths))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("Resolve mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolveProjectDiscoveryResolvesSymlinkedCWD(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join(root, ".git"),
		filepath.Join(root, "tprompt"),
		filepath.Join(root, "cmd", "tool"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	linkParent := t.TempDir()
	linkNested := filepath.Join(linkParent, "tool-link")
	if err := os.Symlink(filepath.Join(root, "cmd", "tool"), linkNested); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	global := filepath.Join(t.TempDir(), "global")
	got, err := Resolve(config.Resolved{PromptsDir: global}, nil, "", linkNested, osStatKind)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := []Source{
		{Path: global, Scope: ScopeGlobal},
		{Path: filepath.Join(mustEvalSymlinks(t, root), "tprompt"), Scope: ScopeProject},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Resolve mismatch (-want +got):\n%s", diff)
	}
}

func TestProjectRootFindsGitRootFromNestedDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "cmd", "tool")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := ProjectRoot(nested, "", os.Stat)
	if err != nil {
		t.Fatalf("ProjectRoot: %v", err)
	}
	want := mustEvalSymlinks(t, root)
	if got != want {
		t.Fatalf("ProjectRoot = %q, want %q", got, want)
	}
}

func TestProjectRootAcceptsGitFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ../.git/worktrees/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ProjectRoot(root, "", os.Stat)
	if err != nil {
		t.Fatalf("ProjectRoot: %v", err)
	}
	want := mustEvalSymlinks(t, root)
	if got != want {
		t.Fatalf("ProjectRoot = %q, want %q", got, want)
	}
}

func TestProjectRootResolvesSymlinkedCWD(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	realNested := filepath.Join(root, "cmd", "tool")
	if err := os.MkdirAll(realNested, 0o700); err != nil {
		t.Fatal(err)
	}

	linkParent := t.TempDir()
	linkNested := filepath.Join(linkParent, "tool-link")
	if err := os.Symlink(realNested, linkNested); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	got, err := ProjectRoot(linkNested, "", os.Stat)
	if err != nil {
		t.Fatalf("ProjectRoot: %v", err)
	}
	want := mustEvalSymlinks(t, root)
	if got != want {
		t.Fatalf("ProjectRoot = %q, want %q", got, want)
	}
}

func TestProjectRootStopsAtHomeBoundary(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(home, "scratch", "work")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := ProjectRoot(cwd, home, os.Stat)
	var notFound *ProjectRootNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("ProjectRoot error = %T %v, want *ProjectRootNotFoundError", err, err)
	}
}

func TestProjectRootOutsideGitTree(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	_, err := ProjectRoot(cwd, "", os.Stat)
	var notFound *ProjectRootNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("ProjectRoot error = %T %v, want *ProjectRootNotFoundError", err, err)
	}
	want := mustEvalSymlinks(t, cwd)
	if notFound.CWD != want {
		t.Fatalf("ProjectRootNotFoundError.CWD = %q, want %q", notFound.CWD, want)
	}
}

// mapGetenv returns a getenv-like func backed by m. A nil m yields a nil
// function so callers can verify Resolve handles a missing env getter.
func mapGetenv(m map[string]string) func(string) string {
	if m == nil {
		return nil
	}
	return func(k string) string { return m[k] }
}

func mapStat(paths map[string]PathKind) StatFunc {
	return func(path string) (PathKind, error) {
		if kind, ok := paths[filepath.Clean(path)]; ok {
			return kind, nil
		}
		return PathMissing, nil
	}
}

func osStatKind(path string) (PathKind, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PathMissing, nil
		}
		return PathMissing, err
	}
	if info.IsDir() {
		return PathDir, nil
	}
	return PathFile, nil
}

func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", path, err)
	}
	return evaluated
}
