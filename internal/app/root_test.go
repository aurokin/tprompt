package app

import (
	"errors"
	"io"
	"testing"
)

func TestRootCmdRegistersAllSubcommands(t *testing.T) {
	root := NewRootCmd()
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
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"daemon", "start"})
	if err != nil {
		t.Fatalf("find daemon start: %v", err)
	}
	if cmd == nil || cmd.CommandPath() != "tprompt daemon start" {
		t.Fatalf("want tprompt daemon start, got %v", cmd)
	}
}

func TestDaemonRunAliasExists(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"daemon", "run"})
	if err != nil {
		t.Fatalf("find daemon run alias: %v", err)
	}
	if cmd == nil || cmd.CommandPath() != "tprompt daemon start" {
		t.Fatalf("want run to resolve to tprompt daemon start, got %v", cmd)
	}
}

func TestRootDispatchRoutesToTUIInTmuxTTY(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-0/default,1,0")
	withStdinTTY(t, true)

	root := NewRootCmd()
	err := root.RunE(root, nil)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("dispatch should hit tui stub (ErrNotImplemented), got %v", err)
	}
}

func TestRootDispatchFallsBackToHelpWithoutTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	withStdinTTY(t, true)

	root := NewRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.RunE(root, nil); err != nil {
		t.Fatalf("want nil from Help path, got %v", err)
	}
}

func TestRootDispatchFallsBackToHelpWithoutTTY(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-0/default,1,0")
	withStdinTTY(t, false)

	root := NewRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.RunE(root, nil); err != nil {
		t.Fatalf("want nil from Help path, got %v", err)
	}
}

func withStdinTTY(t *testing.T, isTTY bool) {
	t.Helper()
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return isTTY }
	t.Cleanup(func() { stdinIsTTY = orig })
}
