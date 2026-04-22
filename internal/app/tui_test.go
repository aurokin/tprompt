package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/submitter"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
)

type recordingRenderer struct {
	state  tui.State
	called bool
	result tui.Result
	err    error
}

func (r *recordingRenderer) Run(s tui.State) (tui.Result, error) {
	r.called = true
	r.state = s
	return r.result, r.err
}

func tuiDeps(t *testing.T, fs *fakeStore, rend tui.Renderer, cfgOverride ...func(*config.Resolved)) Deps {
	t.Helper()
	deps := workingDeps(t, fs)
	deps.LoadConfig = func(string) (config.Resolved, error) {
		cfg := config.Resolved{
			PromptsDir: "/prompts",
			ReservedPrintable: map[rune]string{
				'p': "clipboard",
				'/': "search",
			},
		}
		for _, fn := range cfgOverride {
			fn(&cfg)
		}
		return cfg, nil
	}
	deps.NewDaemonClient = func(config.Resolved) (daemon.Client, error) {
		return &fakeDaemonClient{
			statusFn: func() (daemon.StatusResponse, error) { return daemon.StatusResponse{}, nil },
		}, nil
	}
	deps.NewTmux = func() (tmux.Adapter, error) {
		return &fakeAdapter{paneExists: true}, nil
	}
	deps.NewRenderer = func(config.Resolved) (tui.Renderer, error) {
		return rend, nil
	}
	return deps
}

func TestTUI_MissingTargetPaneExitsUsage(t *testing.T) {
	fs := &fakeStore{}
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, fs, rend)

	_, _, err := executeRootWith(t, deps, "tui")
	if err == nil {
		t.Fatal("want error for missing --target-pane")
	}
	if !strings.Contains(err.Error(), "target-pane") {
		t.Fatalf("want required-flag error mentioning target-pane, got %v", err)
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("want ExitUsage, got %d", ExitCode(err))
	}
	if rend.called {
		t.Fatal("renderer must not run when required flag is missing")
	}
}

func TestTUI_CancelStubExitsZero(t *testing.T) {
	fs := &fakeStore{}
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, fs, rend)

	stdout, stderr, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if stdout != "" {
		t.Fatalf("want silent stdout, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("want silent stderr, got %q", stderr)
	}
	if !rend.called {
		t.Fatal("renderer should have been called")
	}
}

func TestTUI_BuildsStateFromStore(t *testing.T) {
	fs := &fakeStore{
		summaries: []store.Summary{
			{ID: "alpha", Title: "Alpha", Key: "1"},
			{ID: "beta", Description: "Second", Key: "2"},
			{ID: "overflow-one"}, // no Key → overflow
		},
	}
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, fs, rend)

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	// Clipboard row pinned first, then board rows from summaries with a Key.
	if len(rend.state.Rows) != 3 {
		t.Fatalf("want 3 rows (clipboard + 2 board), got %d", len(rend.state.Rows))
	}
	if rend.state.Rows[0].Key != 'p' || rend.state.Rows[0].PromptID != "" {
		t.Fatalf("row[0] should be pinned clipboard with key 'p', got %+v", rend.state.Rows[0])
	}
	if rend.state.Rows[1].PromptID != "alpha" || rend.state.Rows[1].Key != '1' {
		t.Fatalf("row[1] = %+v, want alpha/1", rend.state.Rows[1])
	}
	if rend.state.Rows[2].PromptID != "beta" || rend.state.Rows[2].Key != '2' {
		t.Fatalf("row[2] = %+v, want beta/2", rend.state.Rows[2])
	}

	if len(rend.state.Overflow) != 1 || rend.state.Overflow[0].PromptID != "overflow-one" {
		t.Fatalf("overflow mismatch: %+v", rend.state.Overflow)
	}

	if role := rend.state.Reserved['p']; role != "clipboard" {
		t.Fatalf("reserved map should include clipboard, got %q", role)
	}
}

