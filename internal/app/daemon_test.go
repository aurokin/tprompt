package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/testutil"
	"github.com/hsadler/tprompt/internal/tmux"
)

type fakeDaemonClient struct {
	submitFn func(daemon.SubmitRequest) (daemon.SubmitResponse, error)
	statusFn func() (daemon.StatusResponse, error)
	stopFn   func() (daemon.StopResponse, error)
}

func (f *fakeDaemonClient) Submit(req daemon.SubmitRequest) (daemon.SubmitResponse, error) {
	return f.submitFn(req)
}

func (f *fakeDaemonClient) Status() (daemon.StatusResponse, error) {
	if f.statusFn == nil {
		return daemon.StatusResponse{}, nil
	}
	return f.statusFn()
}

func (f *fakeDaemonClient) Stop() (daemon.StopResponse, error) {
	if f.stopFn == nil {
		return daemon.StopResponse{Accepted: true}, nil
	}
	return f.stopFn()
}

func daemonDeps(t *testing.T, client daemon.Client) Deps {
	t.Helper()
	deps := workingDeps(t, &fakeStore{})
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			PromptsDir: "/prompts",
			SocketPath: "/tmp/tprompt-test.sock",
			LogPath:    "/tmp/tprompt-test.log",
		}, nil
	}
	deps.LoadDaemonConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			SocketPath:    "/tmp/tprompt-test.sock",
			LogPath:       "/tmp/tprompt-test.log",
			MaxPasteBytes: 1 << 20,
		}, nil
	}
	deps.NewStore = func(config.Resolved) (store.Store, error) {
		return &fakeStore{}, nil
	}
	deps.NewTmux = func() (tmux.Adapter, error) {
		return nil, ErrNotImplemented
	}
	deps.NewClip = func(config.Resolved) (clipboard.Reader, error) {
		return nil, ErrNotImplemented
	}
	deps.NewDaemonClient = func(config.Resolved) (daemon.Client, error) {
		return client, nil
	}
	return deps
}

func TestDaemonStatusPrintsFields(t *testing.T) {
	client := &fakeDaemonClient{
		statusFn: func() (daemon.StatusResponse, error) {
			return daemon.StatusResponse{
				PID:         12345,
				Socket:      "/tmp/x.sock",
				LogPath:     "/tmp/x.log",
				UptimeSec:   42,
				PendingJobs: 3,
				Version:     "0.1.0",
			}, nil
		},
	}
	stdout, _, err := executeRootWith(t, daemonDeps(t, client), "daemon", "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := strings.Join([]string{
		"tprompt daemon",
		"  pid:          12345",
		"  socket:       /tmp/x.sock",
		"  log:          /tmp/x.log",
		"  uptime:       42s",
		"  version:      0.1.0",
		"  pending jobs: 3",
		"",
	}, "\n")
	if stdout != want {
		t.Fatalf("daemon status output mismatch\ngot:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name    string
		seconds int64
		want    string
	}{
		{name: "zero", seconds: 0, want: "0s"},
		{name: "seconds", seconds: 42, want: "42s"},
		{name: "minutes", seconds: 125, want: "2m5s"},
		{name: "hours", seconds: 3661, want: "1h1m1s"},
		{name: "negative", seconds: -1, want: "0s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatUptime(tc.seconds); got != tc.want {
				t.Fatalf("formatUptime(%d) = %q, want %q", tc.seconds, got, tc.want)
			}
		})
	}
}

func TestDaemonStatusSocketUnavailableMapsToExitDaemon(t *testing.T) {
	client := &fakeDaemonClient{
		statusFn: func() (daemon.StatusResponse, error) {
			return daemon.StatusResponse{}, &daemon.SocketUnavailableError{
				Path:   "/tmp/x.sock",
				Reason: "connect: connection refused",
			}
		},
	}
	_, _, err := executeRootWith(t, daemonDeps(t, client), "daemon", "status")
	var sue *daemon.SocketUnavailableError
	if !errors.As(err, &sue) {
		t.Fatalf("want SocketUnavailableError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitDaemon {
		t.Fatalf("ExitCode = %d, want %d", ExitCode(err), ExitDaemon)
	}
}

func TestDaemonStatusDoesNotAutoStart(t *testing.T) {
	client := &fakeDaemonClient{
		statusFn: func() (daemon.StatusResponse, error) {
			return daemon.StatusResponse{}, &daemon.SocketUnavailableError{
				Path:   "/tmp/x.sock",
				Reason: "no such file",
			}
		},
	}
	deps := daemonDeps(t, client)
	deps.LoadDaemonConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			SocketPath:      "/tmp/tprompt-test.sock",
			LogPath:         "/tmp/tprompt-test.log",
			MaxPasteBytes:   1 << 20,
			DaemonAutoStart: true,
		}, nil
	}
	deps.StartDaemon = func(config.Resolved, string) error {
		t.Fatal("daemon status must not auto-start")
		return nil
	}

	_, _, err := executeRootWith(t, deps, "daemon", "status")
	var su *daemon.SocketUnavailableError
	if !errors.As(err, &su) {
		t.Fatalf("want SocketUnavailableError, got %T: %v", err, err)
	}
}

