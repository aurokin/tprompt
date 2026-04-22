package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hsadler/tprompt/internal/tmux"
)

// scriptedAdapter returns adapter results from queues so tests can sequence
// pane-exists / pane-selected outcomes per iteration. Calls past the end of
// the queue panic so tests catch unexpected polling.
type scriptedAdapter struct {
	mu             sync.Mutex
	existsResults  []existsResult
	selectedResult []selectedResult
	existsCalls    int
	selectedCalls  int
	existsHook     func(context.Context, string) (bool, error)
	selectedHook   func(context.Context, tmux.TargetContext) (bool, error)
}

type existsResult struct {
	exists bool
	err    error
}

type selectedResult struct {
	selected bool
	err      error
}

func (s *scriptedAdapter) PaneExists(ctx context.Context, paneID string) (bool, error) {
	if s.existsHook != nil {
		return s.existsHook(ctx, paneID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.existsCalls >= len(s.existsResults) {
		panic("scriptedAdapter: PaneExists called more times than scripted")
	}
	r := s.existsResults[s.existsCalls]
	s.existsCalls++
	return r.exists, r.err
}

func (s *scriptedAdapter) IsTargetSelected(ctx context.Context, target tmux.TargetContext) (bool, error) {
	if s.selectedHook != nil {
		return s.selectedHook(ctx, target)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selectedCalls >= len(s.selectedResult) {
		panic("scriptedAdapter: IsTargetSelected called more times than scripted")
	}
	r := s.selectedResult[s.selectedCalls]
	s.selectedCalls++
	return r.selected, r.err
}

func (s *scriptedAdapter) CurrentContext() (tmux.TargetContext, error) {
	return tmux.TargetContext{}, nil
}

func (s *scriptedAdapter) CapturePaneTail(string, int) (string, error) { return "", nil }

func (s *scriptedAdapter) Paste(context.Context, tmux.TargetContext, string, bool) error {
	return nil
}

func (s *scriptedAdapter) Type(context.Context, tmux.TargetContext, string, bool) error {
	return nil
}

func (s *scriptedAdapter) DisplayMessage(tmux.MessageTarget, string) error { return nil }

func target() tmux.TargetContext { return tmux.TargetContext{PaneID: "%5"} }

func TestVerifyReadyImmediately(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	a := &scriptedAdapter{
		existsResults:  []existsResult{{exists: true}},
		selectedResult: []selectedResult{{selected: true}},
	}
	policy := VerificationPolicy{TimeoutMS: 100, PollIntervalMS: 10}
	if err := Verify(context.Background(), a, target(), policy, 0); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if a.existsCalls != 1 || a.selectedCalls != 1 {
		t.Fatalf("expected 1 of each call, got exists=%d selected=%d", a.existsCalls, a.selectedCalls)
	}
}

func TestVerifyReadyAfterPolls(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	a := &scriptedAdapter{
		existsResults: []existsResult{
			{exists: true}, {exists: true}, {exists: true},
		},
		selectedResult: []selectedResult{
			{selected: false}, {selected: false}, {selected: true},
		},
	}
	policy := VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 1}
	if err := Verify(context.Background(), a, target(), policy, 0); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if a.selectedCalls != 3 {
		t.Fatalf("expected 3 select polls, got %d", a.selectedCalls)
	}
}

func TestVerifyTimeout(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	a := &scriptedAdapter{
		// Enough scripted calls that polling won't panic before timeout.
		existsResults:  repeatExists(20, true),
		selectedResult: repeatSelected(20, false),
	}
	policy := VerificationPolicy{TimeoutMS: 20, PollIntervalMS: 5}
	err := Verify(context.Background(), a, target(), policy, 0)
	var te *TimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("want TimeoutError, got %T: %v", err, err)
	}
	if te.TimeoutMS != 20 {
		t.Fatalf("TimeoutMS = %d, want 20", te.TimeoutMS)
	}
}

func TestVerifyTimeoutDoesNotOversleepPollInterval(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	a := &scriptedAdapter{
		existsResults:  repeatExists(4, true),
		selectedResult: repeatSelected(4, false),
	}
	policy := VerificationPolicy{TimeoutMS: 20, PollIntervalMS: 1000}
	start := time.Now()
	err := Verify(context.Background(), a, target(), policy, 0)
	elapsed := time.Since(start)

	var te *TimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("want TimeoutError, got %T: %v", err, err)
	}
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("Verify overslept timeout: elapsed=%v timeout=%dms", elapsed, policy.TimeoutMS)
	}
}