func TestTUI_StateOmitsClipboardRowWhenKeyDisabled(t *testing.T) {
	fs := &fakeStore{
		summaries: []store.Summary{
			{ID: "alpha", Key: "1"},
		},
	}
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, fs, rend, func(c *config.Resolved) {
		c.ReservedPrintable = map[rune]string{'/': "search"} // no clipboard role
	})

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(rend.state.Rows) != 1 || rend.state.Rows[0].PromptID != "alpha" {
		t.Fatalf("want single board row alpha, got %+v", rend.state.Rows)
	}
}

func TestTUI_LoadConfigErrorPropagates(t *testing.T) {
	fs := &fakeStore{}
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, fs, rend)
	cfgErr := &config.ValidationError{Field: "prompts_dir", Message: "must be set"}
	deps.LoadConfig = func(string) (config.Resolved, error) {
		return config.Resolved{}, cfgErr
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want config.ValidationError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("want ExitUsage, got %d", ExitCode(err))
	}
	if rend.called {
		t.Fatal("renderer must not run when config load fails")
	}
}

func TestTUI_RendererErrorPropagates(t *testing.T) {
	fs := &fakeStore{}
	rendErr := errors.New("renderer boom")
	rend := &recordingRenderer{err: rendErr}
	deps := tuiDeps(t, fs, rend)

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	if !errors.Is(err, rendErr) {
		t.Fatalf("want renderer error, got %v", err)
	}
}

func TestTUI_BareDispatchInTmuxTTYHitsRequiredFlagCheck(t *testing.T) {
	// Mirrors what RunCLI does: dispatchArgs rewrites bare args to [tui] when
	// in tmux+tty; cobra then errors on --target-pane before the renderer runs.
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, &fakeStore{}, rend)
	deps.Env = func(k string) string {
		if k == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}
	withStdinTTY(t, true)

	root := NewRootCmd(deps)
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)
	root.SetArgs(dispatchArgs(root, nil, deps.Env, stdinIsTTY))

	err := root.Execute()
	if err == nil {
		t.Fatal("want required-flag error from bare dispatch, got nil")
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("want ExitUsage, got %d (err=%v)", ExitCode(err), err)
	}
	if rend.called {
		t.Fatal("renderer must not run when required flag is missing")
	}
}

func TestTUI_BareDispatchWithFlagInTmuxTTYSucceeds(t *testing.T) {
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, &fakeStore{}, rend)
	deps.Env = func(k string) string {
		if k == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}
	withStdinTTY(t, true)

	root := NewRootCmd(deps)
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)
	args := []string{"--target-pane", "%0"}
	root.SetArgs(dispatchArgs(root, args, deps.Env, stdinIsTTY))

	if err := root.Execute(); err != nil {
		t.Fatalf("bare --target-pane invocation should succeed via cancel stub, got %v", err)
	}
	if !rend.called {
		t.Fatal("renderer should have been called")
	}
}

func TestTUI_BuildTargetThreadsFlags(t *testing.T) {
	target := buildTUITarget(tuiFlags{
		targetPane: "%9",
		clientTTY:  "/dev/pts/2",
		sessionID:  "$3",
	})
	if target.PaneID != "%9" || target.ClientTTY != "/dev/pts/2" || target.Session != "$3" {
		t.Fatalf("TargetContext = %+v", target)
	}
}

