package daemon

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hsadler/tprompt/internal/tmux"
)

// ReplacedBanner is the user-visible message surfaced via tmux display-message
// when a pending job is dropped because a newer job arrived for the same pane
// (DECISIONS.md §26, docs/commands/daemon.md "Error feedback").
const ReplacedBanner = "tprompt: replaced by a newer job — this delivery was dropped"

// JobRunner executes verification + sanitize + delivery for a single job and
// reports whether it reached a terminal outcome on its own. It returns false
// when the queue canceled the worker before completion, so Enqueue can avoid
// surfacing a replacement for a job that had already finished.
type JobRunner func(ctx context.Context, job Job) bool

// Queue holds at most one pending job per target pane and runs each job in
// its own goroutine. When a new job arrives for a pane that already has a
// pending job, the old job is cancelled, logged as replaced, and the
// originating client sees a banner.
type Queue struct {
	mu      sync.Mutex
	pending map[string]*paneState

	adapter tmux.Adapter
	logger  *Logger
	run     JobRunner

	wg sync.WaitGroup
}

type paneState struct {
	active *workerState
	queued *workerState
}

type workerState struct {
	job       Job
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	completed atomic.Bool

	mu         sync.Mutex
	replacedBy string
}

// NewQueue wires a queue with the dependencies it needs to surface
// replacement banners and run delivery.
func NewQueue(adapter tmux.Adapter, logger *Logger, run JobRunner) *Queue {
	return &Queue{
		pending: make(map[string]*paneState),
		adapter: adapter,
		logger:  logger,
		run:     run,
	}
}

// Enqueue registers a new job. If the pane already had a pending job, that
// job's context is cancelled before the new worker starts. ReplacedJobID names
// only a job known to have been dropped synchronously (currently, a queued
// job); active jobs are reported as replaced only after their worker exits due
// to cancellation. The returned SubmitResponse is what the IPC layer hands
// back to the client.
func (q *Queue) Enqueue(job Job) SubmitResponse {
	ctx, cancel := context.WithCancel(context.Background())
	state := &workerState{
		job:    job,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	resp, startNow, replaced := q.enqueueState(state)
	if startNow != nil {
		q.startWorker(startNow)
	}
	q.handleReplacedAsync(replaced)
	return resp
}

type replacementEvent struct {
	displaced      Job
	replacingJobID string
}

func (q *Queue) enqueueState(state *workerState) (SubmitResponse, *workerState, []replacementEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()

	paneID := state.job.PaneID
	slot, ok := q.pending[paneID]
	if !ok {
		slot = &paneState{}
		q.pending[paneID] = slot
	}
	q.normalizeLocked(paneID, slot)
	if _, ok := q.pending[paneID]; !ok {
		slot = &paneState{}
		q.pending[paneID] = slot
	}

	resp := SubmitResponse{Accepted: true, JobID: state.job.JobID}
	var replaced []replacementEvent
	switch {
	case slot.active == nil:
		if slot.queued != nil {
			resp.ReplacedJobID = slot.queued.job.JobID
			slot.queued.cancel()
			close(slot.queued.done)
			replaced = append(replaced, replacementEvent{
				displaced:      slot.queued.job,
				replacingJobID: state.job.JobID,
			})
			slot.queued = nil
		}
		slot.active = state
		return resp, state, replaced
	case slot.queued != nil:
		resp.ReplacedJobID = slot.queued.job.JobID
		slot.queued.cancel()
		close(slot.queued.done)
		replaced = append(replaced, replacementEvent{
			displaced:      slot.queued.job,
			replacingJobID: state.job.JobID,
		})
		slot.queued = state
		slot.active.setReplacedBy(state.job.JobID)
		return resp, nil, replaced
	default:
		slot.active.setReplacedBy(state.job.JobID)
		slot.active.cancel()
		slot.queued = state
		return resp, nil, replaced
	}
}

func (q *Queue) normalizeLocked(paneID string, slot *paneState) {
	if slot.active != nil && stateFinished(slot.active) && slot.queued == nil {
		slot.active = nil
	}
	if slot.queued != nil && stateFinished(slot.queued) {
		slot.queued = nil
	}
	if slot.active == nil && slot.queued == nil {
		delete(q.pending, paneID)
	}
}

func stateFinished(state *workerState) bool {
	if state.completed.Load() {
		return true
	}
	select {
	case <-state.done:
		return true
	default:
		return false
	}
}

func (w *workerState) setReplacedBy(jobID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.replacedBy = jobID
}

func (w *workerState) replacement() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.replacedBy
}