func TestVerifyTimeoutDoesNotReuseStaleRemainingAfterSlowProbe(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	sleepOrDeadline := func(ctx context.Context, d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}

	a := &scriptedAdapter{
		existsHook: func(ctx context.Context, _ string) (bool, error) {
			if err := sleepOrDeadline(ctx, 20*time.Millisecond); err != nil {
				return false, err
			}
			return true, nil
		},
		selectedHook: func(ctx context.Context, _ tmux.TargetContext) (bool, error) {
			return false, nil
		},
	}
	policy := VerificationPolicy{TimeoutMS: 30, PollIntervalMS: 30}
	start := time.Now()
	err := Verify(context.Background(), a, target(), policy, 0)
	elapsed := time.Since(start)

	var te *TimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("want TimeoutError, got %T: %v", err, err)
	}
	if elapsed >= 45*time.Millisecond {
		t.Fatalf("Verify reused stale remaining after slow probe: elapsed=%v timeout=%dms", elapsed, policy.TimeoutMS)
	}
}

func TestVerifyPaneVanishesMidLoop(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	a := &scriptedAdapter{
		existsResults: []existsResult{
			{exists: true},
			{exists: false}, // vanished
		},
		selectedResult: []selectedResult{
			{selected: false},
		},
	}
	policy := VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 1}
	err := Verify(context.Background(), a, target(), policy, 0)
	var pm *tmux.PaneMissingError
	if !errors.As(err, &pm) {
		t.Fatalf("want PaneMissingError, got %T: %v", err, err)
	}
	if pm.PaneID != "%5" {
		t.Fatalf("PaneID = %q, want %%5", pm.PaneID)
	}
}

func TestVerifyPaneMissingFromStart(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	a := &scriptedAdapter{
		existsResults:  []existsResult{{exists: false}},
		selectedResult: []selectedResult{},
	}
	policy := VerificationPolicy{TimeoutMS: 100, PollIntervalMS: 10}
	err := Verify(context.Background(), a, target(), policy, 0)
	var pm *tmux.PaneMissingError
	if !errors.As(err, &pm) {
		t.Fatalf("want PaneMissingError, got %T: %v", err, err)
	}
}

func TestVerifyAdapterErrorPropagates(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	envErr := &tmux.EnvError{Reason: "tmux server gone"}
	a := &scriptedAdapter{
		existsResults:  []existsResult{{err: envErr}},
		selectedResult: []selectedResult{},
	}
	policy := VerificationPolicy{TimeoutMS: 100, PollIntervalMS: 10}
	err := Verify(context.Background(), a, target(), policy, 0)
	var ee *tmux.EnvError
	if !errors.As(err, &ee) {
		t.Fatalf("want EnvError, got %T: %v", err, err)
	}
}