func TestTUI_StoreErrorShortCircuits(t *testing.T) {
	storeErr := &store.DuplicatePromptIDError{ID: "dup", Paths: []string{"/a.md", "/b.md"}}
	fs := &fakeStore{discoverErr: storeErr}
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, fs, rend)
	daemonCalled := false
	deps.NewDaemonClient = func(config.Resolved) (daemon.Client, error) {
		daemonCalled = true
		return nil, nil
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	var dup *store.DuplicatePromptIDError
	if !errors.As(err, &dup) {
		t.Fatalf("want DuplicatePromptIDError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
	if daemonCalled {
		t.Fatal("daemon factory must not be called when store load fails")
	}
	if rend.called {
		t.Fatal("renderer must not run when store load fails")
	}
}

func TestTUI_DaemonUnreachableExitsDaemon(t *testing.T) {
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, &fakeStore{}, rend)
	tmuxCalled := false
	deps.NewDaemonClient = func(config.Resolved) (daemon.Client, error) {
		return &fakeDaemonClient{
			statusFn: func() (daemon.StatusResponse, error) {
				return daemon.StatusResponse{}, &daemon.SocketUnavailableError{Path: "/missing.sock", Reason: "connection refused"}
			},
		}, nil
	}
	deps.NewTmux = func() (tmux.Adapter, error) {
		tmuxCalled = true
		return nil, nil
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	var su *daemon.SocketUnavailableError
	if !errors.As(err, &su) {
		t.Fatalf("want SocketUnavailableError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitDaemon {
		t.Fatalf("want ExitDaemon, got %d", ExitCode(err))
	}
	if tmuxCalled {
		t.Fatal("tmux factory must not be called when daemon preflight fails")
	}
	if rend.called {
		t.Fatal("renderer must not run when daemon preflight fails")
	}
}

func TestTUI_PaneMissingExitsTmux(t *testing.T) {
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, &fakeStore{}, rend)
	deps.NewTmux = func() (tmux.Adapter, error) {
		return &fakeAdapter{paneExists: false}, nil
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%42")
	var pm *tmux.PaneMissingError
	if !errors.As(err, &pm) {
		t.Fatalf("want PaneMissingError, got %T: %v", err, err)
	}
	if pm.PaneID != "%42" {
		t.Fatalf("PaneID = %q, want %%42", pm.PaneID)
	}
	if ExitCode(err) != ExitTmux {
		t.Fatalf("want ExitTmux, got %d", ExitCode(err))
	}
	if rend.called {
		t.Fatal("renderer must not run when pane preflight fails")
	}
}

// recordingSubmitter captures the Submit call so tests can assert on the
// Result that runTUI threaded through.
type recordingSubmitter struct {
	called  bool
	result  tui.Result
	err     error
	cfg     config.Resolved
	target  tmux.TargetContext
	prompts store.Store
	client  daemon.Client
}

func (r *recordingSubmitter) Submit(result tui.Result) error {
	r.called = true
	r.result = result
	return r.err
}

func TestTUI_PromptSelectionInvokesSubmitterWithDeps(t *testing.T) {
	fs := &fakeStore{
		summaries: []store.Summary{{ID: "demo", Key: "1"}},
		prompts:   map[string]store.Prompt{"demo": {Summary: store.Summary{ID: "demo"}, Body: "x"}},
	}
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionPrompt, PromptID: "demo"}}
	rec := &recordingSubmitter{}
	deps := tuiDeps(t, fs, rend)
	deps.NewSubmitter = func(cfg config.Resolved, prompts store.Store, client daemon.Client, target tmux.TargetContext) submitter.Submitter {
		rec.cfg = cfg
		rec.prompts = prompts
		rec.client = client
		rec.target = target
		return rec
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%9", "--client-tty", "/dev/pts/4", "--session-id", "$2")
	if err != nil {
		t.Fatalf("Submit happy path: want nil, got %v", err)
	}
	if !rec.called {
		t.Fatal("submitter should have been called for ActionPrompt")
	}
	if rec.result.Action != tui.ActionPrompt || rec.result.PromptID != "demo" {
		t.Errorf("Submit got %+v, want ActionPrompt/demo", rec.result)
	}
	if rec.target.PaneID != "%9" || rec.target.ClientTTY != "/dev/pts/4" || rec.target.Session != "$2" {
		t.Errorf("target threaded into submitter = %+v", rec.target)
	}
	if rec.prompts != fs {
		t.Error("store not threaded into submitter")
	}
	if rec.client == nil {
		t.Error("daemon client not threaded into submitter")
	}
}

func TestTUI_OversizePromptExitsExitPrompt(t *testing.T) {
	fs := &fakeStore{
		summaries: []store.Summary{{ID: "demo", Key: "1"}},
	}
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionPrompt, PromptID: "demo"}}
	deps := tuiDeps(t, fs, rend)
	deps.NewSubmitter = func(config.Resolved, store.Store, daemon.Client, tmux.TargetContext) submitter.Submitter {
		return &recordingSubmitter{err: &submitter.BodyTooLargeError{Bytes: 99, Limit: 10}}
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	var tooLarge *submitter.BodyTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("want *BodyTooLargeError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("ExitCode = %d, want ExitPrompt", ExitCode(err))
	}
}

func TestTUI_DaemonSubmitFailureExitsExitDaemon(t *testing.T) {
	fs := &fakeStore{
		summaries: []store.Summary{{ID: "demo", Key: "1"}},
	}
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionPrompt, PromptID: "demo"}}
	deps := tuiDeps(t, fs, rend)
	dialErr := &daemon.SocketUnavailableError{Path: "/tmp/x.sock", Reason: "broken pipe mid-submit"}
	deps.NewSubmitter = func(config.Resolved, store.Store, daemon.Client, tmux.TargetContext) submitter.Submitter {
		return &recordingSubmitter{err: dialErr}
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	if !errors.Is(err, dialErr) {
		t.Fatalf("want SocketUnavailableError, got %v", err)
	}
	if ExitCode(err) != ExitDaemon {
		t.Fatalf("ExitCode = %d, want ExitDaemon", ExitCode(err))
	}
}

