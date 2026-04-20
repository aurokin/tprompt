package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/sanitize"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
)

type fakeAdapter struct {
	currentContext    tmux.TargetContext
	currentContextErr error
	paneExists        bool
	paneExistsErr     error
	pasteCalls        []pasteCall
	typeCalls         []typeCall
	pasteErr          error
	typeErr           error
}

type pasteCall struct {
	Target tmux.TargetContext
	Body   string
	Enter  bool
}

type typeCall struct {
	Target tmux.TargetContext
	Body   string
	Enter  bool
}

func (f *fakeAdapter) CurrentContext() (tmux.TargetContext, error) {
	return f.currentContext, f.currentContextErr
}

func (f *fakeAdapter) PaneExists(context.Context, string) (bool, error) {
	return f.paneExists, f.paneExistsErr
}
func (f *fakeAdapter) IsTargetSelected(context.Context, tmux.TargetContext) (bool, error) {
	return true, nil
}
func (f *fakeAdapter) CapturePaneTail(string, int) (string, error) { return "", nil }

func (f *fakeAdapter) Paste(_ context.Context, t tmux.TargetContext, body string, enter bool) error {
	f.pasteCalls = append(f.pasteCalls, pasteCall{Target: t, Body: body, Enter: enter})
	return f.pasteErr
}

func (f *fakeAdapter) Type(_ context.Context, t tmux.TargetContext, body string, enter bool) error {
	f.typeCalls = append(f.typeCalls, typeCall{Target: t, Body: body, Enter: enter})
	return f.typeErr
}
func (f *fakeAdapter) DisplayMessage(tmux.MessageTarget, string) error { return nil }

func sendDeps(t *testing.T, prompt store.Prompt, adapter *fakeAdapter, cfgOverride ...func(*config.Resolved)) Deps {
	t.Helper()
	deps := workingDeps(t, &fakeStore{prompts: map[string]store.Prompt{prompt.ID: prompt}})
	deps.LoadConfig = func(string) (config.Resolved, error) {
		cfg := config.Resolved{
			PromptsDir:    "/prompts",
			DefaultMode:   "paste",
			Sanitize:      "off",
			MaxPasteBytes: 1 << 20,
		}
		for _, f := range cfgOverride {
			f(&cfg)
		}
		return cfg, nil
	}
	deps.NewTmux = func() (tmux.Adapter, error) { return adapter, nil }
	deps.NewClip = func(config.Resolved) (clipboard.Reader, error) { return nil, nil }
	return deps
}

func basePrompt() store.Prompt {
	return store.Prompt{
		Summary: store.Summary{ID: "code-review", Path: "/prompts/code-review.md"},
		Body:    "Review this code.",
	}
}

func TestSend_HappyPathPasteWithFlagTarget(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, basePrompt(), adapter)

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%5")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(adapter.pasteCalls) != 1 {
		t.Fatalf("want 1 paste call, got %d", len(adapter.pasteCalls))
	}
	call := adapter.pasteCalls[0]
	if call.Target.PaneID != "%5" {
		t.Fatalf("target mismatch: got %q", call.Target.PaneID)
	}
	if call.Body != "Review this code." {
		t.Fatalf("body mismatch: got %q", call.Body)
	}
	if call.Enter {
		t.Fatal("enter should default to false")
	}
}

func TestSend_UsesCurrentContextWhenNoFlag(t *testing.T) {
	adapter := &fakeAdapter{
		currentContext: tmux.TargetContext{PaneID: "%9", ClientTTY: "/dev/pts/0"},
	}
	deps := sendDeps(t, basePrompt(), adapter)
	deps.Env = func(k string) string {
		if k == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}

	_, _, err := executeRootWith(t, deps, "send", "code-review")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if adapter.pasteCalls[0].Target.PaneID != "%9" {
		t.Fatalf("expected current-context pane, got %q", adapter.pasteCalls[0].Target.PaneID)
	}
}

func TestSend_NotInTmuxNoFlag(t *testing.T) {
	adapter := &fakeAdapter{}
	deps := sendDeps(t, basePrompt(), adapter)
	deps.Env = func(string) string { return "" } // no TMUX

	_, _, err := executeRootWith(t, deps, "send", "code-review")
	var envErr *tmux.EnvError
	if !errors.As(err, &envErr) {
		t.Fatalf("want EnvError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitTmux {
		t.Fatalf("want ExitTmux, got %d", ExitCode(err))
	}
	if len(adapter.pasteCalls) != 0 {
		t.Fatal("adapter.Paste should not be called when EnvError")
	}
}

func TestSend_PaneMissing(t *testing.T) {
	adapter := &fakeAdapter{paneExists: false}
	deps := sendDeps(t, basePrompt(), adapter)

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%99")
	var pm *tmux.PaneMissingError
	if !errors.As(err, &pm) {
		t.Fatalf("want PaneMissingError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitTmux {
		t.Fatalf("want ExitTmux, got %d", ExitCode(err))
	}
}

func TestSend_Oversize(t *testing.T) {
	p := basePrompt()
	p.Body = strings.Repeat("x", 100)
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, p, adapter, func(c *config.Resolved) { c.MaxPasteBytes = 50 })

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1")
	var oe *tmux.OversizeError
	if !errors.As(err, &oe) {
		t.Fatalf("want OversizeError, got %T: %v", err, err)
	}
	if oe.Bytes != 100 || oe.Limit != 50 {
		t.Fatalf("want Bytes=100 Limit=50, got Bytes=%d Limit=%d", oe.Bytes, oe.Limit)
	}
	if ExitCode(err) != ExitDelivery {
		t.Fatalf("want ExitDelivery, got %d", ExitCode(err))
	}
	if len(adapter.pasteCalls) != 0 {
		t.Fatal("should reject before invoking adapter")
	}
}

func TestSend_CurrentContextError(t *testing.T) {
	adapter := &fakeAdapter{
		currentContextErr: &tmux.EnvError{Reason: "no server running"},
	}
	deps := sendDeps(t, basePrompt(), adapter)
	deps.Env = func(k string) string {
		if k == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}

	_, _, err := executeRootWith(t, deps, "send", "code-review")
	var envErr *tmux.EnvError
	if !errors.As(err, &envErr) {
		t.Fatalf("want EnvError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitTmux {
		t.Fatalf("want ExitTmux, got %d", ExitCode(err))
	}
	if len(adapter.pasteCalls) != 0 {
		t.Fatal("Paste should not be called when CurrentContext fails")
	}
}

func TestSend_UnknownPrompt(t *testing.T) {
	adapter := &fakeAdapter{}
	deps := sendDeps(t, basePrompt(), adapter)

	_, _, err := executeRootWith(t, deps, "send", "nope", "--target-pane", "%1")
	var nf *store.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
}

func TestSend_FlagModeOverridesConfig(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, basePrompt(), adapter, func(c *config.Resolved) { c.DefaultMode = "paste" })

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1", "--mode", "type")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(adapter.typeCalls) != 1 || len(adapter.pasteCalls) != 0 {
		t.Fatalf("want Type call only, got paste=%d type=%d", len(adapter.pasteCalls), len(adapter.typeCalls))
	}
}

