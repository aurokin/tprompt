package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/sanitize"
	"github.com/hsadler/tprompt/internal/tmux"
)

func pasteDeps(t *testing.T, body []byte, adapter *fakeAdapter, cfgOverride ...func(*config.Resolved)) Deps {
	t.Helper()
	deps := workingDeps(t, &fakeStore{})
	deps.LoadConfig = func(string) (config.Resolved, error) {
		t.Fatal("LoadConfig should not be called by paste")
		return config.Resolved{}, nil
	}
	deps.LoadPasteConfig = func(string) (config.Resolved, error) {
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
	deps.NewClip = func(config.Resolved) (clipboard.Reader, error) {
		return clipboard.NewStatic(body), nil
	}
	return deps
}

func TestPaste_HappyPathPasteWithFlagTarget(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte("clipboard body"), adapter)

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%5")
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
	if call.Body != "clipboard body" {
		t.Fatalf("body mismatch: got %q", call.Body)
	}
	if call.Enter {
		t.Fatal("enter should default to false")
	}
}

func TestPaste_DoesNotRequirePromptsDir(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte("clipboard body"), adapter, func(c *config.Resolved) {
		c.PromptsDir = ""
	})

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%5")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(adapter.pasteCalls) != 1 {
		t.Fatalf("want 1 paste call, got %d", len(adapter.pasteCalls))
	}
}

func TestPaste_UsesCurrentContextWhenNoFlag(t *testing.T) {
	adapter := &fakeAdapter{
		currentContext: tmux.TargetContext{PaneID: "%9", ClientTTY: "/dev/pts/0"},
	}
	deps := pasteDeps(t, []byte("clip"), adapter)
	deps.Env = func(k string) string {
		if k == "TMUX" {
			return "/tmp/tmux-0/default,1,0"
		}
		return ""
	}

	_, _, err := executeRootWith(t, deps, "paste")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if adapter.pasteCalls[0].Target.PaneID != "%9" {
		t.Fatalf("expected current-context pane, got %q", adapter.pasteCalls[0].Target.PaneID)
	}
}

func TestPaste_NotInTmuxNoFlag(t *testing.T) {
	adapter := &fakeAdapter{}
	deps := pasteDeps(t, []byte("clip"), adapter)
	deps.Env = func(string) string { return "" }
	deps.NewClip = func(config.Resolved) (clipboard.Reader, error) {
		t.Fatal("NewClip should not be called when no tmux target can be resolved")
		return nil, nil
	}

	_, _, err := executeRootWith(t, deps, "paste")
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

func TestPaste_PaneMissing(t *testing.T) {
	adapter := &fakeAdapter{paneExists: false}
	deps := pasteDeps(t, []byte("clip"), adapter)

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%99")
	var pm *tmux.PaneMissingError
	if !errors.As(err, &pm) {
		t.Fatalf("want PaneMissingError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitTmux {
		t.Fatalf("want ExitTmux, got %d", ExitCode(err))
	}
}

func TestPaste_ExplicitTargetValidatesClipboardBeforePaneProbe(t *testing.T) {
	adapter := &fakeAdapter{paneExists: false}
	deps := pasteDeps(t, nil, adapter)

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%99")
	var empty *clipboard.EmptyClipboardError
	if !errors.As(err, &empty) {
		t.Fatalf("want EmptyClipboardError before pane probe, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
	if len(adapter.pasteCalls) != 0 {
		t.Fatal("adapter.Paste should not be called after clipboard validation failure")
	}
}

func TestPaste_ExplicitTargetOversizeBeforePaneProbe(t *testing.T) {
	adapter := &fakeAdapter{paneExists: false}
	deps := pasteDeps(t, []byte(strings.Repeat("x", 100)), adapter, func(c *config.Resolved) {
		c.MaxPasteBytes = 50
	})

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%99")
	var oe *clipboard.OversizeError
	if !errors.As(err, &oe) {
		t.Fatalf("want clipboard.OversizeError before pane probe, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
}

func TestPaste_FlagModeOverridesConfig(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte("clip"), adapter, func(c *config.Resolved) { c.DefaultMode = "paste" })

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1", "--mode", "type")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(adapter.typeCalls) != 1 || len(adapter.pasteCalls) != 0 {
		t.Fatalf("want Type call only, got paste=%d type=%d", len(adapter.pasteCalls), len(adapter.typeCalls))
	}
}

func TestPaste_EnterFlag(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte("clip"), adapter)

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1", "--enter")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !adapter.pasteCalls[0].Enter {
		t.Fatal("expected Enter=true")
	}
}

func TestPaste_InvalidModeFlag(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte("clip"), adapter)

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1", "--mode", "bogus")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "invalid delivery mode") {
		t.Fatalf("want delivery-mode error, got %v", err)
	}
}

