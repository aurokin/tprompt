package daemon

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/hsadler/tprompt/internal/sanitize"
	"github.com/hsadler/tprompt/internal/tmux"
)

// executorAdapter records adapter calls and returns scripted outcomes for
// Paste/Type/PaneExists/IsTargetSelected. Verification helpers default to
// "always ready" so the executor reaches the delivery stage without extra
// setup.
type executorAdapter struct {
	mu          sync.Mutex
	pasteCalls  []executorCall
	typeCalls   []executorCall
	displays    []displayCall
	displayHook func(tmux.MessageTarget, string) error

	pasteErr  error
	typeErr   error
	pasteHook func(context.Context, tmux.TargetContext, string, bool) error
	typeHook  func(context.Context, tmux.TargetContext, string, bool) error

	captureCalls int
	captureTails []string
	captureErrs  []error
	captureHook  func(context.Context, string, int) (string, error)

	paneExists      bool
	paneExistsErr   error
	targetSelected  bool
	targetSelectErr error
}

type executorCall struct {
	Target tmux.TargetContext
	Body   string
	Enter  bool
}

func newExecutorAdapter() *executorAdapter {
	return &executorAdapter{paneExists: true, targetSelected: true}
}

func (a *executorAdapter) CurrentContext() (tmux.TargetContext, error) {
	return tmux.TargetContext{}, nil
}

func (a *executorAdapter) PaneExists(context.Context, string) (bool, error) {
	return a.paneExists, a.paneExistsErr
}

func (a *executorAdapter) IsTargetSelected(context.Context, tmux.TargetContext) (bool, error) {
	return a.targetSelected, a.targetSelectErr
}

func (a *executorAdapter) CapturePaneTail(ctx context.Context, paneID string, lines int) (string, error) {
	a.mu.Lock()
	hook := a.captureHook
	if hook != nil {
		a.captureCalls++
		a.mu.Unlock()
		return hook(ctx, paneID, lines)
	}
	defer a.mu.Unlock()
	call := a.captureCalls
	a.captureCalls++
	if call < len(a.captureErrs) && a.captureErrs[call] != nil {
		return "", a.captureErrs[call]
	}
	if call < len(a.captureTails) {
		return a.captureTails[call], nil
	}
	return "", nil
}

func (a *executorAdapter) Paste(ctx context.Context, t tmux.TargetContext, body string, enter bool) error {
	a.mu.Lock()
	a.pasteCalls = append(a.pasteCalls, executorCall{Target: t, Body: body, Enter: enter})
	hook := a.pasteHook
	err := a.pasteErr
	a.mu.Unlock()
	if hook != nil {
		return hook(ctx, t, body, enter)
	}
	return err
}

func (a *executorAdapter) Type(ctx context.Context, t tmux.TargetContext, body string, enter bool) error {
	a.mu.Lock()
	a.typeCalls = append(a.typeCalls, executorCall{Target: t, Body: body, Enter: enter})
	hook := a.typeHook
	err := a.typeErr
	a.mu.Unlock()
	if hook != nil {
		return hook(ctx, t, body, enter)
	}
	return err
}

func (a *executorAdapter) DisplayMessage(target tmux.MessageTarget, message string) error {
	a.mu.Lock()
	hook := a.displayHook
	if hook != nil {
		a.displays = append(a.displays, displayCall{Target: target, Message: message})
		a.mu.Unlock()
		return hook(target, message)
	}
	defer a.mu.Unlock()
	a.displays = append(a.displays, displayCall{Target: target, Message: message})
	return nil
}

func (a *executorAdapter) snapshotDisplays() []displayCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]displayCall, len(a.displays))
	copy(out, a.displays)
	return out
}

func (a *executorAdapter) snapshotCaptureCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.captureCalls
}

func basePolicy() VerificationPolicy {
	return VerificationPolicy{TimeoutMS: 100, PollIntervalMS: 1}
}

func makeDeliveryJob(body string, mode string) Job {
	return Job{
		JobID:        "j-1",
		Source:       SourcePrompt,
		PromptID:     "code-review",
		Body:         []byte(body),
		Mode:         mode,
		SanitizeMode: "off",
		PaneID:       "%5",
		Origin:       &tmux.OriginContext{ClientTTY: "/dev/pts/0"},
		Verification: basePolicy(),
	}
}

