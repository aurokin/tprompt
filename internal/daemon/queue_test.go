package daemon

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hsadler/tprompt/internal/tmux"
)

// fakeAdapter captures DisplayMessage calls. Other methods are zero-value
// satisfiers so the queue tests don't depend on adapter behavior.
type fakeAdapter struct {
	mu       sync.Mutex
	messages []displayCall
}

type displayCall struct {
	Target  tmux.MessageTarget
	Message string
}

func (f *fakeAdapter) CurrentContext() (tmux.TargetContext, error) {
	return tmux.TargetContext{}, nil
}
func (f *fakeAdapter) PaneExists(context.Context, string) (bool, error) { return true, nil }
func (f *fakeAdapter) IsTargetSelected(context.Context, tmux.TargetContext) (bool, error) {
	return true, nil
}
func (f *fakeAdapter) CapturePaneTail(string, int) (string, error) { return "", nil }
func (f *fakeAdapter) Paste(context.Context, tmux.TargetContext, string, bool) error {
	return nil
}

func (f *fakeAdapter) Type(context.Context, tmux.TargetContext, string, bool) error {
	return nil
}

func (f *fakeAdapter) DisplayMessage(target tmux.MessageTarget, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, displayCall{Target: target, Message: message})
	return nil
}

func (f *fakeAdapter) snapshot() []displayCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]displayCall, len(f.messages))
	copy(out, f.messages)
	return out
}

func (f *fakeAdapter) waitMessages(t *testing.T, want int) []displayCall {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		msgs := f.snapshot()
		if len(msgs) >= want {
			return msgs
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d banners; got %d: %+v", want, len(msgs), msgs)
		}
		time.Sleep(time.Millisecond)
	}
}

type blockingWriter struct {
	once    sync.Once
	started chan struct{}
	gate    chan struct{}
}

func newBlockingWriter() *blockingWriter {
	return &blockingWriter{
		started: make(chan struct{}),
		gate:    make(chan struct{}),
	}
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.gate
	return len(p), nil
}

func (w *blockingWriter) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-w.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for replacement side effect to start")
	}
}

func (w *blockingWriter) release() {
	close(w.gate)
}

type unstoppableRunner struct {
	started  chan string
	release  chan struct{}
	finished chan finishedJob
}

func newUnstoppableRunner() *unstoppableRunner {
	return &unstoppableRunner{
		started:  make(chan string, 4),
		release:  make(chan struct{}),
		finished: make(chan finishedJob, 4),
	}
}

func (r *unstoppableRunner) run(_ context.Context, job Job) bool {
	r.started <- job.JobID
	<-r.release
	r.finished <- finishedJob{JobID: job.JobID, Cancelled: false}
	return true
}

