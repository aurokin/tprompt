package tmux

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls []fakeCall
	// errOn keys "op" → error to return on that op's first match.
	errOn map[string]error
	// stdoutOn keys "op" → stdout to return.
	stdoutOn map[string][]byte
}

type fakeCall struct {
	Argv  []string
	Stdin []byte
	Ctx   context.Context
}

type scriptedRunner struct {
	calls   []fakeCall
	resultQ []scriptedResult
}

type scriptedResult struct {
	stdout []byte
	err    error
}

func (f *fakeRunner) Run(ctx context.Context, argv []string, stdin []byte) ([]byte, error) {
	f.calls = append(f.calls, fakeCall{
		Argv:  append([]string(nil), argv...),
		Stdin: append([]byte(nil), stdin...),
		Ctx:   ctx,
	})
	op := argv[0]
	if err, ok := f.errOn[op]; ok && err != nil {
		// Consume the error so subsequent calls to same op succeed (simple one-shot).
		delete(f.errOn, op)
		return f.stdoutOn[op], &runnerError{Err: err, Message: err.Error()}
	}
	return f.stdoutOn[op], nil
}

func (s *scriptedRunner) Run(ctx context.Context, argv []string, stdin []byte) ([]byte, error) {
	s.calls = append(s.calls, fakeCall{
		Argv:  append([]string(nil), argv...),
		Stdin: append([]byte(nil), stdin...),
		Ctx:   ctx,
	})
	if len(s.resultQ) == 0 {
		return nil, nil
	}
	next := s.resultQ[0]
	s.resultQ = s.resultQ[1:]
	if next.err != nil {
		return next.stdout, &runnerError{Err: next.err, Message: next.err.Error()}
	}
	return next.stdout, nil
}

func newTestExec(runner Runner) *Exec {
	return &Exec{
		runner:    runner,
		chunkSize: DefaultChunkSize,
		timeout:   30 * time.Second,
		now:       func() time.Time { return time.Unix(0, 1700000000000000000) },
		pid:       4242,
	}
}

func TestExec_Paste_CommandConstruction(t *testing.T) {
	fr := &fakeRunner{}
	e := newTestExec(fr)

	err := e.Paste(context.Background(), TargetContext{PaneID: "%7"}, "hello\nworld", false)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("want 2 calls (load-buffer, paste-buffer), got %d", len(fr.calls))
	}

	lb := fr.calls[0]
	if lb.Argv[0] != "load-buffer" {
		t.Fatalf("call[0] should be load-buffer, got %v", lb.Argv)
	}
	if !hasArg(lb.Argv, "-b") {
		t.Fatalf("load-buffer missing -b flag: %v", lb.Argv)
	}
	bufName := argAfter(lb.Argv, "-b")
	if !strings.HasPrefix(bufName, "tprompt-send-4242-") {
		t.Fatalf("buffer name missing pid prefix, got %q", bufName)
	}
	if lb.Argv[len(lb.Argv)-1] != "-" {
		t.Fatalf("load-buffer should end with '-' (stdin marker), got %v", lb.Argv)
	}
	if string(lb.Stdin) != "hello\nworld" {
		t.Fatalf("load-buffer stdin mismatch: %q", lb.Stdin)
	}

	pb := fr.calls[1]
	if pb.Argv[0] != "paste-buffer" {
		t.Fatalf("call[1] should be paste-buffer, got %v", pb.Argv)
	}
	if !hasArg(pb.Argv, "-d") || !hasArg(pb.Argv, "-p") {
		t.Fatalf("paste-buffer missing -d/-p: %v", pb.Argv)
	}
	if argAfter(pb.Argv, "-b") != bufName {
		t.Fatalf("paste-buffer buffer name must match load-buffer: got %q want %q", argAfter(pb.Argv, "-b"), bufName)
	}
	if argAfter(pb.Argv, "-t") != "%7" {
		t.Fatalf("paste-buffer target must be %%7, got %q", argAfter(pb.Argv, "-t"))
	}
}