func TestExecutorHappyPasteIsSilent(t *testing.T) {
	a := newExecutorAdapter()
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	e.Run(context.Background(), makeDeliveryJob("hello", "paste"))

	if len(a.pasteCalls) != 1 || a.pasteCalls[0].Body != "hello" {
		t.Fatalf("paste calls = %+v, want one Paste(\"hello\")", a.pasteCalls)
	}
	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("expected no banner on success, got: %+v", displays)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("expected no log on success, got: %q", logBuf.String())
	}
	if got := a.snapshotCaptureCalls(); got != 0 {
		t.Fatalf("post-injection verification should be disabled by default, got %d captures", got)
	}
}

func TestExecutorHappyTypeIsSilent(t *testing.T) {
	a := newExecutorAdapter()
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	e.Run(context.Background(), makeDeliveryJob("hello", "type"))

	if len(a.typeCalls) != 1 {
		t.Fatalf("type calls = %d, want 1", len(a.typeCalls))
	}
	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("expected no banner on success, got: %+v", displays)
	}
}

func TestExecutorPostInjectionVerificationChangedTailIsSilent(t *testing.T) {
	a := newExecutorAdapter()
	a.captureTails = []string{"before", "after"}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	e.Run(context.Background(), makeDeliveryJob("hello", "paste"))

	if got := a.snapshotCaptureCalls(); got != 2 {
		t.Fatalf("capture calls = %d, want before and after", got)
	}
	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("changed pane tail should not warn, got: %+v", displays)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("changed pane tail should not log, got: %q", logBuf.String())
	}
}

func TestExecutorPostInjectionVerificationUnchangedTailWarns(t *testing.T) {
	a := newExecutorAdapter()
	a.captureTails = []string{"same", "same"}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	e.Run(context.Background(), makeDeliveryJob("SECRET PROMPT BODY", "paste"))

	displays := a.snapshotDisplays()
	if len(displays) != 1 {
		t.Fatalf("expected warning banner, got %d", len(displays))
	}
	if !strings.Contains(displays[0].Message, "warning: post-injection verification") {
		t.Fatalf("warning banner = %q", displays[0].Message)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "outcome="+OutcomeWarning) {
		t.Fatalf("expected warning log, got: %q", logged)
	}
	if strings.Contains(logged, "SECRET PROMPT BODY") || strings.Contains(displays[0].Message, "SECRET PROMPT BODY") {
		t.Fatalf("warning leaked prompt body; log=%q banner=%q", logged, displays[0].Message)
	}
}

func TestExecutorPostInjectionVerificationWarningRedactsClipboardBody(t *testing.T) {
	a := newExecutorAdapter()
	a.captureTails = []string{"same", "same"}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	job := makeDeliveryJob("SECRET CLIPBOARD BYTES", "paste")
	job.Source = SourceClipboard
	job.PromptID = ""
	e.Run(context.Background(), job)

	displays := a.snapshotDisplays()
	if len(displays) != 1 {
		t.Fatalf("expected warning banner, got %d", len(displays))
	}
	logged := logBuf.String()
	if strings.Contains(logged, "SECRET CLIPBOARD BYTES") || strings.Contains(displays[0].Message, "SECRET CLIPBOARD BYTES") {
		t.Fatalf("warning leaked clipboard body; log=%q banner=%q", logged, displays[0].Message)
	}
	if strings.Contains(logged, "prompt_id=") {
		t.Fatalf("clipboard warning log should omit prompt_id: %q", logged)
	}
}

