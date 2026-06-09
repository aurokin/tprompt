package app

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/aurokin/tprompt/internal/clipboard"
	"github.com/aurokin/tprompt/internal/config"
	"github.com/aurokin/tprompt/internal/importtui"
	"github.com/aurokin/tprompt/internal/picker"
	"github.com/aurokin/tprompt/internal/store"
	"github.com/aurokin/tprompt/internal/submitter"
	"github.com/aurokin/tprompt/internal/tmux"
	"github.com/aurokin/tprompt/internal/tui"
	"github.com/aurokin/tprompt/internal/wispr"
)

func TestRootCmdRegistersAllSubcommands(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	want := []string{"list", "show", "send", "paste", "doctor", "tui", "pick", "new", "init", "import"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

func TestDispatchArgsRewritesBareInvocationInTmuxTTY(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(key string) string {
		if key == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}
	tty := func() bool { return true }

	got := dispatchArgs(root, nil, env, tty)
	if len(got) != 1 || got[0] != "tui" {
		t.Fatalf("bare args should rewrite to [tui], got %v", got)
	}

	got = dispatchArgs(root, []string{"--target-pane", "%0"}, env, tty)
	want := []string{"tui", "--target-pane", "%0"}
	if !stringSliceEqual(got, want) {
		t.Fatalf("flagged bare args should prepend tui, got %v want %v", got, want)
	}
}

func TestDispatchArgsPreservesSubcommandInvocation(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"list"}, env, tty)
	if !stringSliceEqual(got, []string{"list"}) {
		t.Fatalf("subcommand args should pass through, got %v", got)
	}
}

func TestDispatchArgsPreservesExplicitHelpFlags(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	for _, helpArg := range []string{"--help", "-h"} {
		got := dispatchArgs(root, []string{helpArg}, env, tty)
		if !stringSliceEqual(got, []string{helpArg}) {
			t.Fatalf("%q should pass through, got %v", helpArg, got)
		}
	}
}

func TestDispatchArgsRewritesWhenHelpIsFlagValue(t *testing.T) {
	// Regression: `tprompt --config help --target-pane %0` should still
	// rewrite — "help" is the value of --config, not a help invocation.
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	args := []string{"--config", "help", "--target-pane", "%0"}
	got := dispatchArgs(root, args, env, tty)
	want := []string{"tui", "--config", "help", "--target-pane", "%0"}
	if !stringSliceEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDispatchArgsSkipsRewriteWithoutTmux(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "" }
	tty := func() bool { return true }

	got := dispatchArgs(root, nil, env, tty)
	if len(got) != 0 {
		t.Fatalf("bare args outside tmux should not be rewritten, got %v", got)
	}
}

func TestDispatchArgsSkipsRewriteWithoutTTY(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return false }

	got := dispatchArgs(root, nil, env, tty)
	if len(got) != 0 {
		t.Fatalf("bare args without tty should not be rewritten, got %v", got)
	}
}

func TestImportBareDispatchesDefaultSourceInTmuxInteractiveTTY(t *testing.T) {
	withStdinTTY(t, true)
	forceStreamsTTY(t)
	withImportSourceRegistry(t, fakeImportSource{})
	deps, called := bareImportDeps(t)

	_, _, err := executeRootWith(t, deps, "import")
	if err != nil {
		t.Fatalf("bare import dispatch: %v", err)
	}
	if !*called {
		t.Fatal("bare import in tmux+interactive tty did not construct the default interactive renderer")
	}
}

func TestImportBareFallsBackToHelpWhenGateFails(t *testing.T) {
	withImportSourceRegistry(t, fakeImportSource{})
	cases := []struct {
		name     string
		env      func(string) string
		stdinTTY bool
		streams  bool
	}{
		{
			name:     "outside tmux",
			env:      func(string) string { return "" },
			stdinTTY: true,
			streams:  true,
		},
		{
			name:     "process stdin not tty",
			env:      func(string) string { return "/tmp/tmux-0/default,1,0" },
			stdinTTY: false,
			streams:  true,
		},
		{
			name:     "injected streams not tty",
			env:      func(string) string { return "/tmp/tmux-0/default,1,0" },
			stdinTTY: true,
			streams:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withStdinTTY(t, tc.stdinTTY)
			if tc.streams {
				forceStreamsTTY(t)
			}
			deps, called := bareImportDeps(t)
			deps.Env = tc.env

			stdout, _, err := executeRootWith(t, deps, "import")
			if err != nil {
				t.Fatalf("bare import help: %v", err)
			}
			if *called {
				t.Fatal("default interactive renderer was constructed despite a failed gate")
			}
			if !strings.Contains(stdout, "fake") {
				t.Fatalf("bare import should print source-dispatch help, got:\n%s", stdout)
			}
		})
	}
}

