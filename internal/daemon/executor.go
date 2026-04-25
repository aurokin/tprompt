package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/hsadler/tprompt/internal/sanitize"
	"github.com/hsadler/tprompt/internal/tmux"
)

// BannerPrefix is prepended to every error surfaced via tmux display-message
// from the daemon (docs/commands/daemon.md "Error feedback").
const BannerPrefix = "tprompt: "

const postInjectionCaptureLines = 20

// Executor runs verification, the pre-sanitize byte cap, the sanitizer, and
// the tmux adapter for a single Job. It is the JobRunner the queue calls on
// each enqueued worker. Failures surface via the logger and tmux display-
// message; success is silent (no log, no banner) per
// docs/commands/daemon.md.
type Executor struct {
	adapter                   tmux.Adapter
	logger                    *Logger
	maxPasteBytes             int64
	postInjectionVerification bool
}

var sanitizeProcess = func(mode string, body []byte) ([]byte, error) {
	return sanitize.New(sanitize.Mode(mode)).Process(body)
}

// NewExecutor wires an executor with the daemon-wide config it needs.
func NewExecutor(adapter tmux.Adapter, logger *Logger, maxPasteBytes int64) *Executor {
	return &Executor{
		adapter:       adapter,
		logger:        logger,
		maxPasteBytes: maxPasteBytes,
	}
}

// EnablePostInjectionVerification turns on warning-only capture-pane tail
// comparison after successful delivery. The check is diagnostic only: warnings
// do not change the job success result.
func (e *Executor) EnablePostInjectionVerification(enabled bool) {
	e.postInjectionVerification = enabled
}

// Run is the JobRunner entrypoint. Verification first; then the
// max_paste_bytes check (pre-sanitize, matching `runSend`); then the
// sanitizer; then the adapter call. Cancellation by the queue
// (replace-same-target) is silent here — the queue already logged and
// surfaced the banner.
func (e *Executor) Run(ctx context.Context, job Job) bool {
	if done, result := e.finishStep(job, Verify(ctx, e.adapter, job.verificationTarget(), job.Verification, job.SubmitterPID)); done {
		return result
	}
	if done, result := e.finishContext(ctx, job); done {
		return result
	}

	if err := e.checkPasteSize(job); err != nil {
		e.handleFailure(job, err)
		return true
	}

	cleaned, err := sanitizeProcess(job.SanitizeMode, job.Body)
	if done, result := e.finishStep(job, err); done {
		return result
	}
	if done, result := e.finishContext(ctx, job); done {
		return result
	}
	if done, result := e.finishStep(job, e.deliver(ctx, job, cleaned)); done {
		return result
	}
	return true
}

func (e *Executor) finishStep(job Job, err error) (bool, bool) {
	if err == nil {
		return false, false
	}
	if errors.Is(err, context.Canceled) {
		return true, false
	}
	e.handleFailure(job, err)
	return true, true
}

func (e *Executor) finishContext(ctx context.Context, job Job) (bool, bool) {
	shouldStop, err := canceled(ctx)
	if !shouldStop {
		return false, false
	}
	if err != nil {
		e.handleFailure(job, err)
		return true, true
	}
	return true, false
}

func (e *Executor) checkPasteSize(job Job) error {
	if e.maxPasteBytes <= 0 || int64(len(job.Body)) <= e.maxPasteBytes {
		return nil
	}
	return &tmux.OversizeError{Bytes: len(job.Body), Limit: e.maxPasteBytes}
}

func (e *Executor) deliver(ctx context.Context, job Job, cleaned []byte) error {
	target := job.deliveryTarget()
	body := string(cleaned)

	if job.Mode != "paste" && job.Mode != "type" {
		return fmt.Errorf("unresolved delivery mode %q", job.Mode)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	before, captureErr := e.captureBeforeDelivery(ctx, job)
	if isContextDone(ctx, captureErr) {
		return captureErr
	}

	var err error
	if job.Mode == "paste" {
		err = e.adapter.Paste(ctx, target, body, job.Enter)
	} else {
		err = e.adapter.Type(ctx, target, body, job.Enter)
	}
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	return e.verifyPostInjection(ctx, job, before, captureErr)
}

func canceled(ctx context.Context) (bool, error) {
	err := ctx.Err()
	if err == nil {
		return false, nil
	}
	if errors.Is(err, context.Canceled) {
		return true, nil
	}
	return true, err
}

func isContextDone(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) ||
		(ctx != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()))
}

