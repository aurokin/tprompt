package submitter

import (
	"errors"
	"os"
	"testing"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
)

type fakeStore struct {
	prompts map[string]store.Prompt
	err     error
}

func (f *fakeStore) Discover() error                { return nil }
func (f *fakeStore) List() ([]store.Summary, error) { return nil, nil }
func (f *fakeStore) Resolve(id string) (store.Prompt, error) {
	if f.err != nil {
		return store.Prompt{}, f.err
	}
	p, ok := f.prompts[id]
	if !ok {
		return store.Prompt{}, &store.NotFoundError{ID: id}
	}
	return p, nil
}

type fakeDaemonClient struct {
	lastReq daemon.SubmitRequest
	resp    daemon.SubmitResponse
	err     error
	calls   int
}

func (f *fakeDaemonClient) Submit(req daemon.SubmitRequest) (daemon.SubmitResponse, error) {
	f.lastReq = req
	f.calls++
	if f.err != nil {
		return daemon.SubmitResponse{}, f.err
	}
	return f.resp, nil
}

func (f *fakeDaemonClient) Status() (daemon.StatusResponse, error) {
	return daemon.StatusResponse{}, nil
}

func baseCfg() config.Resolved {
	return config.Resolved{
		DefaultMode:                "paste",
		DefaultEnter:               false,
		Sanitize:                   "safe",
		MaxPasteBytes:              1024,
		VerificationTimeoutMS:      5000,
		VerificationPollIntervalMS: 100,
	}
}

func basePrompt() store.Prompt {
	return store.Prompt{
		Summary: store.Summary{
			ID:    "demo",
			Title: "Demo",
			Path:  "/prompts/demo.md",
		},
		Body: "hello world",
	}
}

func baseTarget() tmux.TargetContext {
	return tmux.TargetContext{
		PaneID:    "%0",
		Session:   "$1",
		ClientTTY: "/dev/pts/2",
	}
}