func TestExec_Paste_EnterOutsideWrapper(t *testing.T) {
	fr := &fakeRunner{}
	e := newTestExec(fr)

	if err := e.Paste(context.Background(), TargetContext{PaneID: "%7"}, "hi", true); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fr.calls) != 3 {
		t.Fatalf("want 3 calls (load-buffer, paste-buffer, send-keys Enter), got %d", len(fr.calls))
	}
	last := fr.calls[2].Argv
	if last[0] != "send-keys" {
		t.Fatalf("call[2] should be send-keys, got %v", last)
	}
	if last[len(last)-1] != "Enter" {
		t.Fatalf("send-keys should end with Enter, got %v", last)
	}
	// Enter is not piggy-backed on the paste.
	if string(fr.calls[0].Stdin) != "hi" {
		t.Fatalf("load-buffer stdin must be the raw body, got %q", fr.calls[0].Stdin)
	}
}

func TestExec_Type_ChunksAndFlags(t *testing.T) {
	fr := &fakeRunner{}
	e := newTestExec(fr)
	e.chunkSize = 5

	if err := e.Type(context.Background(), TargetContext{PaneID: "%2"}, "ABCDEFGHIJ", false); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("want 2 chunks for 10 bytes at chunkSize=5, got %d", len(fr.calls))
	}
	for i, c := range fr.calls {
		if c.Argv[0] != "send-keys" {
			t.Fatalf("call[%d] should be send-keys, got %v", i, c.Argv)
		}
		if !hasArg(c.Argv, "-l") {
			t.Fatalf("call[%d] missing -l flag: %v", i, c.Argv)
		}
		if !hasArg(c.Argv, "--") {
			t.Fatalf("call[%d] missing -- separator: %v", i, c.Argv)
		}
		if argAfter(c.Argv, "-t") != "%2" {
			t.Fatalf("call[%d] target mismatch: %v", i, c.Argv)
		}
	}
	if last := fr.calls[len(fr.calls)-1].Argv; last[len(last)-1] != "FGHIJ" {
		t.Fatalf("last chunk mismatch, got %v", last)
	}
}

func TestExec_Type_PressEnter(t *testing.T) {
	fr := &fakeRunner{}
	e := newTestExec(fr)
	if err := e.Type(context.Background(), TargetContext{PaneID: "%2"}, "hi", true); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("want 2 calls (send-keys -l, send-keys Enter), got %d", len(fr.calls))
	}
	last := fr.calls[1].Argv
	if last[len(last)-1] != "Enter" {
		t.Fatalf("last call should be send-keys Enter, got %v", last)
	}
}

func TestExec_PaneExists(t *testing.T) {
	fr := &fakeRunner{stdoutOn: map[string][]byte{"display-message": []byte("%3\n")}}
	e := newTestExec(fr)
	ok, err := e.PaneExists(context.Background(), "%3")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !ok {
		t.Fatal("want pane to exist")
	}
	got := fr.calls[0].Argv
	if got[0] != "display-message" || argAfter(got, "-t") != "%3" {
		t.Fatalf("unexpected argv: %v", got)
	}
}

func TestExec_PaneExistsMissing(t *testing.T) {
	fr := &fakeRunner{errOn: map[string]error{"display-message": errors.New("can't find pane: %99")}}
	e := newTestExec(fr)
	ok, err := e.PaneExists(context.Background(), "%99")
	if err != nil {
		t.Fatalf("missing pane should not error out, got %v", err)
	}
	if ok {
		t.Fatal("want pane to be reported as absent")
	}
}

func TestExec_PaneExistsUnexpectedError(t *testing.T) {
	// Runner failures that aren't "can't find pane" must surface as EnvError
	// so callers can distinguish a missing pane from a broken tmux.
	fr := &fakeRunner{errOn: map[string]error{"display-message": errors.New("no server running")}}
	e := newTestExec(fr)
	ok, err := e.PaneExists(context.Background(), "%1")
	if ok {
		t.Fatal("want false on error")
	}
	var envErr *EnvError
	if !errors.As(err, &envErr) {
		t.Fatalf("want EnvError, got %T: %v", err, err)
	}
}