func TestVerifyContextCancellation(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	a := &scriptedAdapter{
		existsResults:  repeatExists(20, true),
		selectedResult: repeatSelected(20, false),
	}
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	policy := VerificationPolicy{TimeoutMS: 5000, PollIntervalMS: 5}
	err := Verify(ctx, a, target(), policy, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestVerifyCancelsBlockedPaneExistsProbe(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	probeStarted := make(chan struct{})
	a := &scriptedAdapter{
		existsHook: func(ctx context.Context, _ string) (bool, error) {
			close(probeStarted)
			<-ctx.Done()
			return false, ctx.Err()
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Verify(ctx, a, target(), VerificationPolicy{TimeoutMS: 5000, PollIntervalMS: 50}, 0)
	}()

	<-probeStarted
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Verify did not cancel promptly while PaneExists was blocked")
	}
	if a.selectedCalls != 0 {
		t.Fatalf("selected calls = %d, want 0", a.selectedCalls)
	}
}

func TestVerifyTimesOutBlockedPaneExistsProbe(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	probeStarted := make(chan struct{})
	a := &scriptedAdapter{
		existsHook: func(ctx context.Context, _ string) (bool, error) {
			close(probeStarted)
			<-ctx.Done()
			return false, ctx.Err()
		},
	}
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- Verify(context.Background(), a, target(), VerificationPolicy{TimeoutMS: 20, PollIntervalMS: 5}, 0)
	}()

	<-probeStarted

	select {
	case err := <-done:
		var te *TimeoutError
		if !errors.As(err, &te) {
			t.Fatalf("want TimeoutError, got %T: %v", err, err)
		}
		var envErr *tmux.EnvError
		if errors.As(err, &envErr) {
			t.Fatalf("want probe timeout, got EnvError: %v", err)
		}
		if elapsed := time.Since(start); elapsed >= 200*time.Millisecond {
			t.Fatalf("Verify timed out too slowly: %v", elapsed)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Verify did not time out promptly while PaneExists was blocked")
	}
}

func TestVerifyCancelsBlockedSelectionProbe(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	probeStarted := make(chan struct{})
	a := &scriptedAdapter{
		existsResults: []existsResult{{exists: true}},
		selectedHook: func(ctx context.Context, _ tmux.TargetContext) (bool, error) {
			close(probeStarted)
			<-ctx.Done()
			return false, ctx.Err()
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Verify(ctx, a, target(), VerificationPolicy{TimeoutMS: 5000, PollIntervalMS: 50}, 0)
	}()

	<-probeStarted
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Verify did not cancel promptly while IsTargetSelected was blocked")
	}
	if a.existsCalls != 1 {
		t.Fatalf("exists calls = %d, want 1", a.existsCalls)
	}
}

func TestVerifyTimesOutBlockedSelectionProbe(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	probeStarted := make(chan struct{})
	a := &scriptedAdapter{
		existsResults: []existsResult{{exists: true}},
		selectedHook: func(ctx context.Context, _ tmux.TargetContext) (bool, error) {
			close(probeStarted)
			<-ctx.Done()
			return false, ctx.Err()
		},
	}
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- Verify(context.Background(), a, target(), VerificationPolicy{TimeoutMS: 20, PollIntervalMS: 5}, 0)
	}()

	<-probeStarted

	select {
	case err := <-done:
		var te *TimeoutError
		if !errors.As(err, &te) {
			t.Fatalf("want TimeoutError, got %T: %v", err, err)
		}
		var envErr *tmux.EnvError
		if errors.As(err, &envErr) {
			t.Fatalf("want probe timeout, got EnvError: %v", err)
		}
		if elapsed := time.Since(start); elapsed >= 200*time.Millisecond {
			t.Fatalf("Verify timed out too slowly: %v", elapsed)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Verify did not time out promptly while IsTargetSelected was blocked")
	}
}

func TestVerifyRejectsInvalidPolicy(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()

	tests := []struct {
		name  string
		p     VerificationPolicy
		field string
	}{
		{"zero timeout", VerificationPolicy{TimeoutMS: 0, PollIntervalMS: 10}, "timeout_ms"},
		{"negative timeout", VerificationPolicy{TimeoutMS: -1, PollIntervalMS: 10}, "timeout_ms"},
		{"zero interval", VerificationPolicy{TimeoutMS: 100, PollIntervalMS: 0}, "poll_interval_ms"},
		{"negative interval", VerificationPolicy{TimeoutMS: 100, PollIntervalMS: -5}, "poll_interval_ms"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &scriptedAdapter{} // no scripted calls — must not reach adapter
			err := Verify(context.Background(), a, target(), tc.p, 0)
			var ipe *InvalidPolicyError
			if !errors.As(err, &ipe) {
				t.Fatalf("want InvalidPolicyError, got %T: %v", err, err)
			}
			if ipe.Field != tc.field {
				t.Fatalf("Field = %q, want %q", ipe.Field, tc.field)
			}
			if a.existsCalls != 0 || a.selectedCalls != 0 {
				t.Fatalf("adapter must not be called with bad policy, got exists=%d selected=%d",
					a.existsCalls, a.selectedCalls)
			}
		})
	}
}

func TestVerifyWaitsForSubmitterExitBeforeReady(t *testing.T) {
	restore := stubProcessRunning(t, []bool{true, false})
	defer restore()

	a := &scriptedAdapter{
		existsResults:  []existsResult{{exists: true}, {exists: true}},
		selectedResult: []selectedResult{{selected: true}},
	}
	policy := VerificationPolicy{TimeoutMS: 1000, PollIntervalMS: 1}
	if err := Verify(context.Background(), a, target(), policy, 4242); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if a.existsCalls != 2 {
		t.Fatalf("exists calls = %d, want 2", a.existsCalls)
	}
	if a.selectedCalls != 1 {
		t.Fatalf("selected calls = %d, want 1", a.selectedCalls)
	}
}

func TestVerifySubmitterCheckErrorPropagates(t *testing.T) {
	restore := stubProcessRunning(t, nil)
	defer restore()
	processRunning = func(int) (bool, error) {
		return false, errors.New("permission denied")
	}

	a := &scriptedAdapter{
		existsResults:  []existsResult{{exists: true}},
		selectedResult: []selectedResult{},
	}
	policy := VerificationPolicy{TimeoutMS: 100, PollIntervalMS: 1}
	err := Verify(context.Background(), a, target(), policy, 4242)
	if err == nil || err.Error() != "check submitter pid 4242: permission denied" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func repeatExists(n int, val bool) []existsResult {
	out := make([]existsResult, n)
	for i := range out {
		out[i] = existsResult{exists: val}
	}
	return out
}

func repeatSelected(n int, val bool) []selectedResult {
	out := make([]selectedResult, n)
	for i := range out {
		out[i] = selectedResult{selected: val}
	}
	return out
}

func stubProcessRunning(t *testing.T, scripted []bool) func() {
	t.Helper()
	prev := processRunning
	var mu sync.Mutex
	calls := 0
	processRunning = func(int) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if scripted == nil {
			return false, nil
		}
		if calls >= len(scripted) {
			t.Fatalf("processRunning called %d times, scripted %d", calls+1, len(scripted))
		}
		alive := scripted[calls]
		calls++
		return alive, nil
	}
	return func() {
		processRunning = prev
	}
}
