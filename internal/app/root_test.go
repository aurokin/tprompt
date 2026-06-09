package app

import (
	"bytes"
	"io"
	"testing"

	"github.com/aurokin/tprompt/internal/clipboard"
	"github.com/aurokin/tprompt/internal/config"
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

	got := dispatchArgs(root, nil, env, tty, tty, "wispr")
	if len(got) != 1 || got[0] != "tui" {
		t.Fatalf("bare args should rewrite to [tui], got %v", got)
	}

	got = dispatchArgs(root, []string{"--target-pane", "%0"}, env, tty, tty, "wispr")
	want := []string{"tui", "--target-pane", "%0"}
	if !stringSliceEqual(got, want) {
		t.Fatalf("flagged bare args should prepend tui, got %v want %v", got, want)
	}
}

func TestDispatchArgsPreservesSubcommandInvocation(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"list"}, env, tty, tty, "wispr")
	if !stringSliceEqual(got, []string{"list"}) {
		t.Fatalf("subcommand args should pass through, got %v", got)
	}
}

func TestDispatchArgsPreservesExplicitHelpFlags(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	for _, helpArg := range []string{"--help", "-h"} {
		got := dispatchArgs(root, []string{helpArg}, env, tty, tty, "wispr")
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
	got := dispatchArgs(root, args, env, tty, tty, "wispr")
	want := []string{"tui", "--config", "help", "--target-pane", "%0"}
	if !stringSliceEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDispatchArgsSkipsRewriteWithoutTmux(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "" }
	tty := func() bool { return true }

	got := dispatchArgs(root, nil, env, tty, tty, "wispr")
	if len(got) != 0 {
		t.Fatalf("bare args outside tmux should not be rewritten, got %v", got)
	}
}

func TestDispatchArgsSkipsRewriteWithoutTTY(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return false }

	got := dispatchArgs(root, nil, env, tty, tty, "wispr")
	if len(got) != 0 {
		t.Fatalf("bare args without tty should not be rewritten, got %v", got)
	}
}

func TestDispatchArgsRewritesBareImportToInteractiveDefaultSource(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"import"}, env, tty, tty, "wispr")
	want := []string{"import", "wispr", "-i"}
	if !stringSliceEqual(got, want) {
		t.Fatalf("bare import in tmux+tty should rewrite to %v, got %v", want, got)
	}
}

func TestDispatchArgsAppendsImportSourceWithRootFlags(t *testing.T) {
	// A persistent root flag before a bare `import` must survive: `import` is the
	// final command token, so the source and `-i` are appended after it.
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"--config", "x", "import"}, env, tty, tty, "wispr")
	want := []string{"--config", "x", "import", "wispr", "-i"}
	if !stringSliceEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDispatchArgsRewritesBareImportWhenRootFlagValueIsImport(t *testing.T) {
	// Regression: `--config import import` has the literal "import" as the
	// --config value AND as the command token. Appending (rather than splicing
	// after the first "import") keeps the real command token last, so cobra
	// re-parses --config=import and runs `import wispr -i`.
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"--config", "import", "import"}, env, tty, tty, "wispr")
	want := []string{"--config", "import", "import", "wispr", "-i"}
	if !stringSliceEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDispatchArgsDoesNotRewriteImportWithFlags(t *testing.T) {
	// `tprompt import --dry-run` must NOT be rewritten: injecting `-i` would make
	// the engine see both interactive and dry-run and error. Find leaves
	// `--dry-run` in `remaining`, a flag that is not a root persistent flag, so
	// bareImportArgs rejects it and the rewrite is skipped.
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"import", "--dry-run"}, env, tty, tty, "wispr")
	if !stringSliceEqual(got, []string{"import", "--dry-run"}) {
		t.Fatalf("import --dry-run must pass through unchanged, got %v", got)
	}
}

func TestDispatchArgsDoesNotRewriteImportWithStrayPositional(t *testing.T) {
	// `tprompt import bogus` leaves "bogus" in `remaining`, so the rewrite is
	// skipped and cobra reports the unknown source itself.
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"import", "bogus"}, env, tty, tty, "wispr")
	if !stringSliceEqual(got, []string{"import", "bogus"}) {
		t.Fatalf("import bogus must pass through unchanged, got %v", got)
	}
}