func TestExecutorPostInjectionVerificationCaptureFailureDoesNotFailDelivery(t *testing.T) {
	a := newExecutorAdapter()
	a.captureErrs = []error{errors.New("SECRET CAPTURE OUTPUT")}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	e.Run(context.Background(), makeDeliveryJob("hi", "paste"))

	if len(a.pasteCalls) != 1 {
		t.Fatalf("capture failure should not block delivery, paste calls=%d", len(a.pasteCalls))
	}
	if displays := a.snapshotDisplays(); len(displays) != 1 {
		t.Fatalf("capture failure should warn once, got %+v", displays)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "capture before delivery failed") {
		t.Fatalf("expected capture warning log, got %q", logged)
	}
	if strings.Contains(logged, "SECRET CAPTURE OUTPUT") {
		t.Fatalf("warning should not log raw capture failure text: %q", logged)
	}
}

func TestExecutorPostInjectionVerificationPostCaptureFailureDoesNotFailDelivery(t *testing.T) {
	a := newExecutorAdapter()
	a.captureTails = []string{"before"}
	a.captureErrs = []error{nil, errors.New("capture failed")}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	e.Run(context.Background(), makeDeliveryJob("hi", "type"))

	if len(a.typeCalls) != 1 {
		t.Fatalf("post-capture failure should not block delivery, type calls=%d", len(a.typeCalls))
	}
	if displays := a.snapshotDisplays(); len(displays) != 1 {
		t.Fatalf("post-capture failure should warn once, got %+v", displays)
	}
	if !strings.Contains(logBuf.String(), "capture after delivery failed") {
		t.Fatalf("expected capture warning log, got %q", logBuf.String())
	}
}

func TestExecutorPostInjectionWarningRetriesWithoutPaneWhenBannerTargetFails(t *testing.T) {
	a := newExecutorAdapter()
	a.captureTails = []string{"before"}
	a.captureErrs = []error{nil, errors.New("capture failed")}
	a.displayHook = func(target tmux.MessageTarget, _ string) error {
		if target.PaneID != "" {
			return &tmux.DeliveryError{Op: "display-message", Message: "can't find pane"}
		}
		return nil
	}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	job := makeDeliveryJob("hi", "paste")
	job.Origin = &tmux.OriginContext{Session: "$1", Window: "@2"}
	e.Run(context.Background(), job)

	displays := a.snapshotDisplays()
	if len(displays) != 2 {
		t.Fatalf("expected pane-scoped warning plus broader fallback, got %+v", displays)
	}
	if displays[0].Target.PaneID != "%5" {
		t.Fatalf("first warning should target original pane, got %+v", displays[0].Target)
	}
	if displays[1].Target.PaneID != "" {
		t.Fatalf("fallback warning should clear missing pane, got %+v", displays[1].Target)
	}
	if displays[1].Target.Window != "@2" || displays[1].Target.Session != "$1" {
		t.Fatalf("fallback warning should preserve broader scope, got %+v", displays[1].Target)
	}
	if !strings.Contains(logBuf.String(), "outcome="+OutcomeWarning) {
		t.Fatalf("expected warning log, got %q", logBuf.String())
	}
}

func TestExecutorOversizeRejectsBeforeDelivery(t *testing.T) {
	a := newExecutorAdapter()
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 4)

	e.Run(context.Background(), makeDeliveryJob("toomuch", "paste"))

	if len(a.pasteCalls) != 0 {
		t.Fatalf("paste should not be called on oversize, got %d calls", len(a.pasteCalls))
	}
	displays := a.snapshotDisplays()
	if len(displays) != 1 {
		t.Fatalf("expected 1 banner, got %d", len(displays))
	}
	if !strings.HasPrefix(displays[0].Message, BannerPrefix) {
		t.Fatalf("banner missing prefix: %q", displays[0].Message)
	}
	if !strings.Contains(displays[0].Message, "max_paste_bytes") {
		t.Fatalf("banner should mention max_paste_bytes, got: %q", displays[0].Message)
	}
	if !strings.Contains(logBuf.String(), "outcome="+OutcomeOversize) {
		t.Fatalf("expected oversize log entry, got: %q", logBuf.String())
	}
}