func TestSubmit_PromptHappyPath(t *testing.T) {
	prompt := basePrompt()
	fs := &fakeStore{prompts: map[string]store.Prompt{"demo": prompt}}
	dc := &fakeDaemonClient{resp: daemon.SubmitResponse{Accepted: true, JobID: "job-1"}}
	cfg := baseCfg()
	target := baseTarget()

	s := New(fs, dc, cfg, target)
	if err := s.Submit(tui.Result{Action: tui.ActionPrompt, PromptID: "demo"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if dc.calls != 1 {
		t.Fatalf("Submit calls = %d, want 1", dc.calls)
	}
	job := dc.lastReq.Job
	if job.Source != daemon.SourcePrompt {
		t.Errorf("Source = %q, want %q", job.Source, daemon.SourcePrompt)
	}
	if job.PromptID != "demo" {
		t.Errorf("PromptID = %q, want demo", job.PromptID)
	}
	if job.SourcePath != "/prompts/demo.md" {
		t.Errorf("SourcePath = %q, want /prompts/demo.md", job.SourcePath)
	}
	if string(job.Body) != "hello world" {
		t.Errorf("Body = %q, want hello world", string(job.Body))
	}
	if job.Mode != "paste" {
		t.Errorf("Mode = %q, want paste", job.Mode)
	}
	if job.Enter {
		t.Error("Enter = true, want false")
	}
	if job.SanitizeMode != "safe" {
		t.Errorf("SanitizeMode = %q, want safe", job.SanitizeMode)
	}
	if job.PaneID != "%0" {
		t.Errorf("PaneID = %q, want %%0", job.PaneID)
	}
	if job.Origin == nil {
		t.Fatal("Origin = nil, want populated")
	}
	if job.Origin.Session != "$1" || job.Origin.ClientTTY != "/dev/pts/2" {
		t.Errorf("Origin = %+v, want session=$1 clientTTY=/dev/pts/2", *job.Origin)
	}
	if job.Verification.TimeoutMS != 5000 || job.Verification.PollIntervalMS != 100 {
		t.Errorf("Verification = %+v, want {5000, 100}", job.Verification)
	}
	if job.SubmitterPID != os.Getpid() {
		t.Errorf("SubmitterPID = %d, want %d", job.SubmitterPID, os.Getpid())
	}
}

func TestSubmit_FrontmatterOverridesDelivery(t *testing.T) {
	enter := true
	prompt := basePrompt()
	prompt.Defaults = store.DeliveryDefaults{Mode: "type", Enter: &enter}
	fs := &fakeStore{prompts: map[string]store.Prompt{"demo": prompt}}
	dc := &fakeDaemonClient{resp: daemon.SubmitResponse{Accepted: true}}

	s := New(fs, dc, baseCfg(), baseTarget())
	if err := s.Submit(tui.Result{Action: tui.ActionPrompt, PromptID: "demo"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if got := dc.lastReq.Job.Mode; got != "type" {
		t.Errorf("Mode = %q, want type (frontmatter override)", got)
	}
	if !dc.lastReq.Job.Enter {
		t.Error("Enter = false, want true (frontmatter override)")
	}
}

func TestSubmit_BodyAtLimitSucceeds(t *testing.T) {
	cfg := baseCfg()
	cfg.MaxPasteBytes = 5
	prompt := basePrompt()
	prompt.Body = "12345" // exactly at limit
	fs := &fakeStore{prompts: map[string]store.Prompt{"demo": prompt}}
	dc := &fakeDaemonClient{resp: daemon.SubmitResponse{Accepted: true}}

	s := New(fs, dc, cfg, baseTarget())
	if err := s.Submit(tui.Result{Action: tui.ActionPrompt, PromptID: "demo"}); err != nil {
		t.Fatalf("body at limit should submit, got %v", err)
	}
	if dc.calls != 1 {
		t.Fatalf("daemon calls = %d, want 1", dc.calls)
	}
}

func TestSubmit_BodyOverLimitReturnsTypedError(t *testing.T) {
	cfg := baseCfg()
	cfg.MaxPasteBytes = 5
	prompt := basePrompt()
	prompt.Body = "123456" // 1 over limit
	fs := &fakeStore{prompts: map[string]store.Prompt{"demo": prompt}}
	dc := &fakeDaemonClient{}

	s := New(fs, dc, cfg, baseTarget())
	err := s.Submit(tui.Result{Action: tui.ActionPrompt, PromptID: "demo"})
	var tooLarge *BodyTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("want *BodyTooLargeError, got %T: %v", err, err)
	}
	if tooLarge.Bytes != 6 || tooLarge.Limit != 5 {
		t.Errorf("error fields = {%d, %d}, want {6, 5}", tooLarge.Bytes, tooLarge.Limit)
	}
	if dc.calls != 0 {
		t.Errorf("daemon should not be called on oversize, got %d calls", dc.calls)
	}
}

func TestSubmit_DaemonRejectsReturnsIPCError(t *testing.T) {
	fs := &fakeStore{prompts: map[string]store.Prompt{"demo": basePrompt()}}
	dc := &fakeDaemonClient{resp: daemon.SubmitResponse{Accepted: false, JobID: "rejected"}}

	s := New(fs, dc, baseCfg(), baseTarget())
	err := s.Submit(tui.Result{Action: tui.ActionPrompt, PromptID: "demo"})
	var ipc *daemon.IPCError
	if !errors.As(err, &ipc) {
		t.Fatalf("want *daemon.IPCError, got %T: %v", err, err)
	}
}

func TestSubmit_DaemonDialFailurePropagates(t *testing.T) {
	dialErr := &daemon.SocketUnavailableError{Path: "/tmp/x.sock", Reason: "connection refused"}
	fs := &fakeStore{prompts: map[string]store.Prompt{"demo": basePrompt()}}
	dc := &fakeDaemonClient{err: dialErr}

	s := New(fs, dc, baseCfg(), baseTarget())
	err := s.Submit(tui.Result{Action: tui.ActionPrompt, PromptID: "demo"})
	var su *daemon.SocketUnavailableError
	if !errors.As(err, &su) {
		t.Fatalf("want *SocketUnavailableError, got %T: %v", err, err)
	}
}

func TestSubmit_PromptNotFoundPropagates(t *testing.T) {
	fs := &fakeStore{prompts: map[string]store.Prompt{}}
	dc := &fakeDaemonClient{}

	s := New(fs, dc, baseCfg(), baseTarget())
	err := s.Submit(tui.Result{Action: tui.ActionPrompt, PromptID: "missing"})
	var nf *store.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want *store.NotFoundError, got %T: %v", err, err)
	}
	if dc.calls != 0 {
		t.Errorf("daemon should not be dialed when prompt is missing, got %d calls", dc.calls)
	}
}

func TestSubmit_ActionCancelIsNoop(t *testing.T) {
	fs := &fakeStore{prompts: map[string]store.Prompt{"demo": basePrompt()}}
	dc := &fakeDaemonClient{}

	s := New(fs, dc, baseCfg(), baseTarget())
	if err := s.Submit(tui.Result{Action: tui.ActionCancel}); err != nil {
		t.Fatalf("ActionCancel: want nil, got %v", err)
	}
	if dc.calls != 0 {
		t.Errorf("daemon should not be dialed on cancel, got %d calls", dc.calls)
	}
}

func TestSubmit_ClipboardHappyPath(t *testing.T) {
	fs := &fakeStore{} // must never be touched on clipboard path
	dc := &fakeDaemonClient{resp: daemon.SubmitResponse{Accepted: true, JobID: "job-c1"}}

	s := New(fs, dc, baseCfg(), baseTarget())
	err := s.Submit(tui.Result{Action: tui.ActionClipboard, ClipboardBody: []byte("hello clip")})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if dc.calls != 1 {
		t.Fatalf("daemon calls = %d, want 1", dc.calls)
	}
	job := dc.lastReq.Job
	if job.Source != daemon.SourceClipboard {
		t.Errorf("Source = %q, want %q", job.Source, daemon.SourceClipboard)
	}
	if job.PromptID != "" {
		t.Errorf("PromptID = %q, want empty for clipboard", job.PromptID)
	}
	if job.SourcePath != "" {
		t.Errorf("SourcePath = %q, want empty for clipboard", job.SourcePath)
	}
	if string(job.Body) != "hello clip" {
		t.Errorf("Body = %q, want %q", string(job.Body), "hello clip")
	}
	if job.Mode != "paste" {
		t.Errorf("Mode = %q, want paste (config default)", job.Mode)
	}
	if job.Enter {
		t.Error("Enter = true, want false (config default)")
	}
	if job.SanitizeMode != "safe" {
		t.Errorf("SanitizeMode = %q, want safe (config default)", job.SanitizeMode)
	}
	if job.PaneID != "%0" {
		t.Errorf("PaneID = %q, want %%0", job.PaneID)
	}
	if job.Origin == nil || job.Origin.Session != "$1" || job.Origin.ClientTTY != "/dev/pts/2" {
		t.Errorf("Origin = %+v, want session=$1 clientTTY=/dev/pts/2", job.Origin)
	}
	if job.Verification.TimeoutMS != 5000 || job.Verification.PollIntervalMS != 100 {
		t.Errorf("Verification = %+v, want {5000, 100}", job.Verification)
	}
	if job.SubmitterPID != os.Getpid() {
		t.Errorf("SubmitterPID = %d, want %d", job.SubmitterPID, os.Getpid())
	}
}

func TestSubmit_ClipboardSkipsStoreResolution(t *testing.T) {
	// If the clipboard path ever calls Resolve, this poisoned fakeStore will
	// return an error instead of a prompt, surfacing the accidental coupling.
	fs := &fakeStore{err: errors.New("store must not be called on ActionClipboard")}
	dc := &fakeDaemonClient{resp: daemon.SubmitResponse{Accepted: true}}

	s := New(fs, dc, baseCfg(), baseTarget())
	if err := s.Submit(tui.Result{Action: tui.ActionClipboard, ClipboardBody: []byte("x")}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
}

func TestSubmit_ClipboardEmptyReturnsTypedError(t *testing.T) {
	dc := &fakeDaemonClient{}
	s := New(&fakeStore{}, dc, baseCfg(), baseTarget())
	err := s.Submit(tui.Result{Action: tui.ActionClipboard, ClipboardBody: nil})
	var empty *clipboard.EmptyClipboardError
	if !errors.As(err, &empty) {
		t.Fatalf("want *clipboard.EmptyClipboardError, got %T: %v", err, err)
	}
	if dc.calls != 0 {
		t.Errorf("daemon should not be dialed on empty clipboard, got %d calls", dc.calls)
	}
}

func TestSubmit_ClipboardInvalidUTF8ReturnsTypedError(t *testing.T) {
	dc := &fakeDaemonClient{}
	s := New(&fakeStore{}, dc, baseCfg(), baseTarget())
	err := s.Submit(tui.Result{Action: tui.ActionClipboard, ClipboardBody: []byte{0xff, 0xfe}})
	var badUTF8 *clipboard.InvalidUTF8Error
	if !errors.As(err, &badUTF8) {
		t.Fatalf("want *clipboard.InvalidUTF8Error, got %T: %v", err, err)
	}
	if dc.calls != 0 {
		t.Errorf("daemon should not be dialed on non-UTF-8 clipboard, got %d calls", dc.calls)
	}
}

func TestSubmit_ClipboardOversizeReturnsBodyTooLargeError(t *testing.T) {
	cfg := baseCfg()
	cfg.MaxPasteBytes = 5
	dc := &fakeDaemonClient{}
	s := New(&fakeStore{}, dc, cfg, baseTarget())

	err := s.Submit(tui.Result{Action: tui.ActionClipboard, ClipboardBody: []byte("123456")})
	var tooLarge *BodyTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("want *BodyTooLargeError, got %T: %v", err, err)
	}
	if tooLarge.Bytes != 6 || tooLarge.Limit != 5 {
		t.Errorf("error fields = {%d, %d}, want {6, 5}", tooLarge.Bytes, tooLarge.Limit)
	}
	if dc.calls != 0 {
		t.Errorf("daemon should not be dialed on oversize clipboard, got %d calls", dc.calls)
	}
}

func TestSubmit_ClipboardDaemonRejectsReturnsIPCError(t *testing.T) {
	dc := &fakeDaemonClient{resp: daemon.SubmitResponse{Accepted: false, JobID: "rejected"}}
	s := New(&fakeStore{}, dc, baseCfg(), baseTarget())

	err := s.Submit(tui.Result{Action: tui.ActionClipboard, ClipboardBody: []byte("hi")})
	var ipc *daemon.IPCError
	if !errors.As(err, &ipc) {
		t.Fatalf("want *daemon.IPCError, got %T: %v", err, err)
	}
}

func TestSubmit_ClipboardDaemonDialFailurePropagates(t *testing.T) {
	dialErr := &daemon.SocketUnavailableError{Path: "/tmp/x.sock", Reason: "connection refused"}
	dc := &fakeDaemonClient{err: dialErr}
	s := New(&fakeStore{}, dc, baseCfg(), baseTarget())

	err := s.Submit(tui.Result{Action: tui.ActionClipboard, ClipboardBody: []byte("hi")})
	var su *daemon.SocketUnavailableError
	if !errors.As(err, &su) {
		t.Fatalf("want *SocketUnavailableError, got %T: %v", err, err)
	}
}

func TestBuildOrigin_NilWhenAllEmpty(t *testing.T) {
	if got := buildOrigin(tmux.TargetContext{PaneID: "%0"}); got != nil {
		t.Errorf("buildOrigin = %+v, want nil when only PaneID is set", got)
	}
}

func TestBuildOrigin_PopulatedWhenAnyMetadataSet(t *testing.T) {
	cases := []struct {
		name   string
		target tmux.TargetContext
	}{
		{"session only", tmux.TargetContext{PaneID: "%0", Session: "$1"}},
		{"window only", tmux.TargetContext{PaneID: "%0", Window: "@2"}},
		{"client tty only", tmux.TargetContext{PaneID: "%0", ClientTTY: "/dev/pts/3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if buildOrigin(tc.target) == nil {
				t.Errorf("buildOrigin should be non-nil for %+v", tc.target)
			}
		})
	}
}