func TestDispatchArgsDoesNotRewriteBareImportWhenNotInteractiveCapable(t *testing.T) {
	// The picker needs both injected streams to be ttys (NewImportRenderer's
	// preflight). When they are not — stdout redirected (`tprompt import >out.txt`
	// in tmux) or an embedded caller injecting a non-tty stdin — forcing `-i`
	// would turn §34's "bare form prints help" into InteractiveRequiresTTYError,
	// so the rewrite is gated on interactiveTTY and bare `import` prints help.
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	stdinTTY := func() bool { return true }
	notInteractive := func() bool { return false }

	got := dispatchArgs(root, []string{"import"}, env, stdinTTY, notInteractive, "wispr")
	if !stringSliceEqual(got, []string{"import"}) {
		t.Fatalf("bare import without an interactive-capable tty must pass through unchanged, got %v", got)
	}
}

func TestDispatchArgsRewritesBareImportWithRootFlagAfterImport(t *testing.T) {
	// A root persistent flag placed AFTER `import` must still count as bare:
	// `tprompt import --config /tmp/cfg` carries only a root flag, so it dispatches
	// to the default source regardless of flag position relative to `import`.
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"import", "--config", "/tmp/cfg"}, env, tty, tty, "wispr")
	want := []string{"import", "--config", "/tmp/cfg", "wispr", "-i"}
	if !stringSliceEqual(got, want) {
		t.Fatalf("import with trailing root flag should rewrite to %v, got %v", want, got)
	}
}

func TestDispatchArgsRewritesBareImportWhenConfigValueLooksLikeHelpFlag(t *testing.T) {
	// `tprompt import --config -v` names a config path "-v" — it is NOT a version
	// request. bareImportArgs reads "-v" as --config's value, so the invocation is
	// bare and dispatches to the default source (the help/version guard is scoped
	// to the root→tui path and must not suppress this).
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"import", "--config", "-v"}, env, tty, tty, "wispr")
	want := []string{"import", "--config", "-v", "wispr", "-i"}
	if !stringSliceEqual(got, want) {
		t.Fatalf("import --config -v should rewrite to %v, got %v", want, got)
	}
}

func TestDispatchArgsDoesNotRewriteImportWithDanglingRootFlag(t *testing.T) {
	// `tprompt import --config` omits --config's required value. Rewriting would
	// append `wispr`, which cobra would bind as the missing value; instead the
	// invocation must pass through so cobra reports the missing-value error.
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"import", "--config"}, env, tty, tty, "wispr")
	if !stringSliceEqual(got, []string{"import", "--config"}) {
		t.Fatalf("import with dangling --config must pass through unchanged, got %v", got)
	}
}

func TestDispatchArgsDoesNotRewriteImportWithStrayImportPositional(t *testing.T) {
	// `tprompt import import` has "import" as a stray positional (Find leaves it in
	// `remaining` as a non-flag), so it must NOT rewrite — cobra rejects the
	// unknown source, the same as `import bogus`.
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"import", "import"}, env, tty, tty, "wispr")
	if !stringSliceEqual(got, []string{"import", "import"}) {
		t.Fatalf("import import must pass through unchanged, got %v", got)
	}
}

func TestDispatchArgsDoesNotRewriteExplicitImportSource(t *testing.T) {
	// `tprompt import wispr` names a source, so Find matches the wispr command,
	// not the import parent — it is never rewritten (no surprise -i).
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "/tmp/tmux-0/default,1,0" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"import", "wispr"}, env, tty, tty, "wispr")
	if !stringSliceEqual(got, []string{"import", "wispr"}) {
		t.Fatalf("explicit `import wispr` must pass through unchanged, got %v", got)
	}
}

func TestDispatchArgsSkipsBareImportOutsideTmux(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(string) string { return "" }
	tty := func() bool { return true }

	got := dispatchArgs(root, []string{"import"}, env, tty, tty, "wispr")
	if !stringSliceEqual(got, []string{"import"}) {
		t.Fatalf("bare import outside tmux must print help (unchanged), got %v", got)
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