func TestExecutorSanitizeStrictRejection(t *testing.T) {
	a := newExecutorAdapter()
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	job := makeDeliveryJob("x\x1b]0;title\x07y", "paste")
	job.SanitizeMode = "strict"
	e.Run(context.Background(), job)

	if len(a.pasteCalls) != 0 {
		t.Fatalf("paste should not be called when sanitizer rejects")
	}
	displays := a.snapshotDisplays()
	if len(displays) != 1 || !strings.Contains(displays[0].Message, "OSC") {
		t.Fatalf("expected sanitize-reject banner with OSC, got: %+v", displays)
	}
	if !strings.Contains(logBuf.String(), "outcome="+OutcomeSanitizeReject) {
		t.Fatalf("expected sanitize_reject log entry, got: %q", logBuf.String())
	}
}

func TestExecutorSanitizeSafeStrips(t *testing.T) {
	a := newExecutorAdapter()
	e := NewExecutor(a, NewLoggerWriter(&bytes.Buffer{}), 1<<20)

	job := makeDeliveryJob("before\x1b]0;title\x07after", "paste")
	job.SanitizeMode = "safe"
	e.Run(context.Background(), job)

	if len(a.pasteCalls) != 1 {
		t.Fatalf("expected paste, got %d calls", len(a.pasteCalls))
	}
	if a.pasteCalls[0].Body != "beforeafter" {
		t.Fatalf("body = %q, want stripped", a.pasteCalls[0].Body)
	}
}

func TestExecutorPaneVanishedDuringVerify(t *testing.T) {
	a := newExecutorAdapter()
	a.paneExists = false
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	e.Run(context.Background(), makeDeliveryJob("hi", "paste"))

	if len(a.pasteCalls) != 0 {
		t.Fatal("paste should not be called when verification fails")
	}
	if got := a.snapshotCaptureCalls(); got != 0 {
		t.Fatalf("post-injection verification should not capture after pane-missing verify failure, got %d captures", got)
	}
	displays := a.snapshotDisplays()
	if len(displays) != 1 {
		t.Fatalf("expected 1 banner, got %d", len(displays))
	}
	if !strings.Contains(displays[0].Message, "%5") {
		t.Fatalf("banner should reference pane id, got: %q", displays[0].Message)
	}
	if displays[0].Target.PaneID != "" {
		t.Fatalf("pane-missing banner should not target dead pane, got %+v", displays[0].Target)
	}
	if !strings.Contains(logBuf.String(), "outcome="+OutcomePaneMissing) {
		t.Fatalf("expected pane_missing log, got: %q", logBuf.String())
	}
}

func TestExecutorPaneMissingWithoutRemainingBannerScopeLogsOnly(t *testing.T) {
	a := newExecutorAdapter()
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	job := makeDeliveryJob("hi", "paste")
	job.Origin = nil
	a.paneExists = false

	e.Run(context.Background(), job)

	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("pane-missing without client/window/session should not banner, got %+v", displays)
	}
	if !strings.Contains(logBuf.String(), "outcome="+OutcomePaneMissing) {
		t.Fatalf("expected pane_missing log, got: %q", logBuf.String())
	}
}

func TestExecutorPaneMissingBannerPreservesBroaderTargetScope(t *testing.T) {
	a := newExecutorAdapter()
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	job := makeDeliveryJob("hi", "paste")
	job.Origin = &tmux.OriginContext{Session: "$1", Window: "@2", ClientTTY: "/dev/pts/0"}
	a.paneExists = false

	e.Run(context.Background(), job)

	displays := a.snapshotDisplays()
	if len(displays) != 1 {
		t.Fatalf("expected 1 banner, got %d", len(displays))
	}
	if displays[0].Target.PaneID != "" {
		t.Fatalf("pane-missing banner should clear pane id, got %+v", displays[0].Target)
	}
	if displays[0].Target.Window != "@2" || displays[0].Target.Session != "$1" {
		t.Fatalf("pane-missing banner should preserve window/session, got %+v", displays[0].Target)
	}
	if displays[0].Target.ClientTTY != "/dev/pts/0" {
		t.Fatalf("pane-missing banner should preserve client tty, got %+v", displays[0].Target)
	}
}

