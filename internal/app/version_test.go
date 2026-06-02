package app

import "testing"

// The release pipeline injects the version via
// -ldflags "-X github.com/hsadler/tprompt/internal/app.appVersion=<v>"; that
// only takes effect if the variable exists and the root command surfaces it.
// Guard both so a future refactor can't silently re-break the dead-ldflag bug.
func TestRootCmdExposesVersion(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))

	if root.Version == "" {
		t.Fatal("root.Version is empty; `tprompt --version` would not be wired")
	}
	if root.Version != appVersion {
		t.Errorf("root.Version = %q, want appVersion %q", root.Version, appVersion)
	}
	// cobra registers the --version flag lazily at Execute time; trigger that
	// init directly so the test confirms a non-empty Version yields the flag.
	root.InitDefaultVersionFlag()
	if root.Flags().Lookup("version") == nil {
		t.Error("cobra --version flag not registered on root")
	}
}

// A bare `tprompt --version` (or `-v`) inside tmux+tty must reach the root
// command, not be rewritten to `tui` by the default-subcommand dispatch.
func TestDispatchArgsPreservesVersionFlags(t *testing.T) {
	root := NewRootCmd(fakeDeps(t))
	env := func(key string) string {
		if key == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}
	tty := func() bool { return true }

	for _, flag := range []string{"--version", "-v"} {
		got := dispatchArgs(root, []string{flag}, env, tty)
		if len(got) != 1 || got[0] != flag {
			t.Errorf("dispatchArgs([%s]) = %v, want [%s] unchanged", flag, got, flag)
		}
	}
}
