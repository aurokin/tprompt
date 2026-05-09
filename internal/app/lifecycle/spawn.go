package lifecycle

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// ProductionSpawner detaches a child via setsid, sends stdout to /dev/null,
// and appends stderr to logPath so daemon-side panics or pre-listen
// failures still leave evidence.
type ProductionSpawner struct{}

// Spawn implements Spawner. exec is the absolute path of the daemon
// executable. args is the argv (without exec[0]) — typically
// `daemon run` plus optional `--config <path>` per buildSpawnArgs. The
// returned SpawnHandle carries the child's PID so the launcher can
// detect early child exit via kill(pid, 0) while polling Status.
func (ProductionSpawner) Spawn(ctx context.Context, execPath string, args []string, logPath string) (SpawnHandle, error) {
	if err := ctx.Err(); err != nil {
		return SpawnHandle{}, err
	}
	if execPath == "" {
		return SpawnHandle{}, fmt.Errorf("lifecycle spawner: empty executable path")
	}
	// Deliberately exec.Command, NOT exec.CommandContext: the child
	// daemon is detached via setsid and is meant to outlive this call.
	// CommandContext installs a goroutine that SIGKILLs the child when
	// ctx is done, so a caller-side cancellation (Ctrl-C during the
	// readiness poll, a cobra command deadline, etc.) would tear down
	// a healthy daemon we just spawned and Release()'d. ctx is still
	// honored above for pre-fork cancellation.
	cmd := exec.Command(execPath, args...)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	if logPath != "" {
		// On a fresh install the daemon log's parent dir may not exist
		// yet — daemon.NewLogger normally creates it inside the child,
		// but here we open the file in the parent before fork-exec, so
		// we have to mkdir-p ourselves at the same 0700 mode the daemon
		// uses. Without this, first-run spawns fail with ENOENT before
		// the child has any chance to set up its log dir.
		if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
			return SpawnHandle{}, fmt.Errorf("lifecycle spawner: mkdir daemon log dir: %w", err)
		}
		// G304: logPath is config-derived and resolved through the
		// caller's config pipeline; identical to the file the daemon
		// writes structured logs to. Opening it append-only with 0o600
		// is the documented behavior.
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec
		if err != nil {
			return SpawnHandle{}, fmt.Errorf("lifecycle spawner: open daemon log %s: %w", logPath, err)
		}
		cmd.Stderr = f
		// Closing the parent fd after Start is safe — the child has its
		// own duplicated descriptor pointing at the same file.
		defer func() { _ = f.Close() }()
	} else {
		cmd.Stderr = io.Discard
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return SpawnHandle{}, err
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return SpawnHandle{PID: pid}, fmt.Errorf("lifecycle spawner: release child: %w", err)
	}
	return SpawnHandle{PID: pid}, nil
}