func TestExecutorVerifyTimeoutSurfaces(t *testing.T) {
	a := newExecutorAdapter()
	a.targetSelected = false
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	job := makeDeliveryJob("hi", "paste")
	job.Verification = VerificationPolicy{TimeoutMS: 5, PollIntervalMS: 1}
	e.Run(context.Background(), job)

	displays := a.snapshotDisplays()
	if len(displays) != 1 || !strings.Contains(displays[0].Message, "verification timed out") {
		t.Fatalf("expected timeout banner, got: %+v", displays)
	}
	if !strings.Contains(logBuf.String(), "outcome="+OutcomeTimeout) {
		t.Fatalf("expected timeout log, got: %q", logBuf.String())
	}
}

func TestExecutorAdapterDeliveryError(t *testing.T) {
	a := newExecutorAdapter()
	a.pasteErr = &tmux.DeliveryError{Op: "paste-buffer", Target: "%5", Message: "tmux server died"}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	e.Run(context.Background(), makeDeliveryJob("hi", "paste"))

	displays := a.snapshotDisplays()
	if len(displays) != 1 || !strings.Contains(displays[0].Message, "tmux server died") {
		t.Fatalf("expected delivery-error banner, got: %+v", displays)
	}
	if !strings.Contains(logBuf.String(), "outcome="+OutcomeDeliveryError) {
		t.Fatalf("expected delivery_error log, got: %q", logBuf.String())
	}
}