func TestImportBareCobraParsingBehavior(t *testing.T) {
	withStdinTTY(t, true)
	forceStreamsTTY(t)
	withImportSourceRegistry(t, fakeImportSource{})

	cases := []struct {
		name       string
		args       []string
		wantCalled bool
		wantErr    string
	}{
		{"root flag before import", []string{"--config", "x", "import"}, true, ""},
		{"root flag after import", []string{"import", "--config", "x"}, true, ""},
		{"root flag value is import", []string{"--config", "import", "import"}, true, ""},
		{"root flag value looks like shorthand", []string{"import", "--config", "-v"}, true, ""},
		{"dangling root flag", []string{"import", "--config"}, false, "flag needs an argument"},
		{"source flag without source", []string{"import", "--dry-run"}, false, "unknown flag"},
		{"stray positional", []string{"import", "bogus"}, false, "unknown command"},
		{"stray import positional", []string{"import", "import"}, false, "unknown command"},
		{"explicit source", []string{"import", "fake"}, false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps, called := bareImportDeps(t)
			_, _, err := executeRootWith(t, deps, tc.args...)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("executeRootWith(%v): %v", tc.args, err)
			}
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("executeRootWith(%v) err = nil, want containing %q", tc.args, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("executeRootWith(%v) err = %q, want containing %q", tc.args, err, tc.wantErr)
				}
			}
			if *called != tc.wantCalled {
				t.Fatalf("default interactive renderer called = %v, want %v", *called, tc.wantCalled)
			}
		})
	}
}

func TestRootBareInvocationFallsBackToHelp(t *testing.T) {
	deps := fakeDeps(t)
	root := NewRootCmd(deps)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.RunE(root, nil); err != nil {
		t.Fatalf("want nil from Help path, got %v", err)
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestConfigFlagExists(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	f := root.PersistentFlags().Lookup("config")
	if f == nil {
		t.Fatal("--config flag not registered")
	}
}

func withStdinTTY(t *testing.T, isTTY bool) {
	t.Helper()
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return isTTY }
	t.Cleanup(func() { stdinIsTTY = orig })
}

func withImportSourceRegistry(t *testing.T, sources ...ImportSource) {
	t.Helper()
	orig := importSourceRegistry
	importSourceRegistry = sources
	t.Cleanup(func() { importSourceRegistry = orig })
}

func bareImportDeps(t *testing.T) (Deps, *bool) {
	t.Helper()
	called := false
	deps := newCmdDeps(t, t.TempDir())
	deps.Env = func(key string) string {
		if key == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}
	deps.NewImportRenderer = func() (importtui.Renderer, error) {
		called = true
		return &recordingImportRenderer{decide: confirmAll}, nil
	}
	return deps, &called
}

func fakeDeps(t *testing.T) Deps {
	t.Helper()
	configPath := ""
	return Deps{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		Stdin:      &bytes.Buffer{},
		Env:        func(string) string { return "" },
		ConfigPath: &configPath,
		LoadConfig: func(string) (config.Resolved, error) {
			return config.Resolved{}, ErrNotImplemented
		},
		LoadPasteConfig: func(string) (config.Resolved, error) {
			return config.Resolved{}, ErrNotImplemented
		},
		NewStore: func(config.Resolved) (store.Store, error) {
			return nil, ErrNotImplemented
		},
		NewWisprReader: func(string) wispr.Reader {
			return &fakeWisprReader{err: ErrNotImplemented}
		},
		NewTmux: func() (tmux.Adapter, error) {
			return nil, ErrNotImplemented
		},
		NewClip: func(config.Resolved) (clipboard.Reader, error) {
			return nil, ErrNotImplemented
		},
		NewPicker: func(config.Resolved) (picker.Picker, error) {
			return nil, ErrNotImplemented
		},
		NewRenderer: func(config.Resolved, store.Store, submitter.Submitter) (tui.Renderer, error) {
			return nil, ErrNotImplemented
		},
	}
}
