package app

import (
	"bytes"
	"io"
	"testing"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/picker"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/submitter"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
)

func TestRootCmdRegistersAllSubcommands(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	want := []string{"list", "show", "send", "paste", "doctor", "tui", "pick", "daemon"}
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

func TestDaemonStartCommandExists(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	cmd, _, err := root.Find([]string{"daemon", "start"})
	if err != nil {
		t.Fatalf("find daemon start: %v", err)
	}
	if cmd == nil || cmd.CommandPath() != "tprompt daemon start" {
		t.Fatalf("want tprompt daemon start, got %v", cmd)
	}
}

func TestDaemonRunAliasExists(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	cmd, _, err := root.Find([]string{"daemon", "run"})
	if err != nil {
		t.Fatalf("find daemon run alias: %v", err)
	}
	if cmd == nil || cmd.CommandPath() != "tprompt daemon start" {
		t.Fatalf("want run to resolve to tprompt daemon start, got %v", cmd)
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