func TestExecutorFailureLogIncludesCorrelationMetadataAndRedactsPromptBody(t *testing.T) {
	a := newExecutorAdapter()
	a.pasteErr = &tmux.DeliveryError{Op: "paste-buffer", Target: "%5", Message: "tmux server died"}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	job := makeDeliveryJob("SECRET PROMPT BODY", "paste")
	job.JobID = "j-prompt"
	job.PromptID = "code-review"
	e.Run(context.Background(), job)

	logged := logBuf.String()
	for _, want := range []string{
		"job_id=j-prompt",
		"pane=%5",
		"source=prompt",
		"prompt_id=code-review",
		"outcome=delivery_error",
		`msg="tmux paste-buffer into %5 failed: tmux server died"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("failure log missing %q\ngot: %q", want, logged)
		}
	}
	if strings.Contains(logged, "SECRET PROMPT BODY") {
		t.Fatalf("failure log leaked prompt body: %q", logged)
	}
}

func TestExecutorFailureLogRedactsClipboardBody(t *testing.T) {
	a := newExecutorAdapter()
	a.typeErr = &tmux.DeliveryError{Op: "send-keys", Target: "%5", Message: "tmux server died"}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	job := makeDeliveryJob("SECRET CLIPBOARD BYTES", "type")
	job.JobID = "j-clip"
	job.Source = SourceClipboard
	job.PromptID = ""
	e.Run(context.Background(), job)

	logged := logBuf.String()
	for _, want := range []string{
		"job_id=j-clip",
		"pane=%5",
		"source=clipboard",
		"outcome=delivery_error",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("clipboard failure log missing %q\ngot: %q", want, logged)
		}
	}
	if strings.Contains(logged, "prompt_id=") {
		t.Fatalf("clipboard failure log should omit prompt_id: %q", logged)
	}
	if strings.Contains(logged, "SECRET CLIPBOARD BYTES") {
		t.Fatalf("failure log leaked clipboard body: %q", logged)
	}
}

func TestExecutorCancellationIsSilent(t *testing.T) {
	a := newExecutorAdapter()
	a.targetSelected = false // never ready, so verify keeps polling
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	job := makeDeliveryJob("hi", "paste")
	job.Verification = VerificationPolicy{TimeoutMS: 5000, PollIntervalMS: 5}
	e.Run(ctx, job)

	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("cancellation should be silent, got banners: %+v", displays)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("cancellation should be silent, got log: %q", logBuf.String())
	}
}

func TestExecutorCancellationAfterVerifySkipsDelivery(t *testing.T) {
	a := newExecutorAdapter()
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	prevSanitize := sanitizeProcess
	defer func() { sanitizeProcess = prevSanitize }()

	ctx, cancel := context.WithCancel(context.Background())
	sanitizeProcess = func(mode string, body []byte) ([]byte, error) {
		cancel()
		return append([]byte(nil), body...), nil
	}

	e.Run(ctx, makeDeliveryJob("hi", "paste"))

	if len(a.pasteCalls) != 0 {
		t.Fatalf("paste should not be called after replacement cancellation")
	}
	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("cancellation should be silent, got banners: %+v", displays)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("cancellation should be silent, got log: %q", logBuf.String())
	}
}

func TestExecutorCancellationBeforePreCaptureIsSilent(t *testing.T) {
	a := newExecutorAdapter()
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := e.deliver(ctx, makeDeliveryJob("hi", "paste"), []byte("hi"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("deliver error = %v, want context.Canceled", err)
	}
	if got := a.snapshotCaptureCalls(); got != 0 {
		t.Fatalf("canceled delivery should not capture, got %d captures", got)
	}
	if len(a.pasteCalls) != 0 {
		t.Fatalf("canceled delivery should not paste, got %d calls", len(a.pasteCalls))
	}
	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("cancellation should be silent, got banners: %+v", displays)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("cancellation should be silent, got log: %q", logBuf.String())
	}
}

func TestExecutorCancellationDuringPreCaptureIsSilent(t *testing.T) {
	a := newExecutorAdapter()
	started := make(chan struct{})
	a.captureHook = func(ctx context.Context, _ string, _ int) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- e.deliver(ctx, makeDeliveryJob("hi", "paste"), []byte("hi"))
	}()

	<-started
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("deliver error = %v, want context.Canceled", err)
	}
	if got := a.snapshotCaptureCalls(); got != 1 {
		t.Fatalf("capture calls = %d, want 1", got)
	}
	if len(a.pasteCalls) != 0 {
		t.Fatalf("canceled delivery should not paste, got %d calls", len(a.pasteCalls))
	}
	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("cancellation should be silent, got banners: %+v", displays)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("cancellation should be silent, got log: %q", logBuf.String())
	}
}

func TestExecutorCancellationBeforePostCaptureStillCompletesDelivery(t *testing.T) {
	a := newExecutorAdapter()
	var cancel context.CancelFunc
	a.captureTails = []string{"before", "same"}
	a.pasteHook = func(context.Context, tmux.TargetContext, string, bool) error {
		cancel()
		return nil
	}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	ctx, cancelFn := context.WithCancel(context.Background())
	cancel = cancelFn
	err := e.deliver(ctx, makeDeliveryJob("hi", "paste"), []byte("hi"))
	if err != nil {
		t.Fatalf("deliver error = %v, want nil after successful paste", err)
	}
	if got := a.snapshotCaptureCalls(); got != 1 {
		t.Fatalf("canceled delivery should skip post-capture, got %d captures", got)
	}
	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("cancellation should be silent, got banners: %+v", displays)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("cancellation should be silent, got log: %q", logBuf.String())
	}
}

func TestExecutorCancellationDuringPostCaptureStillCompletesDelivery(t *testing.T) {
	a := newExecutorAdapter()
	captureStarted := make(chan struct{})
	captureRelease := make(chan struct{})
	var cancel context.CancelFunc
	a.captureHook = func(ctx context.Context, _ string, _ int) (string, error) {
		if a.snapshotCaptureCalls() == 1 {
			return "before", nil
		}
		close(captureStarted)
		<-captureRelease
		return "", ctx.Err()
	}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)
	e.EnablePostInjectionVerification(true)

	ctx, cancelFn := context.WithCancel(context.Background())
	cancel = cancelFn
	done := make(chan error, 1)
	go func() {
		done <- e.deliver(ctx, makeDeliveryJob("hi", "paste"), []byte("hi"))
	}()

	<-captureStarted
	cancel()
	close(captureRelease)

	if err := <-done; err != nil {
		t.Fatalf("deliver error = %v, want nil after successful paste", err)
	}
	if got := a.snapshotCaptureCalls(); got != 2 {
		t.Fatalf("capture calls = %d, want 2", got)
	}
	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("cancellation should be silent, got banners: %+v", displays)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("cancellation should be silent, got log: %q", logBuf.String())
	}
}

func TestExecutorCancellationDuringPasteIsSilent(t *testing.T) {
	a := newExecutorAdapter()
	started := make(chan struct{})
	a.pasteHook = func(ctx context.Context, _ tmux.TargetContext, _ string, _ bool) error {
		close(started)
		<-ctx.Done()
		return &tmux.DeliveryError{
			Op:      "paste-buffer",
			Target:  "%5",
			Message: ctx.Err().Error(),
			Cause:   ctx.Err(),
		}
	}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- e.Run(ctx, makeDeliveryJob("hi", "paste"))
	}()

	<-started
	cancel()

	if completed := <-done; completed {
		t.Fatal("expected canceled delivery to report incomplete job")
	}
	if len(a.pasteCalls) != 1 {
		t.Fatalf("expected one paste call, got %d", len(a.pasteCalls))
	}
	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("cancellation should be silent, got banners: %+v", displays)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("cancellation should be silent, got log: %q", logBuf.String())
	}
}

func TestExecutorCancellationDuringTypeIsSilent(t *testing.T) {
	a := newExecutorAdapter()
	started := make(chan struct{})
	a.typeHook = func(ctx context.Context, _ tmux.TargetContext, _ string, _ bool) error {
		close(started)
		<-ctx.Done()
		return &tmux.DeliveryError{
			Op:      "send-keys",
			Target:  "%5",
			Message: ctx.Err().Error(),
			Cause:   ctx.Err(),
		}
	}
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- e.Run(ctx, makeDeliveryJob("hi", "type"))
	}()

	<-started
	cancel()

	if completed := <-done; completed {
		t.Fatal("expected canceled delivery to report incomplete job")
	}
	if len(a.typeCalls) != 1 {
		t.Fatalf("expected one type call, got %d", len(a.typeCalls))
	}
	if displays := a.snapshotDisplays(); len(displays) != 0 {
		t.Fatalf("cancellation should be silent, got banners: %+v", displays)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("cancellation should be silent, got log: %q", logBuf.String())
	}
}

func TestExecutorTargetsPaneBannerWhenClientTTYEmpty(t *testing.T) {
	a := newExecutorAdapter()
	a.pasteErr = &tmux.DeliveryError{Op: "paste-buffer", Target: "%5", Message: "tmux server died"}
	e := NewExecutor(a, NewLoggerWriter(&bytes.Buffer{}), 1<<20)

	job := makeDeliveryJob("hi", "paste")
	job.Origin = nil
	e.Run(context.Background(), job)

	displays := a.snapshotDisplays()
	if len(displays) != 1 {
		t.Fatalf("expected 1 banner, got %d", len(displays))
	}
	if displays[0].Target.ClientTTY != "" {
		t.Fatalf("expected empty ClientTTY, got %q", displays[0].Target.ClientTTY)
	}
	if displays[0].Target.PaneID != "%5" {
		t.Fatalf("expected pane-targeted banner, got %+v", displays[0].Target)
	}
}

func TestExecutorUnknownModeSurfacesError(t *testing.T) {
	a := newExecutorAdapter()
	var logBuf bytes.Buffer
	e := NewExecutor(a, NewLoggerWriter(&logBuf), 1<<20)

	job := makeDeliveryJob("hi", "stomp")
	e.Run(context.Background(), job)

	displays := a.snapshotDisplays()
	if len(displays) != 1 || !strings.Contains(displays[0].Message, "stomp") {
		t.Fatalf("expected unknown-mode banner, got: %+v", displays)
	}
}

func TestFailureOutcomeMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"PaneMissing", &tmux.PaneMissingError{PaneID: "%1"}, OutcomePaneMissing},
		{"Timeout", &TimeoutError{TimeoutMS: 100}, OutcomeTimeout},
		{"StrictReject", &sanitize.StrictRejectError{Class: "OSC", Offset: 0}, OutcomeSanitizeReject},
		{"DeliveryError", &tmux.DeliveryError{Op: "x", Message: "y"}, OutcomeDeliveryError},
		{"Oversize", &tmux.OversizeError{Bytes: 10, Limit: 5}, OutcomeOversize},
		{"Generic", errors.New("something"), OutcomeDeliveryError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := failureOutcome(tc.err); got != tc.want {
				t.Fatalf("failureOutcome(%T) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