func TestDaemonStatusLoadConfigError(t *testing.T) {
	client := &fakeDaemonClient{
		statusFn: func() (daemon.StatusResponse, error) {
			t.Fatal("Status should not be called when config fails")
			return daemon.StatusResponse{}, nil
		},
	}
	deps := daemonDeps(t, client)
	deps.LoadDaemonConfig = func(string) (config.Resolved, error) {
		return config.Resolved{}, &config.ValidationError{Field: "socket_path", Message: "must be set"}
	}
	_, _, err := executeRootWith(t, deps, "daemon", "status")
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %T: %v", err, err)
	}
}

func TestDaemonStatusIgnoresPromptConfigValidation(t *testing.T) {
	client := &fakeDaemonClient{
		statusFn: func() (daemon.StatusResponse, error) {
			return daemon.StatusResponse{Socket: "/tmp/x.sock"}, nil
		},
	}
	deps := daemonDeps(t, client)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		t.Fatal("LoadConfig should not be called by daemon status")
		return config.Resolved{}, nil
	}
	deps.LoadDaemonConfig = func(string) (config.Resolved, error) {
		return config.Resolved{SocketPath: "/tmp/tprompt-test.sock"}, nil
	}

	if _, _, err := executeRootWith(t, deps, "daemon", "status"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonStatusRejectsEmptySocketPath(t *testing.T) {
	client := &fakeDaemonClient{
		statusFn: func() (daemon.StatusResponse, error) {
			t.Fatal("Status should not be called when socket_path is invalid")
			return daemon.StatusResponse{}, nil
		},
	}
	deps := daemonDeps(t, client)
	deps.LoadDaemonConfig = func(string) (config.Resolved, error) {
		return config.Resolved{}, nil
	}

	_, _, err := executeRootWith(t, deps, "daemon", "status")
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "socket_path" {
		t.Fatalf("Field = %q, want %q", ve.Field, "socket_path")
	}
}

func TestDaemonStopPrintsStoppedAfterSocketDisappears(t *testing.T) {
	statusCalls := 0
	client := &fakeDaemonClient{
		stopFn: func() (daemon.StopResponse, error) {
			return daemon.StopResponse{Accepted: true}, nil
		},
		statusFn: func() (daemon.StatusResponse, error) {
			statusCalls++
			if statusCalls == 1 {
				return daemon.StatusResponse{Socket: "/tmp/x.sock"}, nil
			}
			return daemon.StatusResponse{}, &daemon.SocketUnavailableError{Path: "/tmp/x.sock", Reason: "connection refused"}
		},
	}

	deps := daemonDeps(t, client)
	stdout, _, err := executeRootWith(t, deps, "daemon", "stop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "tprompt daemon stopped") {
		t.Fatalf("stdout = %q, want stopped message", stdout)
	}
}

func TestDaemonStopNoDaemonRunningPrintsClearMessage(t *testing.T) {
	client := &fakeDaemonClient{
		stopFn: func() (daemon.StopResponse, error) {
			return daemon.StopResponse{}, &daemon.SocketUnavailableError{Path: "/tmp/x.sock", Reason: "no such file"}
		},
		statusFn: func() (daemon.StatusResponse, error) {
			t.Fatal("Status should not be called when daemon is not running")
			return daemon.StatusResponse{}, nil
		},
	}

	deps := daemonDeps(t, client)
	stdout, _, err := executeRootWith(t, deps, "daemon", "stop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "daemon not running") {
		t.Fatalf("stdout = %q, want not-running message", stdout)
	}
}

func TestDaemonStopTimeoutMapsToExitDaemon(t *testing.T) {
	client := &fakeDaemonClient{
		stopFn: func() (daemon.StopResponse, error) {
			return daemon.StopResponse{Accepted: true}, nil
		},
		statusFn: func() (daemon.StatusResponse, error) {
			return daemon.StatusResponse{Socket: "/tmp/x.sock"}, nil
		},
	}

	err := runDaemonStop(daemonDeps(t, client), time.Millisecond)
	var timeoutErr *daemon.ShutdownTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("want ShutdownTimeoutError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitDaemon {
		t.Fatalf("ExitCode = %d, want %d", ExitCode(err), ExitDaemon)
	}
}

type stubTmuxAdapter struct{}

func (stubTmuxAdapter) CurrentContext() (tmux.TargetContext, error) {
	return tmux.TargetContext{}, nil
}

func (stubTmuxAdapter) PaneExists(context.Context, string) (bool, error) { return true, nil }

func (stubTmuxAdapter) IsTargetSelected(context.Context, tmux.TargetContext) (bool, error) {
	return true, nil
}

func (stubTmuxAdapter) CapturePaneTail(string, int) (string, error) { return "", nil }

func (stubTmuxAdapter) Paste(context.Context, tmux.TargetContext, string, bool) error { return nil }

func (stubTmuxAdapter) Type(context.Context, tmux.TargetContext, string, bool) error { return nil }

func (stubTmuxAdapter) DisplayMessage(tmux.MessageTarget, string) error { return nil }

func TestDaemonStartIgnoresPromptConfigValidation(t *testing.T) {
	deps := daemonDeps(t, &fakeDaemonClient{})
	deps.LoadConfig = func(string) (config.Resolved, error) {
		t.Fatal("LoadConfig should not be called by daemon start")
		return config.Resolved{}, nil
	}

	dir := testutil.ShortTempDir(t)
	socketPath := filepath.Join(dir, "daemon.sock")
	logPath := filepath.Join(dir, "daemon.log")
	deps.LoadDaemonConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			SocketPath:    socketPath,
			LogPath:       logPath,
			MaxPasteBytes: 1 << 20,
		}, nil
	}
	deps.NewTmux = func() (tmux.Adapter, error) {
		return stubTmuxAdapter{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runDaemonStart(ctx, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonStartRejectsEmptyLogPath(t *testing.T) {
	deps := daemonDeps(t, &fakeDaemonClient{})
	deps.LoadDaemonConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			SocketPath:    "/tmp/tprompt-test.sock",
			MaxPasteBytes: 1 << 20,
		}, nil
	}
	deps.NewTmux = func() (tmux.Adapter, error) {
		t.Fatal("NewTmux should not be called when config is invalid")
		return nil, nil
	}

	err := runDaemonStart(context.Background(), deps)
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "log_path" {
		t.Fatalf("Field = %q, want %q", ve.Field, "log_path")
	}
}

func TestDaemonStartSkipsStoppedLogWhenRunReturnsError(t *testing.T) {
	deps := daemonDeps(t, &fakeDaemonClient{})
	dir := testutil.ShortTempDir(t)
	socketPath := filepath.Join(dir, "daemon.sock")
	logPath := filepath.Join(dir, "daemon.log")
	deps.LoadDaemonConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			SocketPath:    socketPath,
			LogPath:       logPath,
			MaxPasteBytes: 1 << 20,
		}, nil
	}
	deps.NewTmux = func() (tmux.Adapter, error) {
		return stubTmuxAdapter{}, nil
	}

	prevRunDaemon := runDaemon
	runDaemon = func(_ context.Context, _ *daemon.Server, onReady func()) (daemon.RunResult, error) {
		onReady()
		return daemon.RunResult{Started: true, ExitReason: daemon.RunExitServeError}, errors.New("boom")
	}
	t.Cleanup(func() { runDaemon = prevRunDaemon })

	err := runDaemonStart(context.Background(), deps)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("runDaemonStart error = %v, want boom", err)
	}

	logged, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("ReadFile(logPath): %v", readErr)
	}
	logText := string(logged)
	if !strings.Contains(logText, "outcome=started") {
		t.Fatalf("expected started log entry, got %q", logText)
	}
	if strings.Contains(logText, "outcome=stopped") {
		t.Fatalf("unexpected stopped log entry on run error, got %q", logText)
	}
}