func TestExec_PaneExistsPreservesCancellation(t *testing.T) {
	fr := &fakeRunner{errOn: map[string]error{"display-message": context.Canceled}}
	e := newTestExec(fr)

	ok, err := e.PaneExists(context.Background(), "%1")
	if ok {
		t.Fatal("want false on cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %T: %v", err, err)
	}
}

func TestExec_PaneExistsWrapsAdapterTimeoutAsEnvError(t *testing.T) {
	fr := &fakeRunner{errOn: map[string]error{"display-message": context.DeadlineExceeded}}
	e := newTestExec(fr)

	ok, err := e.PaneExists(context.Background(), "%1")
	if ok {
		t.Fatal("want false on timeout")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("adapter timeout should not escape as raw deadline: %v", err)
	}
	var envErr *EnvError
	if !errors.As(err, &envErr) {
		t.Fatalf("want EnvError, got %T: %v", err, err)
	}
}

func TestExec_PaneExistsPreservesCallerDeadline(t *testing.T) {
	fr := &fakeRunner{errOn: map[string]error{"display-message": context.DeadlineExceeded}}
	e := newTestExec(fr)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	ok, err := e.PaneExists(ctx, "%1")
	if ok {
		t.Fatal("want false on caller deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %T: %v", err, err)
	}
}

func TestExec_PaneExistsEmptyStdout(t *testing.T) {
	// Some tmux versions exit 0 with empty stdout for a bogus -t.
	fr := &fakeRunner{stdoutOn: map[string][]byte{"display-message": []byte("\n")}}
	e := newTestExec(fr)
	ok, err := e.PaneExists(context.Background(), "%99")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if ok {
		t.Fatal("empty stdout must be treated as absent")
	}
}

func TestExec_CurrentContext(t *testing.T) {
	fr := &fakeRunner{stdoutOn: map[string][]byte{
		"display-message": []byte("$1|@2|%3|/dev/pts/7\n"),
	}}
	e := newTestExec(fr)
	tc, err := e.CurrentContext()
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if tc.Session != "$1" || tc.Window != "@2" || tc.PaneID != "%3" || tc.ClientTTY != "/dev/pts/7" {
		t.Fatalf("unexpected context: %+v", tc)
	}
}

func TestExec_CurrentContextEnvError(t *testing.T) {
	fr := &fakeRunner{errOn: map[string]error{"display-message": errors.New("no server running")}}
	e := newTestExec(fr)
	_, err := e.CurrentContext()
	var envErr *EnvError
	if !errors.As(err, &envErr) {
		t.Fatalf("want EnvError, got %T: %v", err, err)
	}
}