func (e *Executor) handleFailure(job Job, err error) {
	_ = e.logger.Log(jobEntry(job, failureOutcome(err), err.Error()))
	if target, ok := bannerTarget(job.messageTarget(), err); ok {
		_ = e.adapter.DisplayMessage(target, BannerPrefix+err.Error())
	}
}

func (e *Executor) captureBeforeDelivery(ctx context.Context, job Job) (string, error) {
	if !e.postInjectionVerification {
		return "", nil
	}
	tail, err := e.adapter.CapturePaneTail(ctx, job.PaneID, postInjectionCaptureLines)
	if err != nil {
		if isContextDone(ctx, err) {
			return "", err
		}
		return "", fmt.Errorf("post-injection verification: capture before delivery failed")
	}
	return tail, nil
}

func (e *Executor) verifyPostInjection(ctx context.Context, job Job, before string, beforeErr error) error {
	if !e.postInjectionVerification {
		return nil
	}
	if beforeErr != nil {
		e.handleWarning(job, beforeErr.Error())
		return nil
	}
	after, err := e.adapter.CapturePaneTail(ctx, job.PaneID, postInjectionCaptureLines)
	if err != nil {
		if isContextDone(ctx, err) {
			return err
		}
		e.handleWarning(job, "post-injection verification: capture after delivery failed")
		return nil
	}
	if after == before {
		e.handleWarning(job, "post-injection verification: pane output appeared unchanged after delivery; this is a diagnostic warning, not proof that the target application ignored the input")
	}
	return nil
}

func (e *Executor) handleWarning(job Job, msg string) {
	_ = e.logger.Log(jobEntry(job, OutcomeWarning, msg))
	e.displayWarning(job.messageTarget(), BannerPrefix+"warning: "+msg)
}

func (e *Executor) displayWarning(target tmux.MessageTarget, msg string) {
	if err := e.adapter.DisplayMessage(target, msg); err == nil {
		return
	}
	if target.PaneID == "" {
		return
	}
	target.PaneID = ""
	if target.ClientTTY == "" && target.Window == "" && target.Session == "" {
		return
	}
	_ = e.adapter.DisplayMessage(target, msg)
}

func jobEntry(job Job, outcome, msg string) Entry {
	return Entry{
		JobID:    job.JobID,
		Pane:     job.PaneID,
		Source:   job.Source,
		PromptID: job.PromptID,
		Outcome:  outcome,
		Msg:      msg,
	}
}

func failureOutcome(err error) string {
	var paneMissing *tmux.PaneMissingError
	if errors.As(err, &paneMissing) {
		return OutcomePaneMissing
	}
	var timeout *TimeoutError
	if errors.As(err, &timeout) {
		return OutcomeTimeout
	}
	var strictReject *sanitize.StrictRejectError
	if errors.As(err, &strictReject) {
		return OutcomeSanitizeReject
	}
	var oversize *tmux.OversizeError
	if errors.As(err, &oversize) {
		return OutcomeOversize
	}
	return OutcomeDeliveryError
}

func bannerTarget(msgTarget tmux.MessageTarget, err error) (tmux.MessageTarget, bool) {
	var paneMissing *tmux.PaneMissingError
	if !errors.As(err, &paneMissing) {
		return msgTarget, true
	}
	msgTarget.PaneID = ""
	if msgTarget.ClientTTY == "" && msgTarget.Window == "" && msgTarget.Session == "" {
		return tmux.MessageTarget{}, false
	}
	return msgTarget, true
}
