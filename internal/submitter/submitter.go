// Package submitter converts a tui.Result into a delivery submission. Pure
// orchestration; no UI coupling. Lives outside internal/tui so the tui package
// stays a presentation-only leaf.
package submitter

import (
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/aurokin/tprompt/internal/clipboard"
	"github.com/aurokin/tprompt/internal/config"
	"github.com/aurokin/tprompt/internal/delivery"
	"github.com/aurokin/tprompt/internal/prompttmpl"
	"github.com/aurokin/tprompt/internal/store"
	"github.com/aurokin/tprompt/internal/tmux"
	"github.com/aurokin/tprompt/internal/tui"
)

// Submitter converts a TUI selection into a delivery submission. Errors are
// typed so the renderer can decide between staying open with an inline
// message and propagating to runTUI for an exit-code mapping.
type Submitter interface {
	Submit(result tui.Result) error
}

type scopedStore interface {
	ResolveScoped(id, scope string) (store.Prompt, error)
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
func New(prompts store.Store, client delivery.Client, cfg config.Resolved, target tmux.TargetContext) Submitter {
	return &submitter{
		prompts: prompts,
		client:  client,
		cfg:     cfg,
		target:  target,
	}
}

type submitter struct {
	prompts store.Store
	client  delivery.Client
	cfg     config.Resolved
	target  tmux.TargetContext
}

func (s *submitter) Submit(result tui.Result) error {
	switch result.Action {
	case tui.ActionPrompt:
		return s.submitPrompt(result.PromptID, result.Scope, result.TemplateValues)
	case tui.ActionClipboard:
		return s.submitClipboard(result.ClipboardBody)
	case tui.ActionCancel:
		return nil
	default:
		return fmt.Errorf("submitter: unknown action %q", result.Action)
	}
}

func (s *submitter) submitPrompt(id, scope string, values map[string]string) error {
	var (
		prompt store.Prompt
		err    error
	)
	if scope != "" {
		if scoped, ok := s.prompts.(scopedStore); ok {
			prompt, err = scoped.ResolveScoped(id, scope)
		} else {
			prompt, err = s.prompts.Resolve(id)
		}
	} else {
		prompt, err = s.prompts.Resolve(id)
	}
	if err != nil {
		return err
	}

	resolved, err := config.ResolveDelivery(s.cfg, config.FrontmatterDefaults{
		Mode:  prompt.Defaults.Mode,
		Enter: prompt.Defaults.Enter,
	}, config.DeliveryFlags{})
	if err != nil {
		return err
	}

	rendered, err := prompttmpl.Render(prompt.Body, prompt.Variables, values)
	if err != nil {
		return err
	}
	body := []byte(rendered)
	if s.cfg.MaxPasteBytes > 0 && int64(len(body)) > s.cfg.MaxPasteBytes {
		return &BodyTooLargeError{Bytes: len(body), Limit: s.cfg.MaxPasteBytes}
	}

	job := delivery.Job{
		SubmitterPID: os.Getpid(),
		Source:       delivery.SourcePrompt,
		PromptID:     prompt.ID,
		SourcePath:   prompt.Path,
		Body:         body,
		Mode:         resolved.Mode,
		Enter:        resolved.Enter,
		SanitizeMode: resolved.Sanitize,
		PaneID:       s.target.PaneID,
		Origin:       buildOrigin(s.target),
		Verification: delivery.VerificationPolicy{
			TimeoutMS:      s.cfg.VerificationTimeoutMS,
			PollIntervalMS: s.cfg.VerificationPollIntervalMS,
		},
	}

	resp, err := s.client.Submit(delivery.SubmitRequest{Job: job})
	if err != nil {
		return err
	}
	if !resp.Accepted {
		return &delivery.IPCError{
			Op:     "submit",
			Reason: fmt.Sprintf("delivery client did not accept job (job_id=%q)", resp.JobID),
		}
	}
	return nil
}

func (s *submitter) submitClipboard(body []byte) error {
	// Validate content before resolving delivery so
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

	resolved, err := config.ResolveDelivery(s.cfg, config.FrontmatterDefaults{}, config.DeliveryFlags{})
	if err != nil {
		return err
	}

	job := delivery.Job{
		SubmitterPID: os.Getpid(),
		Source:       delivery.SourceClipboard,
		Body:         body,
		Mode:         resolved.Mode,
		Enter:        resolved.Enter,
		SanitizeMode: resolved.Sanitize,
		PaneID:       s.target.PaneID,
		Origin:       buildOrigin(s.target),
		Verification: delivery.VerificationPolicy{
			TimeoutMS:      s.cfg.VerificationTimeoutMS,
			PollIntervalMS: s.cfg.VerificationPollIntervalMS,
		},
	}

	resp, err := s.client.Submit(delivery.SubmitRequest{Job: job})
	if err != nil {
		return err
	}
	if !resp.Accepted {
		return &delivery.IPCError{
			Op:     "submit",
			Reason: fmt.Sprintf("delivery client did not accept job (job_id=%q)", resp.JobID),
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