func (w *workerState) clearReplacement() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.replacedBy = ""
}

func (q *Queue) startWorker(state *workerState) {
	q.wg.Add(1)
	go q.runWorker(state)
}

// Pending reports the number of jobs currently in flight or queued. Used by
// StatusResponse.
func (q *Queue) Pending() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	total := 0
	for paneID, slot := range q.pending {
		q.normalizeLocked(paneID, slot)
		if _, ok := q.pending[paneID]; !ok {
			continue
		}
		if slot.active != nil {
			total++
		}
		if slot.queued != nil {
			total++
		}
	}
	return total
}

// Wait blocks until every worker has finished. Used during graceful daemon
// shutdown after the listener stops accepting new connections.
//
// Caller contract: no Enqueue may race with Wait when Pending() is zero —
// sync.WaitGroup requires a positive Add to happen-before Wait in that
// case. Run satisfies this by closing the listener (which stops accepting
// new connections, and hence new Enqueue calls) before invoking Wait.
// Callers outside Run must honor the same ordering.
func (q *Queue) Wait() {
	q.wg.Wait()
}

// CancelAll cancels every in-flight worker without waiting. Called during
// shutdown so workers exit promptly through their context.
func (q *Queue) CancelAll() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for paneID, slot := range q.pending {
		if slot.active != nil {
			slot.active.cancel()
		}
		if slot.queued != nil {
			if slot.active != nil {
				slot.active.clearReplacement()
			}
			slot.queued.cancel()
			closeStateDone(slot.queued)
			slot.queued = nil
		}
		if slot.active == nil {
			delete(q.pending, paneID)
		}
	}
}

func (q *Queue) runWorker(state *workerState) {
	defer state.cancel()
	defer q.wg.Done()
	defer close(state.done)
	if q.run(state.ctx, state.job) {
		state.completed.Store(true)
	}

	next := q.finishWorker(state, state.completed.Load())
	if next != nil {
		q.startWorker(next)
	}
	if replacingJobID := state.replacement(); !state.completed.Load() && replacingJobID != "" {
		q.handleReplaced(state.job, replacingJobID)
	}
}

func (q *Queue) finishWorker(state *workerState, delivered bool) *workerState {
	q.mu.Lock()
	defer q.mu.Unlock()

	paneID := state.job.PaneID
	slot, ok := q.pending[paneID]
	if !ok {
		return nil
	}
	if slot.active != state {
		q.normalizeLocked(paneID, slot)
		return nil
	}

	slot.active = nil
	if slot.queued != nil && stateFinished(slot.queued) {
		slot.queued = nil
	}
	if delivered && slot.queued != nil {
		// The active job already injected successfully, so the queued replacement
		// is stale and must not run afterward in the same pane.
		slot.queued.cancel()
		closeStateDone(slot.queued)
		slot.queued = nil
	}
	if slot.queued != nil {
		next := slot.queued
		slot.queued = nil
		slot.active = next
		return next
	}

	delete(q.pending, paneID)
	return nil
}

func (q *Queue) handleReplaced(displaced Job, replacingJobID string) {
	_ = q.logger.Log(Entry{
		JobID:   displaced.JobID,
		Pane:    displaced.PaneID,
		Source:  displaced.Source,
		Outcome: OutcomeReplaced,
		Msg:     fmt.Sprintf("replaced by job %s", replacingJobID),
	})
	_ = q.adapter.DisplayMessage(displaced.messageTarget(), ReplacedBanner)
}

func (q *Queue) handleReplacedAsync(replaced []replacementEvent) {
	if len(replaced) == 0 {
		return
	}
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		for _, replacement := range replaced {
			q.handleReplaced(replacement.displaced, replacement.replacingJobID)
		}
	}()
}

func closeStateDone(state *workerState) {
	select {
	case <-state.done:
	default:
		close(state.done)
	}
}