func TestSend_FrontmatterModeOverridesConfig(t *testing.T) {
	p := basePrompt()
	p.Defaults.Mode = "type"
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, p, adapter, func(c *config.Resolved) { c.DefaultMode = "paste" })

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(adapter.typeCalls) != 1 || len(adapter.pasteCalls) != 0 {
		t.Fatalf("want Type call (frontmatter override), got paste=%d type=%d",
			len(adapter.pasteCalls), len(adapter.typeCalls))
	}
}

func TestSend_FlagWinsOverFrontmatter(t *testing.T) {
	p := basePrompt()
	p.Defaults.Mode = "type"
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, p, adapter)

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1", "--mode", "paste")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(adapter.pasteCalls) != 1 || len(adapter.typeCalls) != 0 {
		t.Fatalf("want Paste call (flag over frontmatter), got paste=%d type=%d",
			len(adapter.pasteCalls), len(adapter.typeCalls))
	}
}

func TestSend_EnterFlag(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, basePrompt(), adapter)

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1", "--enter")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !adapter.pasteCalls[0].Enter {
		t.Fatal("expected Enter=true")
	}
}

func TestSend_InvalidModeFlag(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, basePrompt(), adapter)

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1", "--mode", "bogus")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "invalid delivery mode") {
		t.Fatalf("want delivery-mode error, got %v", err)
	}
}

func TestSend_InvalidSanitizeFlag(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, basePrompt(), adapter)

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1", "--sanitize", "bogus")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "invalid sanitize mode") {
		t.Fatalf("want sanitize error, got %v", err)
	}
}

func TestSend_SanitizeOffUnchanged(t *testing.T) {
	p := basePrompt()
	p.Body = "raw body"
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, p, adapter)

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1", "--sanitize", "off")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if adapter.pasteCalls[0].Body != "raw body" {
		t.Fatalf("body should pass through for --sanitize off, got %q", adapter.pasteCalls[0].Body)
	}
}

func TestSend_SanitizeSafeStripsDangerous(t *testing.T) {
	p := basePrompt()
	p.Body = "before\x1b]0;title\x07after"
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, p, adapter)

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1", "--sanitize", "safe")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(adapter.pasteCalls) != 1 {
		t.Fatalf("want 1 paste call, got %d", len(adapter.pasteCalls))
	}
	if adapter.pasteCalls[0].Body != "beforeafter" {
		t.Fatalf("body = %q, want %q", adapter.pasteCalls[0].Body, "beforeafter")
	}
}

func TestSend_SanitizeStrictRejectsEscape(t *testing.T) {
	p := basePrompt()
	p.Body = "ok\x1b]0;t\x07end"
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, p, adapter)

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1", "--sanitize", "strict")
	var sre *sanitize.StrictRejectError
	if !errors.As(err, &sre) {
		t.Fatalf("want StrictRejectError, got %T: %v", err, err)
	}
	if sre.Offset != 2 || sre.Class != "OSC" {
		t.Fatalf("got offset=%d class=%q, want 2/OSC", sre.Offset, sre.Class)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
	if len(adapter.pasteCalls) != 0 {
		t.Fatal("adapter should not be called after strict rejection")
	}
}

func TestSend_OversizeCheckedBeforeSanitize(t *testing.T) {
	// Body with escape sequence, safe mode would shrink it — but the cap
	// must be enforced pre-sanitize, so oversize still fails.
	p := basePrompt()
	p.Body = "x\x1b]0;title\x07y" + strings.Repeat("z", 50)
	adapter := &fakeAdapter{paneExists: true}
	deps := sendDeps(t, p, adapter, func(c *config.Resolved) { c.MaxPasteBytes = 20 })

	_, _, err := executeRootWith(t, deps, "send", "code-review", "--target-pane", "%1", "--sanitize", "safe")
	var oe *tmux.OversizeError
	if !errors.As(err, &oe) {
		t.Fatalf("want OversizeError (cap is pre-sanitize), got %T: %v", err, err)
	}
}