func TestPaste_InvalidSanitizeFlag(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte("clip"), adapter)

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1", "--sanitize", "bogus")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "invalid sanitize mode") {
		t.Fatalf("want sanitize error, got %v", err)
	}
}

func TestPaste_SanitizeSafeStripsDangerous(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte("before\x1b]0;title\x07after"), adapter)

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1", "--sanitize", "safe")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if adapter.pasteCalls[0].Body != "beforeafter" {
		t.Fatalf("body = %q, want %q", adapter.pasteCalls[0].Body, "beforeafter")
	}
}

func TestPaste_SanitizeStrictRejectsEscape(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte("ok\x1b]0;t\x07end"), adapter)

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1", "--sanitize", "strict")
	var sre *sanitize.StrictRejectError
	if !errors.As(err, &sre) {
		t.Fatalf("want StrictRejectError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
	if len(adapter.pasteCalls) != 0 {
		t.Fatal("adapter should not be called after strict rejection")
	}
}

func TestPaste_ClipboardEmpty(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, nil, adapter)

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1")
	var empty *clipboard.EmptyClipboardError
	if !errors.As(err, &empty) {
		t.Fatalf("want EmptyClipboardError, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
	if len(adapter.pasteCalls) != 0 {
		t.Fatal("adapter should not be called after clipboard validation failure")
	}
}

func TestPaste_ClipboardInvalidUTF8(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte{0xff, 0xfe}, adapter)

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1")
	var invalid *clipboard.InvalidUTF8Error
	if !errors.As(err, &invalid) {
		t.Fatalf("want InvalidUTF8Error, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
}

func TestPaste_ClipboardOversize(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte(strings.Repeat("x", 100)), adapter, func(c *config.Resolved) {
		c.MaxPasteBytes = 50
	})

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1")
	var oe *clipboard.OversizeError
	if !errors.As(err, &oe) {
		t.Fatalf("want clipboard.OversizeError, got %T: %v", err, err)
	}
	if oe.Bytes != 100 || oe.Limit != 50 {
		t.Fatalf("want Bytes=100 Limit=50, got Bytes=%d Limit=%d", oe.Bytes, oe.Limit)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
	if len(adapter.pasteCalls) != 0 {
		t.Fatal("should reject before invoking adapter")
	}
}

func TestPaste_OversizeCheckedBeforeSanitize(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	body := []byte("x\x1b]0;title\x07y" + strings.Repeat("z", 50))
	deps := pasteDeps(t, body, adapter, func(c *config.Resolved) { c.MaxPasteBytes = 20 })

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1", "--sanitize", "safe")
	var oe *clipboard.OversizeError
	if !errors.As(err, &oe) {
		t.Fatalf("want clipboard.OversizeError (cap is pre-sanitize), got %T: %v", err, err)
	}
	if len(adapter.pasteCalls) != 0 {
		t.Fatal("should reject before invoking adapter")
	}
}

func TestPaste_NoClipboardReader(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte("unused"), adapter)
	deps.NewClip = func(config.Resolved) (clipboard.Reader, error) {
		return nil, clipboard.ErrNoReaderAvailable
	}

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1")
	if !errors.Is(err, clipboard.ErrNoReaderAvailable) {
		t.Fatalf("want ErrNoReaderAvailable, got %T: %v", err, err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
}

func TestPaste_ClipboardReadError(t *testing.T) {
	adapter := &fakeAdapter{paneExists: true}
	deps := pasteDeps(t, []byte("unused"), adapter)
	deps.NewClip = func(config.Resolved) (clipboard.Reader, error) {
		return clipboard.NewCommand([]string{"sh", "-c", "printf boom >&2; exit 7"}), nil
	}

	_, _, err := executeRootWith(t, deps, "paste", "--target-pane", "%1")
	var readErr *clipboard.ReadError
	if !errors.As(err, &readErr) {
		t.Fatalf("want ReadError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want stderr in error, got %v", err)
	}
	if ExitCode(err) != ExitPrompt {
		t.Fatalf("want ExitPrompt, got %d", ExitCode(err))
	}
	if len(adapter.pasteCalls) != 0 {
		t.Fatal("adapter should not be called after clipboard read failure")
	}
}