func (r *unstoppableRunner) waitStarted(t *testing.T, jobID string) {
	t.Helper()
	select {
	case started := <-r.started:
		if started != jobID {
			t.Fatalf("unexpected job start %q while waiting for %q", started, jobID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s to start", jobID)
	}
}

func (r *unstoppableRunner) waitFinished(t *testing.T, jobID string) finishedJob {
	t.Helper()
	select {
	case finished := <-r.finished:
		if finished.JobID != jobID {
			t.Fatalf("unexpected job finish %+v while waiting for %q", finished, jobID)
		}
		return finished
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s to finish", jobID)
	}
	return finishedJob{}
}

func (r *unstoppableRunner) assertNotStarted(t *testing.T, jobID string) {
	t.Helper()
	select {
	case started := <-r.started:
		t.Fatalf("job %s should not have started yet; saw %s", jobID, started)
	default:
	}
}

func (r *unstoppableRunner) finishCurrent() {
	close(r.release)
}

// blockingRunner returns a JobRunner that blocks on a per-job channel until
// the test releases it (or the context is cancelled). It records started and
// finished job IDs in arrival order.
type blockingRunner struct {
	mu       sync.Mutex
	gates    map[string]chan struct{}
	started  []string
	finished []finishedJob
	cond     *sync.Cond
}

type finishedJob struct {
	JobID     string
	Cancelled bool
}

func newBlockingRunner() *blockingRunner {
	r := &blockingRunner{gates: make(map[string]chan struct{})}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *blockingRunner) gate(jobID string) chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.gates[jobID]
	if !ok {
		g = make(chan struct{})
		r.gates[jobID] = g
	}
	return g
}

func (r *blockingRunner) release(jobID string) {
	close(r.gate(jobID))
}

func (r *blockingRunner) run(ctx context.Context, job Job) bool {
	r.mu.Lock()
	r.started = append(r.started, job.JobID)
	r.cond.Broadcast()
	r.mu.Unlock()

	select {
	case <-r.gate(job.JobID):
		r.mu.Lock()
		r.finished = append(r.finished, finishedJob{JobID: job.JobID, Cancelled: false})
		r.cond.Broadcast()
		r.mu.Unlock()
		return true
	case <-ctx.Done():
		r.mu.Lock()
		r.finished = append(r.finished, finishedJob{JobID: job.JobID, Cancelled: true})
		r.cond.Broadcast()
		r.mu.Unlock()
		return false
	}
}

func (r *blockingRunner) waitStarted(t *testing.T, jobID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	r.mu.Lock()
	defer r.mu.Unlock()
	for !contains(r.started, jobID) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s to start; started=%v", jobID, r.started)
		}
		// brief unlock+sleep so the runner goroutine can publish
		r.mu.Unlock()
		time.Sleep(time.Millisecond)
		r.mu.Lock()
	}
}

func (r *blockingRunner) waitFinished(t *testing.T, jobID string) finishedJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		for _, f := range r.finished {
			if f.JobID == jobID {
				return f
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s to finish; finished=%v", jobID, r.finished)
		}
		r.mu.Unlock()
		time.Sleep(time.Millisecond)
		r.mu.Lock()
	}
}

