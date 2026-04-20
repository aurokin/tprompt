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

// Executor runs verification, the pre-sanitize byte cap, the sanitizer, and
// the tmux adapter for a single Job. It is the JobRunner the queue calls on
// each enqueued worker. Failures surface via the logger and tmux display-
// message; success is silent (no log, no banner) per
// docs/commands/daemon.md.
type Executor struct {
	adapter       tmux.Adapter
	logger        *Logger
	maxPasteBytes int64
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

// Run is the JobRunner entrypoint. Verification first; then the
// max_paste_bytes check (pre-sanitize, matching `runSend`); then the
// sanitizer; then the adapter call. Cancellation by the queue
// (replace-same-target) is silent here — the queue already logged and
// surfaced the banner.
func (e *Executor) Run(ctx context.Context, job Job) bool {
	if err := Verify(ctx, e.adapter, job.verificationTarget(), job.Verification, job.SubmitterPID); err != nil {
		if errors.Is(err, context.Canceled) {
			return false
		}
		e.handleFailure(job, err)
		return true
	}
	if shouldStop, err := canceled(ctx); shouldStop {
		if err != nil {
			e.handleFailure(job, err)
		}
		return false
	}

	if e.maxPasteBytes > 0 && int64(len(job.Body)) > e.maxPasteBytes {
		e.handleFailure(job, &tmux.OversizeError{Bytes: len(job.Body), Limit: e.maxPasteBytes})
		return true
	}

	cleaned, err := sanitizeProcess(job.SanitizeMode, job.Body)
	if err != nil {
		e.handleFailure(job, err)
		return true
	}
	if shouldStop, err := canceled(ctx); shouldStop {
		if err != nil {
			e.handleFailure(job, err)
		}
		return false
	}

	var deliveryErr error
	switch job.Mode {
	case "paste":
		deliveryErr = e.adapter.Paste(ctx, job.deliveryTarget(), string(cleaned), job.Enter)
	case "type":
		deliveryErr = e.adapter.Type(ctx, job.deliveryTarget(), string(cleaned), job.Enter)
	default:
		deliveryErr = fmt.Errorf("unresolved delivery mode %q", job.Mode)
	}
	if deliveryErr != nil {
		if errors.Is(deliveryErr, context.Canceled) {
			return false
		}
		e.handleFailure(job, deliveryErr)
	}
	return true
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

func (e *Executor) handleFailure(job Job, err error) {
	_ = e.logger.Log(Entry{
		JobID:   job.JobID,
		Pane:    job.PaneID,
		Source:  job.Source,
		Outcome: failureOutcome(err),
		Msg:     err.Error(),
	})
	if target, ok := bannerTarget(job.messageTarget(), err); ok {
		_ = e.adapter.DisplayMessage(target, BannerPrefix+err.Error())
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
