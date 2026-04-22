// Package submitter converts a tui.Result into a daemon SubmitRequest and
// dials the daemon. Pure orchestration; no UI coupling. Lives outside
// internal/tui so the tui package stays a presentation-only leaf.
package submitter

import (
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/tmux"
	"github.com/hsadler/tprompt/internal/tui"
)

// Submitter converts a TUI selection into a daemon submission. Errors are
// typed so the renderer can decide between staying open with an inline
// message and propagating to runTUI for an exit-code mapping.
type Submitter interface {
	Submit(result tui.Result) error
}

// BodyTooLargeError reports that a prompt or clipboard body exceeds the
// configured max_paste_bytes ceiling at submission time. Distinct from
// tmux.OversizeError, which fires at delivery time and maps to ExitDelivery.
type BodyTooLargeError struct {
	Bytes int
	Limit int64
}

func (e *BodyTooLargeError) Error() string {
	return fmt.Sprintf("body exceeds max_paste_bytes (%d > %d)", e.Bytes, e.Limit)
}

// New returns a Submitter wired against real dependencies.
func New(prompts store.Store, client daemon.Client, cfg config.Resolved, target tmux.TargetContext) Submitter {
	return &submitter{
		prompts: prompts,
		client:  client,
		cfg:     cfg,
		target:  target,
	}
}

type submitter struct {
	prompts store.Store
	client  daemon.Client
	cfg     config.Resolved
	target  tmux.TargetContext
}

func (s *submitter) Submit(result tui.Result) error {
	switch result.Action {
	case tui.ActionPrompt:
		return s.submitPrompt(result.PromptID)
	case tui.ActionClipboard:
		return s.submitClipboard(result.ClipboardBody)
	case tui.ActionCancel:
		return nil
	default:
		return fmt.Errorf("submitter: unknown action %q", result.Action)
	}
}

func (s *submitter) submitPrompt(id string) error {
	prompt, err := s.prompts.Resolve(id)
	if err != nil {
		return err
	}

	delivery, err := config.ResolveDelivery(s.cfg, config.FrontmatterDefaults{
		Mode:  prompt.Defaults.Mode,
		Enter: prompt.Defaults.Enter,
	}, config.DeliveryFlags{})
	if err != nil {
		return err
	}

	body := []byte(prompt.Body)
	if s.cfg.MaxPasteBytes > 0 && int64(len(body)) > s.cfg.MaxPasteBytes {
		return &BodyTooLargeError{Bytes: len(body), Limit: s.cfg.MaxPasteBytes}
	}

	job := daemon.Job{
		SubmitterPID: os.Getpid(),
		Source:       daemon.SourcePrompt,
		PromptID:     prompt.ID,
		SourcePath:   prompt.Path,
		Body:         body,
		Mode:         delivery.Mode,
		Enter:        delivery.Enter,
		SanitizeMode: delivery.Sanitize,
		PaneID:       s.target.PaneID,
		Origin:       buildOrigin(s.target),
		Verification: daemon.VerificationPolicy{
			TimeoutMS:      s.cfg.VerificationTimeoutMS,
			PollIntervalMS: s.cfg.VerificationPollIntervalMS,
		},
	}

	resp, err := s.client.Submit(daemon.SubmitRequest{Job: job})
	if err != nil {
		return err
	}
	if !resp.Accepted {
		return &daemon.IPCError{
			Op:     "submit",
			Reason: fmt.Sprintf("daemon did not accept job (job_id=%q)", resp.JobID),
		}
	}
	return nil
}

func (s *submitter) submitClipboard(body []byte) error {
	// Validate content before resolving delivery or dialing the daemon so
	// typed content errors propagate cheaply. The 5b Renderer pre-validates
	// via clipboard.Validate, but the Phase 5a stub Renderer and tests hand
	// bytes in directly — defense in depth.
	if len(body) == 0 {
		return &clipboard.EmptyClipboardError{}
	}
	if !utf8.Valid(body) {
		return &clipboard.InvalidUTF8Error{}
	}
	if s.cfg.MaxPasteBytes > 0 && int64(len(body)) > s.cfg.MaxPasteBytes {
		// Translate to BodyTooLargeError so the submit path has one uniform
		// oversize contract across prompt and clipboard sources.
		return &BodyTooLargeError{Bytes: len(body), Limit: s.cfg.MaxPasteBytes}
	}

	delivery, err := config.ResolveDelivery(s.cfg, config.FrontmatterDefaults{}, config.DeliveryFlags{})
	if err != nil {
		return err
	}

	job := daemon.Job{
		SubmitterPID: os.Getpid(),
		Source:       daemon.SourceClipboard,
		Body:         body,
		Mode:         delivery.Mode,
		Enter:        delivery.Enter,
		SanitizeMode: delivery.Sanitize,
		PaneID:       s.target.PaneID,
		Origin:       buildOrigin(s.target),
		Verification: daemon.VerificationPolicy{
			TimeoutMS:      s.cfg.VerificationTimeoutMS,
			PollIntervalMS: s.cfg.VerificationPollIntervalMS,
		},
	}

	resp, err := s.client.Submit(daemon.SubmitRequest{Job: job})
	if err != nil {
		return err
	}
	if !resp.Accepted {
		return &daemon.IPCError{
			Op:     "submit",
			Reason: fmt.Sprintf("daemon did not accept job (job_id=%q)", resp.JobID),
		}
	}
	return nil
}

func buildOrigin(target tmux.TargetContext) *tmux.OriginContext {
	if target.Session == "" && target.Window == "" && target.ClientTTY == "" {
		return nil
	}
	return &tmux.OriginContext{
		Session:   target.Session,
		Window:    target.Window,
		ClientTTY: target.ClientTTY,
	}
}
