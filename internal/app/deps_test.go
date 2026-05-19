package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	applife "github.com/hsadler/tprompt/internal/app/lifecycle"
	dlife "github.com/hsadler/tprompt/internal/daemon/lifecycle"
	"github.com/hsadler/tprompt/internal/tui"
)

// TestErrLauncherSurfacesConfigFailure asserts that errLauncher
// (returned by productionNewLauncher when os.Executable() fails) maps
// to a structured StartResult with ReasonConfig and a detail that
// names the underlying resolution failure. Without this, callers
// would see a generic spawn error like "empty executable path" much
// later, with no signal that executable resolution is the root cause.
func TestErrLauncherSurfacesConfigFailure(t *testing.T) {
	l := errLauncher{detail: "resolve executable path: boom"}
	res := l.Start(context.Background(), applife.IntentExplicitStart)
	if res.Outcome != dlife.OutcomeFailed {
		t.Fatalf("Outcome = %v, want Failed", res.Outcome)
	}
	if res.Reason != dlife.ReasonConfig {
		t.Fatalf("Reason = %v, want Config", res.Reason)
	}
	if !strings.Contains(res.Detail, "resolve executable path") {
		t.Fatalf("Detail = %q, want it to name executable resolution", res.Detail)
	}
}

// stubRendererSubmitter captures Submit calls for parseTestRenderer tests.
type stubRendererSubmitter struct {
	calls []tui.Result
	err   error
}

func (r *stubRendererSubmitter) Submit(result tui.Result) error {
	r.calls = append(r.calls, result)
	return r.err
}

func TestParseTestRenderer_Cancel(t *testing.T) {
	r, err := parseTestRenderer("cancel", nil)
	if err != nil {
		t.Fatalf("parseTestRenderer: %v", err)
	}
	got, err := r.Run(tui.State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Action != tui.ActionCancel {
		t.Errorf("Action = %q, want ActionCancel", got.Action)
	}
}

func TestParseTestRenderer_Clipboard(t *testing.T) {
	sub := &stubRendererSubmitter{}
	r, err := parseTestRenderer("clipboard:hello from tests", sub)
	if err != nil {
		t.Fatalf("parseTestRenderer: %v", err)
	}
	got, err := r.Run(tui.State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Action != tui.ActionClipboard {
		t.Errorf("Action = %q, want ActionClipboard", got.Action)
	}
	if string(got.ClipboardBody) != "hello from tests" {
		t.Errorf("ClipboardBody = %q", got.ClipboardBody)
	}
	// Stub owns the Submit call so runTUI's ActionClipboard branch stays a no-op.
	if len(sub.calls) != 1 {
		t.Fatalf("Submit calls = %d, want 1", len(sub.calls))
	}
	if sub.calls[0].Action != tui.ActionClipboard || string(sub.calls[0].ClipboardBody) != "hello from tests" {
		t.Errorf("Submit call = %+v, want ActionClipboard with matching body", sub.calls[0])
	}
}

func TestParseTestRenderer_ClipboardSurfacesSubmitErr(t *testing.T) {
	// `clipboard:` with empty body exercises the Submitter's EmptyClipboardError
	// path. The stub
	// Renderer now owns the Submit call, so its error must propagate out of Run.
	boom := errors.New("handoff refused")
	sub := &stubRendererSubmitter{err: boom}
	r, err := parseTestRenderer("clipboard:", sub)
	if err != nil {
		t.Fatalf("parseTestRenderer: %v", err)
	}
	got, runErr := r.Run(tui.State{})
	if !errors.Is(runErr, boom) {
		t.Fatalf("Run err = %v, want %v", runErr, boom)
	}
	if got.Action != tui.ActionClipboard {
		t.Errorf("Action = %q, want ActionClipboard", got.Action)
	}
	if len(got.ClipboardBody) != 0 {
		t.Errorf("ClipboardBody = %q, want empty", got.ClipboardBody)
	}
	if len(sub.calls) != 1 {
		t.Fatalf("Submit calls = %d, want 1", len(sub.calls))
	}
}

func TestParseTestRenderer_Unknown(t *testing.T) {
	_, err := parseTestRenderer("nonsense", nil)
	if err == nil {
		t.Fatal("want error for unknown spec")
	}
	if !strings.Contains(err.Error(), "unsupported spec") {
		t.Errorf("error = %q, want 'unsupported spec'", err)
	}
}