func (r *blockingRunner) assertNotStarted(t *testing.T, jobID string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if contains(r.started, jobID) {
		t.Fatalf("job %s should not have started yet; started=%v", jobID, r.started)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func makeJob(id, pane, clientTTY string) Job {
	return Job{
		JobID:    id,
		Source:   SourcePrompt,
		PromptID: "code-review",
		Body:     []byte("body"),
		Mode:     "paste",
		PaneID:   pane,
		Origin:   &tmux.OriginContext{ClientTTY: clientTTY},
	}
}

func TestQueueEnqueueRunsJob(t *testing.T) {
	adapter := &fakeAdapter{}
	var logBuf bytes.Buffer
	logger := NewLoggerWriter(&logBuf)
	runner := newBlockingRunner()
	q := NewQueue(adapter, logger, runner.run)

	resp := q.Enqueue(makeJob("j-1", "%5", "/dev/pts/0"))

	if !resp.Accepted || resp.JobID != "j-1" || resp.ReplacedJobID != "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	runner.waitStarted(t, "j-1")
	if got := q.Pending(); got != 1 {
		t.Fatalf("Pending = %d, want 1", got)
	}
	runner.release("j-1")
	q.Wait()
	if got := q.Pending(); got != 0 {
		t.Fatalf("Pending after Wait = %d, want 0", got)
	}
}

func TestQueueReplaceSameTargetCancelsOldAndBanners(t *testing.T) {
	adapter := &fakeAdapter{}
	var logBuf bytes.Buffer
	logger := NewLoggerWriter(&logBuf)
	runner := newBlockingRunner()
	q := NewQueue(adapter, logger, runner.run)

	first := makeJob("j-1", "%5", "/dev/pts/0")
	second := makeJob("j-2", "%5", "/dev/pts/0")

	q.Enqueue(first)
	runner.waitStarted(t, "j-1")

	resp := q.Enqueue(second)
	if resp.ReplacedJobID != "" {
		t.Fatalf("ReplacedJobID = %q, want empty until cancellation wins", resp.ReplacedJobID)
	}
	runner.assertNotStarted(t, "j-2")

	finished := runner.waitFinished(t, "j-1")
	if !finished.Cancelled {
		t.Fatal("first job should have been cancelled by replacement")
	}
	runner.waitStarted(t, "j-2")

	msgs := adapter.waitMessages(t, 1)
	if msgs[0].Message != ReplacedBanner {
		t.Fatalf("banner = %q, want %q", msgs[0].Message, ReplacedBanner)
	}
	if msgs[0].Target.ClientTTY != "/dev/pts/0" {
		t.Fatalf("banner client_tty = %q, want /dev/pts/0", msgs[0].Target.ClientTTY)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "outcome=replaced") {
		t.Fatalf("expected replaced log entry, got: %q", logged)
	}
	if !strings.Contains(logged, `msg="replaced by job j-2"`) {
		t.Fatalf("expected replaced-by-j-2 message, got: %q", logged)
	}
	if !strings.Contains(logged, "job_id=j-1") {
		t.Fatalf("replaced log should reference the displaced job, got: %q", logged)
	}
	if !strings.Contains(logged, "prompt_id=code-review") {
		t.Fatalf("replaced log should include prompt_id for prompt jobs, got: %q", logged)
	}

	runner.release("j-2")
	q.Wait()
}

func TestQueueStartsReplacementBeforeActiveReplacementSideEffects(t *testing.T) {
	adapter := &fakeAdapter{}
	logWriter := newBlockingWriter()
	logger := NewLoggerWriter(logWriter)
	runner := newStubbornRunner()
	q := NewQueue(adapter, logger, runner.run)

	q.Enqueue(makeJob("j-1", "%5", "/dev/pts/0"))
	runner.waitStarted(t, "j-1")

	resp := q.Enqueue(makeJob("j-2", "%5", "/dev/pts/1"))
	if resp.ReplacedJobID != "" {
		t.Fatalf("ReplacedJobID = %q, want empty until cancellation wins", resp.ReplacedJobID)
	}
	runner.assertNotStarted(t, "j-2")

	runner.release("j-1")
	finished := runner.waitFinished(t, "j-1")
	if !finished.Cancelled {
		t.Fatal("first job should have been cancelled by replacement")
	}

	logWriter.waitStarted(t)
	runner.waitStarted(t, "j-2")

	logWriter.release()
	msgs := adapter.waitMessages(t, 1)
	if msgs[0].Message != ReplacedBanner {
		t.Fatalf("banner = %q, want %q", msgs[0].Message, ReplacedBanner)
	}

	runner.release("j-2")
	q.Wait()
}

func TestQueueReplacementAckReturnsBeforeDisplacedWorkerExits(t *testing.T) {
	adapter := &fakeAdapter{}
	logger := NewLoggerWriter(&bytes.Buffer{})
	runner := newStubbornRunner()
	q := NewQueue(adapter, logger, runner.run)

	first := makeJob("j-1", "%5", "/dev/pts/0")
	second := makeJob("j-2", "%5", "/dev/pts/0")

	q.Enqueue(first)
	runner.waitStarted(t, "j-1")

	respCh := make(chan SubmitResponse, 1)
	go func() {
		respCh <- q.Enqueue(second)
	}()

	select {
	case resp := <-respCh:
		if resp.ReplacedJobID != "" {
			t.Fatalf("ReplacedJobID = %q, want empty until cancellation wins", resp.ReplacedJobID)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("replacement enqueue should acknowledge immediately")
	}

	runner.assertNotStarted(t, "j-2")
	runner.release("j-1")
	runner.waitFinished(t, "j-1")
	runner.waitStarted(t, "j-2")
	runner.release("j-2")
	q.Wait()
}

func TestQueueQueuedReplacementAckDoesNotWaitForReplacementSideEffects(t *testing.T) {
	adapter := &fakeAdapter{}
	logWriter := newBlockingWriter()
	logger := NewLoggerWriter(logWriter)
	runner := newStubbornRunner()
	q := NewQueue(adapter, logger, runner.run)

	q.Enqueue(makeJob("j-1", "%5", "/dev/pts/0"))
	runner.waitStarted(t, "j-1")

	resp2 := q.Enqueue(makeJob("j-2", "%5", "/dev/pts/1"))
	if resp2.ReplacedJobID != "" {
		t.Fatalf("second response replaced %q, want empty until cancellation wins", resp2.ReplacedJobID)
	}

	respCh := make(chan SubmitResponse, 1)
	go func() {
		respCh <- q.Enqueue(makeJob("j-3", "%5", "/dev/pts/2"))
	}()

	select {
	case resp3 := <-respCh:
		if resp3.ReplacedJobID != "j-2" {
			t.Fatalf("third response replaced %q, want j-2", resp3.ReplacedJobID)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("queued replacement enqueue should acknowledge immediately")
	}

	logWriter.waitStarted(t)
	runner.assertNotStarted(t, "j-2")
	runner.assertNotStarted(t, "j-3")

	logWriter.release()
	runner.release("j-1")
	finished := runner.waitFinished(t, "j-1")
	if !finished.Cancelled {
		t.Fatal("first job should have been cancelled by replacement")
	}
	runner.waitStarted(t, "j-3")
	runner.release("j-3")
	q.Wait()
}

func TestQueueIgnoresFinishedStalePendingEntry(t *testing.T) {
	adapter := &fakeAdapter{}
	var logBuf bytes.Buffer
	logger := NewLoggerWriter(&logBuf)
	runner := newBlockingRunner()
	q := NewQueue(adapter, logger, runner.run)

	stale := &workerState{
		job:    makeJob("j-1", "%5", "/dev/pts/0"),
		cancel: func() {},
		done:   make(chan struct{}),
	}
	stale.completed.Store(true)
	close(stale.done)
	q.pending["%5"] = &paneState{active: stale}

	resp := q.Enqueue(makeJob("j-2", "%5", "/dev/pts/0"))
	if resp.ReplacedJobID != "" {
		t.Fatalf("ReplacedJobID = %q, want empty for finished stale entry", resp.ReplacedJobID)
	}

	runner.waitStarted(t, "j-2")
	if msgs := adapter.snapshot(); len(msgs) != 0 {
		t.Fatalf("expected no replaced banner, got: %+v", msgs)
	}
	if strings.Contains(logBuf.String(), "outcome=replaced") {
		t.Fatalf("stale finished entry must not log replacement, got: %q", logBuf.String())
	}

	runner.release("j-2")
	q.Wait()
}

func TestQueuePendingPreservesFinishedActiveUntilQueuedPromotion(t *testing.T) {
	adapter := &fakeAdapter{}
	logger := NewLoggerWriter(&bytes.Buffer{})
	runner := newBlockingRunner()
	q := NewQueue(adapter, logger, runner.run)

	activeCtx, activeCancel := context.WithCancel(context.Background())
	defer activeCancel()
	active := &workerState{
		job:    makeJob("j-1", "%5", "/dev/pts/0"),
		ctx:    activeCtx,
		cancel: activeCancel,
		done:   make(chan struct{}),
	}
	close(active.done)

	queuedCtx, queuedCancel := context.WithCancel(context.Background())
	queued := &workerState{
		job:    makeJob("j-2", "%5", "/dev/pts/1"),
		ctx:    queuedCtx,
		cancel: queuedCancel,
		done:   make(chan struct{}),
	}
	defer queuedCancel()

	q.pending["%5"] = &paneState{active: active, queued: queued}

	if got := q.Pending(); got != 2 {
		t.Fatalf("Pending = %d, want 2 before promotion", got)
	}

	q.mu.Lock()
	slot := q.pending["%5"]
	if slot.active != active {
		q.mu.Unlock()
		t.Fatalf("active worker was cleared before finishWorker promotion: %+v", slot)
	}
	q.mu.Unlock()

	next := q.finishWorker(active, active.completed.Load())
	if next != queued {
		t.Fatalf("finishWorker promoted %p, want queued state %p", next, queued)
	}

	q.startWorker(next)
	runner.waitStarted(t, "j-2")
	runner.release("j-2")
	q.Wait()

	if got := q.Pending(); got != 0 {
		t.Fatalf("Pending after queued promotion = %d, want 0", got)
	}
}

func TestQueueSuccessfulActiveCompletionDropsQueuedReplacement(t *testing.T) {
	adapter := &fakeAdapter{}
	logger := NewLoggerWriter(&bytes.Buffer{})
	q := NewQueue(adapter, logger, nil)

	activeCtx, activeCancel := context.WithCancel(context.Background())
	defer activeCancel()
	active := &workerState{
		job:    makeJob("j-1", "%5", "/dev/pts/0"),
		ctx:    activeCtx,
		cancel: activeCancel,
		done:   make(chan struct{}),
	}
	active.completed.Store(true)
	close(active.done)

	queuedCtx, queuedCancel := context.WithCancel(context.Background())
	queued := &workerState{
		job:    makeJob("j-2", "%5", "/dev/pts/1"),
		ctx:    queuedCtx,
		cancel: queuedCancel,
		done:   make(chan struct{}),
	}
	defer queuedCancel()

	q.pending["%5"] = &paneState{active: active, queued: queued}

	next := q.finishWorker(active, active.completed.Load())
	if next != nil {
		t.Fatalf("finishWorker returned %p, want nil after successful delivery", next)
	}
	if got := q.Pending(); got != 0 {
		t.Fatalf("Pending = %d, want 0 after dropping queued replacement", got)
	}
	select {
	case <-queued.done:
	default:
		t.Fatal("queued replacement done channel was not closed")
	}
	if err := queued.ctx.Err(); err == nil {
		t.Fatal("queued replacement was not cancelled")
	}
}

func TestQueueCoalescesRapidSamePaneReplacements(t *testing.T) {
	adapter := &fakeAdapter{}
	var logBuf bytes.Buffer
	logger := NewLoggerWriter(&logBuf)
	runner := newStubbornRunner()
	q := NewQueue(adapter, logger, runner.run)

	q.Enqueue(makeJob("j-1", "%5", "/dev/pts/0"))
	runner.waitStarted(t, "j-1")

	resp2 := q.Enqueue(makeJob("j-2", "%5", "/dev/pts/1"))
	if resp2.ReplacedJobID != "" {
		t.Fatalf("second response replaced %q, want empty until cancellation wins", resp2.ReplacedJobID)
	}

	resp3 := q.Enqueue(makeJob("j-3", "%5", "/dev/pts/2"))
	if resp3.ReplacedJobID != "j-2" {
		t.Fatalf("third response replaced %q, want j-2", resp3.ReplacedJobID)
	}

	runner.assertNotStarted(t, "j-2")
	runner.assertNotStarted(t, "j-3")

	msgs := adapter.waitMessages(t, 1)
	if msgs[0].Target.ClientTTY != "/dev/pts/1" {
		t.Fatalf("queued replacement banner client_tty = %q, want /dev/pts/1", msgs[0].Target.ClientTTY)
	}

	runner.release("j-1")
	finished := runner.waitFinished(t, "j-1")
	if !finished.Cancelled {
		t.Fatal("first job should have been cancelled by replacement")
	}
	runner.waitStarted(t, "j-3")
	runner.assertNotStarted(t, "j-2")

	msgs = adapter.waitMessages(t, 2)
	if msgs[1].Target.ClientTTY != "/dev/pts/0" {
		t.Fatalf("active replacement banner client_tty = %q, want /dev/pts/0", msgs[1].Target.ClientTTY)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, `job_id=j-1`) || !strings.Contains(logged, `msg="replaced by job j-3"`) {
		t.Fatalf("expected active job to log replacement by j-3, got: %q", logged)
	}
	if !strings.Contains(logged, `job_id=j-2`) {
		t.Fatalf("expected queued replacement log for j-2, got: %q", logged)
	}

	runner.release("j-3")
	q.Wait()
}

func TestQueueDifferentPanesRunConcurrently(t *testing.T) {
	adapter := &fakeAdapter{}
	logger := NewLoggerWriter(&bytes.Buffer{})
	runner := newBlockingRunner()
	q := NewQueue(adapter, logger, runner.run)

	q.Enqueue(makeJob("j-A", "%1", ""))
	q.Enqueue(makeJob("j-B", "%2", ""))

	runner.waitStarted(t, "j-A")
	runner.waitStarted(t, "j-B")
	if got := q.Pending(); got != 2 {
		t.Fatalf("Pending = %d, want 2", got)
	}

	// No banner should fire — different panes are independent.
	if msgs := adapter.snapshot(); len(msgs) != 0 {
		t.Fatalf("expected no banner, got: %+v", msgs)
	}

	runner.release("j-A")
	runner.release("j-B")
	q.Wait()
}

func TestQueueClearsPendingAfterCompletion(t *testing.T) {
	adapter := &fakeAdapter{}
	logger := NewLoggerWriter(&bytes.Buffer{})
	runner := newBlockingRunner()
	q := NewQueue(adapter, logger, runner.run)

	q.Enqueue(makeJob("j-1", "%5", ""))
	runner.waitStarted(t, "j-1")
	runner.release("j-1")
	q.Wait()

	if got := q.Pending(); got != 0 {
		t.Fatalf("Pending = %d, want 0", got)
	}
}

func TestQueueCancelAllInterruptsWorkers(t *testing.T) {
	adapter := &fakeAdapter{}
	logger := NewLoggerWriter(&bytes.Buffer{})
	runner := newBlockingRunner()
	q := NewQueue(adapter, logger, runner.run)

	q.Enqueue(makeJob("j-1", "%1", ""))
	q.Enqueue(makeJob("j-2", "%2", ""))
	runner.waitStarted(t, "j-1")
	runner.waitStarted(t, "j-2")

	q.CancelAll()
	q.Wait()

	for _, id := range []string{"j-1", "j-2"} {
		f := runner.waitFinished(t, id)
		if !f.Cancelled {
			t.Fatalf("job %s should have been cancelled", id)
		}
	}
}

func TestQueueCancelAllDropsQueuedReplacementWithoutBanner(t *testing.T) {
	adapter := &fakeAdapter{}
	var logBuf bytes.Buffer
	logger := NewLoggerWriter(&logBuf)
	runner := newBlockingRunner()
	q := NewQueue(adapter, logger, runner.run)

	resp1 := q.Enqueue(makeJob("j-1", "%5", "/dev/pts/0"))
	if resp1.ReplacedJobID != "" {
		t.Fatalf("initial enqueue replaced %q, want none", resp1.ReplacedJobID)
	}
	runner.waitStarted(t, "j-1")

	resp2 := q.Enqueue(makeJob("j-2", "%5", "/dev/pts/1"))
	if resp2.ReplacedJobID != "" {
		t.Fatalf("replacement enqueue replaced %q, want empty until cancellation wins", resp2.ReplacedJobID)
	}
	runner.assertNotStarted(t, "j-2")

	q.CancelAll()
	q.Wait()

	finished := runner.waitFinished(t, "j-1")
	if !finished.Cancelled {
		t.Fatal("active job should be cancelled during shutdown")
	}
	runner.assertNotStarted(t, "j-2")
	if msgs := adapter.snapshot(); len(msgs) != 0 {
		t.Fatalf("shutdown should not surface replacement banners, got %+v", msgs)
	}
	if strings.Contains(logBuf.String(), "outcome=replaced") {
		t.Fatalf("shutdown should not log replacement, got: %q", logBuf.String())
	}
	if got := q.Pending(); got != 0 {
		t.Fatalf("Pending = %d, want 0", got)
	}
}

func TestQueueDropsQueuedReplacementWhenActiveWorkerStillDelivers(t *testing.T) {
	adapter := &fakeAdapter{}
	var logBuf bytes.Buffer
	logger := NewLoggerWriter(&logBuf)
	runner := newUnstoppableRunner()
	q := NewQueue(adapter, logger, runner.run)

	q.Enqueue(makeJob("j-1", "%5", "/dev/pts/0"))
	runner.waitStarted(t, "j-1")

	resp := q.Enqueue(makeJob("j-2", "%5", "/dev/pts/1"))
	if resp.ReplacedJobID != "" {
		t.Fatalf("ReplacedJobID = %q, want empty while active delivery can still succeed", resp.ReplacedJobID)
	}
	runner.assertNotStarted(t, "j-2")

	runner.finishCurrent()
	finished := runner.waitFinished(t, "j-1")
	if finished.Cancelled {
		t.Fatal("first job should still deliver successfully")
	}

	q.Wait()
	runner.assertNotStarted(t, "j-2")
	if got := q.Pending(); got != 0 {
		t.Fatalf("Pending = %d, want 0 after dropping queued replacement", got)
	}

	if msgs := adapter.snapshot(); len(msgs) != 0 {
		t.Fatalf("successful active delivery must not emit replacement banner, got %+v", msgs)
	}
	if strings.Contains(logBuf.String(), "outcome=replaced") {
		t.Fatalf("successful active delivery must not log replacement, got %q", logBuf.String())
	}
}

type stubbornRunner struct {
	mu       sync.Mutex
	started  chan string
	gates    map[string]chan struct{}
	finished chan finishedJob
}

func newStubbornRunner() *stubbornRunner {
	return &stubbornRunner{
		started:  make(chan string, 8),
		gates:    make(map[string]chan struct{}),
		finished: make(chan finishedJob, 8),
	}
}

func (r *stubbornRunner) gate(jobID string) chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gates[jobID]; ok {
		return g
	}
	g := make(chan struct{})
	r.gates[jobID] = g
	return g
}

func (r *stubbornRunner) run(ctx context.Context, job Job) bool {
	r.started <- job.JobID
	<-r.gate(job.JobID)
	cancelled := ctx.Err() != nil
	r.finished <- finishedJob{JobID: job.JobID, Cancelled: cancelled}
	return !cancelled
}

func (r *stubbornRunner) release(jobID string) {
	close(r.gate(jobID))
}

func (r *stubbornRunner) waitStarted(t *testing.T, jobID string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case started := <-r.started:
			if started == jobID {
				return
			}
			t.Fatalf("unexpected job start %q while waiting for %q", started, jobID)
		case <-deadline:
			t.Fatalf("timed out waiting for %s to start", jobID)
		}
	}
}

func (r *stubbornRunner) waitFinished(t *testing.T, jobID string) finishedJob {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case finished := <-r.finished:
			if finished.JobID == jobID {
				return finished
			}
			t.Fatalf("unexpected job finish %+v while waiting for %q", finished, jobID)
		case <-deadline:
			t.Fatalf("timed out waiting for %s to finish", jobID)
		}
	}
}

func (r *stubbornRunner) assertNotStarted(t *testing.T, jobID string) {
	t.Helper()
	select {
	case started := <-r.started:
		t.Fatalf("job %s should not have started yet; saw %s", jobID, started)
	default:
	}
}