func TestExec_IsTargetSelectedUsesOriginatingClientContext(t *testing.T) {
	fr := &fakeRunner{stdoutOn: map[string][]byte{
		"display-message": []byte("$1|@2|%3\n"),
	}}
	e := newTestExec(fr)

	ok, err := e.IsTargetSelected(context.Background(), TargetContext{
		Session:   "$1",
		Window:    "@2",
		PaneID:    "%3",
		ClientTTY: "/dev/pts/7",
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !ok {
		t.Fatal("want target to be selected for originating client")
	}

	got := fr.calls[0].Argv
	if got[0] != "display-message" || argAfter(got, "-c") != "/dev/pts/7" {
		t.Fatalf("expected client-scoped display-message, got %v", got)
	}
	if hasArg(got, "-t") {
		t.Fatalf("client-scoped readiness check should not target the pane directly: %v", got)
	}
}

func TestExec_IsTargetSelectedFallsBackWhenOriginatingClientDisappears(t *testing.T) {
	fr := &fakeRunner{
		errOn: map[string]error{
			"display-message": errors.New("can't find client: /dev/pts/7"),
		},
		stdoutOn: map[string][]byte{
			"display-message": []byte("$1|@2|%3|1|1\n"),
		},
	}
	e := newTestExec(fr)

	ok, err := e.IsTargetSelected(context.Background(), TargetContext{
		Session:   "$1",
		Window:    "@2",
		PaneID:    "%3",
		ClientTTY: "/dev/pts/7",
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !ok {
		t.Fatal("foreground pane should be treated as selected after client fallback")
	}
	if len(fr.calls) != 2 {
		t.Fatalf("want 2 display-message calls, got %d", len(fr.calls))
	}
	if got := fr.calls[0].Argv; argAfter(got, "-c") != "/dev/pts/7" {
		t.Fatalf("first call should be client-scoped, got %v", got)
	}
	if got := fr.calls[1].Argv; argAfter(got, "-t") != "%3" {
		t.Fatalf("second call should fall back to pane target, got %v", got)
	}
}

func TestExec_IsTargetSelectedPreservesCancellationFromClientProbe(t *testing.T) {
	fr := &fakeRunner{
		errOn: map[string]error{
			"display-message": context.Canceled,
		},
	}
	e := newTestExec(fr)

	ok, err := e.IsTargetSelected(context.Background(), TargetContext{
		Session:   "$1",
		Window:    "@2",
		PaneID:    "%3",
		ClientTTY: "/dev/pts/7",
	})
	if ok {
		t.Fatal("want false on cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %T: %v", err, err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("want only the client probe call, got %d calls", len(fr.calls))
	}
}

func TestExec_IsTargetSelectedWrapsClientProbeTimeoutAsEnvError(t *testing.T) {
	fr := &fakeRunner{
		errOn: map[string]error{
			"display-message": context.DeadlineExceeded,
		},
	}
	e := newTestExec(fr)

	ok, err := e.IsTargetSelected(context.Background(), TargetContext{
		Session:   "$1",
		Window:    "@2",
		PaneID:    "%3",
		ClientTTY: "/dev/pts/7",
	})
	if ok {
		t.Fatal("want false on timeout")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("adapter timeout should not escape as raw deadline: %v", err)
	}
	var envErr *EnvError
	if !errors.As(err, &envErr) {
		t.Fatalf("want EnvError, got %T: %v", err, err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("want only the client probe call, got %d calls", len(fr.calls))
	}
}

func TestExec_IsTargetSelectedRejectsBackgroundWindow(t *testing.T) {
	fr := &fakeRunner{stdoutOn: map[string][]byte{
		"display-message": []byte("$1|@2|%3|1|0\n"),
	}}
	e := newTestExec(fr)

	ok, err := e.IsTargetSelected(context.Background(), TargetContext{Session: "$1", Window: "@2", PaneID: "%3"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if ok {
		t.Fatal("background window must not be treated as selected")
	}

	got := fr.calls[0].Argv
	if got[0] != "display-message" || argAfter(got, "-t") != "%3" {
		t.Fatalf("expected pane-scoped fallback check, got %v", got)
	}
}

func TestExec_IsTargetSelectedFallbackAcceptsForegroundPane(t *testing.T) {
	fr := &fakeRunner{stdoutOn: map[string][]byte{
		"display-message": []byte("$1|@2|%3|1|1\n"),
	}}
	e := newTestExec(fr)

	ok, err := e.IsTargetSelected(context.Background(), TargetContext{Session: "$1", Window: "@2", PaneID: "%3"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !ok {
		t.Fatal("foreground pane should be treated as selected")
	}
}

func TestExec_IsTargetSelectedPreservesCancellationFromPaneProbe(t *testing.T) {
	fr := &fakeRunner{
		errOn: map[string]error{
			"display-message": context.Canceled,
		},
	}
	e := newTestExec(fr)

	ok, err := e.IsTargetSelected(context.Background(), TargetContext{PaneID: "%3"})
	if ok {
		t.Fatal("want false on cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %T: %v", err, err)
	}
}

func TestExec_IsTargetSelectedWrapsPaneProbeTimeoutAsEnvError(t *testing.T) {
	fr := &fakeRunner{
		errOn: map[string]error{
			"display-message": context.DeadlineExceeded,
		},
	}
	e := newTestExec(fr)

	ok, err := e.IsTargetSelected(context.Background(), TargetContext{PaneID: "%3"})
	if ok {
		t.Fatal("want false on timeout")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("adapter timeout should not escape as raw deadline: %v", err)
	}
	var envErr *EnvError
	if !errors.As(err, &envErr) {
		t.Fatalf("want EnvError, got %T: %v", err, err)
	}
}

func TestExec_IsTargetSelectedReportsPaneMissingFromPaneProbe(t *testing.T) {
	fr := &fakeRunner{
		errOn: map[string]error{
			"display-message": errors.New("can't find pane: %3"),
		},
	}
	e := newTestExec(fr)

	ok, err := e.IsTargetSelected(context.Background(), TargetContext{PaneID: "%3"})
	if ok {
		t.Fatal("want false when pane is missing")
	}
	var pm *PaneMissingError
	if !errors.As(err, &pm) {
		t.Fatalf("want PaneMissingError, got %T: %v", err, err)
	}
	if pm.PaneID != "%3" {
		t.Fatalf("want pane id %%3, got %q", pm.PaneID)
	}
}

func TestExec_IsTargetSelectedFallbackReportsPaneMissingWhenPaneDisappears(t *testing.T) {
	sr := &scriptedRunner{
		resultQ: []scriptedResult{
			{err: errors.New("can't find client: /dev/pts/7")},
			{err: errors.New("can't find pane: %3")},
		},
	}
	e := newTestExec(sr)

	ok, err := e.IsTargetSelected(context.Background(), TargetContext{
		Session:   "$1",
		Window:    "@2",
		PaneID:    "%3",
		ClientTTY: "/dev/pts/7",
	})
	if ok {
		t.Fatal("want false when pane disappears during fallback probe")
	}
	var pm *PaneMissingError
	if !errors.As(err, &pm) {
		t.Fatalf("want PaneMissingError, got %T: %v", err, err)
	}
	if pm.PaneID != "%3" {
		t.Fatalf("want pane id %%3, got %q", pm.PaneID)
	}
	if len(sr.calls) != 2 {
		t.Fatalf("want 2 calls, got %d", len(sr.calls))
	}
}

func TestExec_Paste_LoadBufferFailure(t *testing.T) {
	fr := &fakeRunner{errOn: map[string]error{"load-buffer": errors.New("boom")}}
	e := newTestExec(fr)
	err := e.Paste(context.Background(), TargetContext{PaneID: "%1"}, "x", false)
	var de *DeliveryError
	if !errors.As(err, &de) {
		t.Fatalf("want DeliveryError, got %T: %v", err, err)
	}
	if de.Op != "load-buffer" {
		t.Fatalf("want op=load-buffer, got %q", de.Op)
	}
}

func TestExec_Paste_PasteBufferFailureCleansUpBuffer(t *testing.T) {
	fr := &fakeRunner{errOn: map[string]error{"paste-buffer": errors.New("boom")}}
	e := newTestExec(fr)

	err := e.Paste(context.Background(), TargetContext{PaneID: "%1"}, "x", false)
	var de *DeliveryError
	if !errors.As(err, &de) || de.Op != "paste-buffer" {
		t.Fatalf("want paste-buffer DeliveryError, got %T: %v", err, err)
	}
	if len(fr.calls) != 3 {
		t.Fatalf("want 3 calls (load, paste-fail, delete-cleanup), got %d", len(fr.calls))
	}
	if fr.calls[2].Argv[0] != "delete-buffer" {
		t.Fatalf("call[2] should be delete-buffer, got %v", fr.calls[2].Argv)
	}
	bufName := argAfter(fr.calls[0].Argv, "-b")
	if got := argAfter(fr.calls[2].Argv, "-b"); got != bufName {
		t.Fatalf("delete-buffer should target same buffer: got %q want %q", got, bufName)
	}
}

func TestExec_DisplayMessage_ClientScope(t *testing.T) {
	fr := &fakeRunner{}
	e := newTestExec(fr)
	target := MessageTarget{PaneID: "%7", ClientTTY: "/dev/pts/3"}
	if err := e.DisplayMessage(target, "hello"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	got := fr.calls[0].Argv
	if argAfter(got, "-c") != "/dev/pts/3" {
		t.Fatalf("want -c /dev/pts/3, got %v", got)
	}
	if got[len(got)-1] != "hello" {
		t.Fatalf("message must be last arg, got %v", got)
	}
}

func TestExec_DisplayMessageFallsBackWhenOriginatingClientDisappears(t *testing.T) {
	fr := &fakeRunner{
		errOn: map[string]error{
			"display-message": errors.New("can't find client: /dev/pts/3"),
		},
	}
	e := newTestExec(fr)
	target := MessageTarget{PaneID: "%7", ClientTTY: "/dev/pts/3"}
	if err := e.DisplayMessage(target, "hello"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("want 2 display-message calls, got %d", len(fr.calls))
	}
	if got := fr.calls[0].Argv; argAfter(got, "-c") != "/dev/pts/3" {
		t.Fatalf("first call should target originating client, got %v", got)
	}
	if got := fr.calls[1].Argv; argAfter(got, "-t") != "%7" || hasArg(got, "-c") {
		t.Fatalf("second call should fall back to pane target, got %v", got)
	}
}

func TestExec_DisplayMessageFallsBackToWindowWhenClientDisappearsAndPaneOmitted(t *testing.T) {
	fr := &fakeRunner{
		errOn: map[string]error{
			"display-message": errors.New("can't find client: /dev/pts/3"),
		},
	}
	e := newTestExec(fr)
	target := MessageTarget{Session: "$1", Window: "@2", ClientTTY: "/dev/pts/3"}
	if err := e.DisplayMessage(target, "hello"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("want 2 display-message calls, got %d", len(fr.calls))
	}
	if got := fr.calls[1].Argv; argAfter(got, "-t") != "@2" || hasArg(got, "-c") {
		t.Fatalf("second call should fall back to window target, got %v", got)
	}
}

func TestExec_DisplayMessageFallsBackToUnscopedWhenClientDisappearsAndNoOtherTargetExists(t *testing.T) {
	fr := &fakeRunner{
		errOn: map[string]error{
			"display-message": errors.New("can't find client: /dev/pts/3"),
		},
	}
	e := newTestExec(fr)
	target := MessageTarget{ClientTTY: "/dev/pts/3"}
	if err := e.DisplayMessage(target, "hello"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("want 2 display-message calls, got %d", len(fr.calls))
	}
	if got := fr.calls[1].Argv; hasArg(got, "-t") || hasArg(got, "-c") {
		t.Fatalf("second call should be unscoped, got %v", got)
	}
}

func TestExec_DisplayMessage_Unscoped(t *testing.T) {
	fr := &fakeRunner{}
	e := newTestExec(fr)
	if err := e.DisplayMessage(MessageTarget{}, "hello"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	got := fr.calls[0].Argv
	if hasArg(got, "-c") {
		t.Fatalf("unscoped display-message should not have -c: %v", got)
	}
}

func TestExec_DisplayMessage_TargetPaneFallback(t *testing.T) {
	fr := &fakeRunner{}
	e := newTestExec(fr)
	target := MessageTarget{PaneID: "%7"}
	if err := e.DisplayMessage(target, "hello"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	got := fr.calls[0].Argv
	if argAfter(got, "-t") != "%7" {
		t.Fatalf("want -t %%7, got %v", got)
	}
	if hasArg(got, "-c") {
		t.Fatalf("pane-targeted display-message should not have -c: %v", got)
	}
}

func TestExec_AppliesTimeoutToRunnerCalls(t *testing.T) {
	fr := &fakeRunner{}
	e := newTestExec(fr)
	e.timeout = 250 * time.Millisecond

	if err := e.Paste(context.Background(), TargetContext{PaneID: "%1"}, "x", true); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(fr.calls) == 0 {
		t.Fatal("expected runner calls")
	}
	for i, c := range fr.calls {
		if c.Ctx == nil {
			t.Fatalf("call[%d] has nil ctx", i)
		}
		dl, ok := c.Ctx.Deadline()
		if !ok {
			t.Fatalf("call[%d] ctx has no deadline", i)
		}
		if time.Until(dl) > 500*time.Millisecond {
			t.Fatalf("call[%d] deadline too far out: %v", i, time.Until(dl))
		}
	}
}

func hasArg(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}

func argAfter(argv []string, key string) string {
	for i, a := range argv {
		if a == key && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}