func TestTUI_ClipboardSelectionInvokesSubmitterWithDeps(t *testing.T) {
	fs := &fakeStore{} // clipboard path must never touch the store
	rend := &recordingRenderer{result: tui.Result{
		Action:        tui.ActionClipboard,
		ClipboardBody: []byte("clip body"),
	}}
	rec := &recordingSubmitter{}
	deps := tuiDeps(t, fs, rend)
	deps.NewSubmitter = func(cfg config.Resolved, prompts store.Store, client daemon.Client, target tmux.TargetContext) submitter.Submitter {
		rec.cfg = cfg
		rec.prompts = prompts
		rec.client = client
		rec.target = target
		return rec
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%9", "--client-tty", "/dev/pts/4", "--session-id", "$2")
	if err != nil {
		t.Fatalf("clipboard happy path: want nil, got %v", err)
	}
	if !rec.called {
		t.Fatal("submitter should have been called for ActionClipboard")
	}
	if rec.result.Action != tui.ActionClipboard {
		t.Errorf("Action = %q, want ActionClipboard", rec.result.Action)
	}
	if string(rec.result.ClipboardBody) != "clip body" {
		t.Errorf("ClipboardBody = %q, want %q", rec.result.ClipboardBody, "clip body")
	}
	if rec.target.PaneID != "%9" || rec.target.ClientTTY != "/dev/pts/4" || rec.target.Session != "$2" {
		t.Errorf("target threaded into submitter = %+v", rec.target)
	}
	if rec.prompts != fs {
		t.Error("store not threaded into submitter")
	}
	if rec.client == nil {
		t.Error("daemon client not threaded into submitter")
	}
}

func TestTUI_ClipboardOversizeExitsExitPrompt(t *testing.T) {
	rend := &recordingRenderer{result: tui.Result{
		Action:        tui.ActionClipboard,
		ClipboardBody: []byte("too big"),
	}}
	deps := tuiDeps(t, &fakeStore{}, rend)
	deps.NewSubmitter = func(config.Resolved, store.Store, daemon.Client, tmux.TargetContext) submitter.Submitter {
		return &recordingSubmitter{err: &submitter.BodyTooLargeError{Bytes: 7, Limit: 3}}
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	var tooLarge *submitter.BodyTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("want *BodyTooLargeError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("ExitCode = %d, want ExitPrompt", ExitCode(err))
	}
}

func TestTUI_ClipboardEmptyExitsExitPrompt(t *testing.T) {
	rend := &recordingRenderer{result: tui.Result{
		Action:        tui.ActionClipboard,
		ClipboardBody: nil,
	}}
	deps := tuiDeps(t, &fakeStore{}, rend)
	deps.NewSubmitter = func(config.Resolved, store.Store, daemon.Client, tmux.TargetContext) submitter.Submitter {
		return &recordingSubmitter{err: &clipboard.EmptyClipboardError{}}
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	var empty *clipboard.EmptyClipboardError
	if !errors.As(err, &empty) {
		t.Fatalf("want *clipboard.EmptyClipboardError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("ExitCode = %d, want ExitPrompt", ExitCode(err))
	}
}

func TestTUI_ClipboardDaemonFailureExitsExitDaemon(t *testing.T) {
	rend := &recordingRenderer{result: tui.Result{
		Action:        tui.ActionClipboard,
		ClipboardBody: []byte("x"),
	}}
	deps := tuiDeps(t, &fakeStore{}, rend)
	dialErr := &daemon.SocketUnavailableError{Path: "/tmp/x.sock", Reason: "broken pipe mid-submit"}
	deps.NewSubmitter = func(config.Resolved, store.Store, daemon.Client, tmux.TargetContext) submitter.Submitter {
		return &recordingSubmitter{err: dialErr}
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	if !errors.Is(err, dialErr) {
		t.Fatalf("want SocketUnavailableError, got %v", err)
	}
	if ExitCode(err) != ExitDaemon {
		t.Fatalf("ExitCode = %d, want ExitDaemon", ExitCode(err))
	}
}

// TestTUI_ClipboardEndToEndThroughRealSubmitter wires the real submitter.New
// against a capturing fakeDaemonClient so the test observes the actual
// SubmitRequest that would reach the daemon for a clipboard selection.
func TestTUI_ClipboardEndToEndThroughRealSubmitter(t *testing.T) {
	fs := &fakeStore{}
	rend := &recordingRenderer{result: tui.Result{
		Action:        tui.ActionClipboard,
		ClipboardBody: []byte("end-to-end clip"),
	}}
	var captured daemon.SubmitRequest
	dc := &fakeDaemonClient{
		statusFn: func() (daemon.StatusResponse, error) { return daemon.StatusResponse{}, nil },
		submitFn: func(req daemon.SubmitRequest) (daemon.SubmitResponse, error) {
			captured = req
			return daemon.SubmitResponse{Accepted: true, JobID: "j-e2e"}, nil
		},
	}
	deps := tuiDeps(t, fs, rend, func(c *config.Resolved) {
		c.DefaultMode = "paste"
		c.DefaultEnter = false
		c.Sanitize = "safe"
		c.MaxPasteBytes = 1024
		c.VerificationTimeoutMS = 5000
		c.VerificationPollIntervalMS = 100
	})
	deps.NewDaemonClient = func(config.Resolved) (daemon.Client, error) { return dc, nil }
	deps.NewSubmitter = func(cfg config.Resolved, s store.Store, c daemon.Client, target tmux.TargetContext) submitter.Submitter {
		return submitter.New(s, c, cfg, target)
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%9")
	if err != nil {
		t.Fatalf("end-to-end: want nil, got %v", err)
	}
	if captured.Job.Source != daemon.SourceClipboard {
		t.Errorf("Source = %q, want clipboard", captured.Job.Source)
	}
	if string(captured.Job.Body) != "end-to-end clip" {
		t.Errorf("Body = %q", captured.Job.Body)
	}
	if captured.Job.PaneID != "%9" {
		t.Errorf("PaneID = %q, want %%9", captured.Job.PaneID)
	}
	if captured.Job.PromptID != "" || captured.Job.SourcePath != "" {
		t.Errorf("prompt fields should be empty for clipboard, got PromptID=%q SourcePath=%q",
			captured.Job.PromptID, captured.Job.SourcePath)
	}
	if captured.Job.Mode != "paste" || captured.Job.SanitizeMode != "safe" {
		t.Errorf("delivery = Mode=%q Sanitize=%q, want paste/safe (config defaults)",
			captured.Job.Mode, captured.Job.SanitizeMode)
	}
}

func TestTUI_PanePreflightSurfacesAdapterError(t *testing.T) {
	rend := &recordingRenderer{result: tui.Result{Action: tui.ActionCancel}}
	deps := tuiDeps(t, &fakeStore{}, rend)
	envErr := &tmux.EnvError{Reason: "tmux server not running"}
	deps.NewTmux = func() (tmux.Adapter, error) {
		return &fakeAdapter{paneExistsErr: envErr}, nil
	}

	_, _, err := executeRootWith(t, deps, "tui", "--target-pane", "%0")
	if !errors.Is(err, envErr) {
		t.Fatalf("want adapter EnvError, got %v", err)
	}
	if rend.called {
		t.Fatal("renderer must not run when pane preflight errors")
	}
}