func TestDaemonStartLogsStoppedOnCleanShutdown(t *testing.T) {
	deps := daemonDeps(t, &fakeDaemonClient{})
	dir := testutil.ShortTempDir(t)
	socketPath := filepath.Join(dir, "daemon.sock")
	logPath := filepath.Join(dir, "daemon.log")
	deps.LoadDaemonConfig = func(string) (config.Resolved, error) {
		return config.Resolved{
			SocketPath:    socketPath,
			LogPath:       logPath,
			MaxPasteBytes: 1 << 20,
		}, nil
	}
	deps.NewTmux = func() (tmux.Adapter, error) {
		return stubTmuxAdapter{}, nil
	}

	prevRunDaemon := runDaemon
	runDaemon = func(_ context.Context, _ *daemon.Server, onReady func()) (daemon.RunResult, error) {
		onReady()
		return daemon.RunResult{Started: true, ExitReason: daemon.RunExitContextCanceled}, nil
	}
	t.Cleanup(func() { runDaemon = prevRunDaemon })

	if err := runDaemonStart(context.Background(), deps); err != nil {
		t.Fatalf("runDaemonStart: %v", err)
	}

	logged, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("ReadFile(logPath): %v", readErr)
	}
	logText := string(logged)
	if !strings.Contains(logText, "outcome=started") {
		t.Fatalf("expected started log entry, got %q", logText)
	}
	if !strings.Contains(logText, "outcome=stopped") {
		t.Fatalf("expected stopped log entry, got %q", logText)
	}
}
