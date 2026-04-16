package app

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
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

func TestRootDispatchRoutesToTUIInTmuxTTY(t *testing.T) {
	withStdinTTY(t, true)

	deps := fakeDeps(t)
	deps.Env = func(key string) string {
		if key == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}

	root := NewRootCmd(deps)
	err := root.RunE(root, nil)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("dispatch should hit tui stub (ErrNotImplemented), got %v", err)
	}
}

func TestRootDispatchFallsBackToHelpWithoutTmux(t *testing.T) {
	withStdinTTY(t, true)

	deps := fakeDeps(t)
	deps.Env = func(string) string { return "" }

	root := NewRootCmd(deps)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.RunE(root, nil); err != nil {
		t.Fatalf("want nil from Help path, got %v", err)
	}
}

func TestRootDispatchFallsBackToHelpWithoutTTY(t *testing.T) {
	withStdinTTY(t, false)

	deps := fakeDeps(t)
	deps.Env = func(key string) string {
		if key == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}

	root := NewRootCmd(deps)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.RunE(root, nil); err != nil {
		t.Fatalf("want nil from Help path, got %v", err)
	}
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
		NewStore: func(config.Resolved) (store.Store, error) {
			return nil, ErrNotImplemented
		},
		NewTmux: func() (tmux.Adapter, error) {
			return nil, ErrNotImplemented
		},
		NewClip: func(config.Resolved) (clipboard.Reader, error) {
			return nil, ErrNotImplemented
		},
	}
}
