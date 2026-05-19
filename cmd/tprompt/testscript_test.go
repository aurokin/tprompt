package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hsadler/tprompt/internal/app"
	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"tprompt": func() {
			os.Exit(app.RunCLI(os.Args[1:], os.Stdout, os.Stderr, os.Stdin))
		},
	})
}

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(env *testscript.Env) error {
			// macOS limits unix-socket paths to ~103 bytes, and testscript's
			// $WORK paths (under /var/folders/...) exceed that. Expose a short
			// per-test tmpdir that tests needing a unix socket (e.g. tmux) can
			// use as TMUX_TMPDIR without colliding across parallel runs.
			d, err := os.MkdirTemp("/tmp", "tp.")
			if err != nil {
				return err
			}
			env.Vars = append(env.Vars, "SHORT_TMPDIR="+d)
			env.Defer(func() {
				// Belt-and-suspenders cleanup: if a script aborts before its
				// own `exec tmux kill-server`, kill any tmux server started on
				// this test's socket. Use -S with the explicit socket path so
				// tmux's socket resolution cannot fall back to the host's
				// server via $TMUX, $TMUX_TMPDIR, $TMPDIR, or any other env
				// var. Errors (including "no server running") are ignored —
				// this is best-effort cleanup, not a test assertion.
				sock := filepath.Join(d, fmt.Sprintf("tmux-%d", os.Getuid()), "default")
				kill := exec.Command("tmux", "-S", sock, "kill-server")
				_ = kill.Run()
				_ = os.RemoveAll(d)
			})
			return nil
		},
	})
}
