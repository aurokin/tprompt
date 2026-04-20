package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// DefaultChunkSize is the rune-safe byte cap for a single send-keys chunk.
// See docs/tmux/delivery.md.
const DefaultChunkSize = 4096

// DefaultTimeout caps how long any single adapter method will wait on tmux.
// A local tmux shouldn't take more than milliseconds, so this exists to bound
// hangs (wedged server, stuck socket) rather than to pace normal calls.
const DefaultTimeout = 5 * time.Second

// Exec is the default tmux.Adapter implementation. Construction is cheap — no
// tmux invocations happen until a method is called.
type Exec struct {
	runner    Runner
	chunkSize int
	timeout   time.Duration
	now       func() time.Time
	pid       int
}

// New returns an Exec adapter using the given Runner.
func New(runner Runner) *Exec {
	return &Exec{
		runner:    runner,
		chunkSize: DefaultChunkSize,
		timeout:   DefaultTimeout,
		now:       time.Now,
		pid:       os.Getpid(),
	}
}

func (e *Exec) CurrentContext() (TargetContext, error) {
	ctx, cancel := e.timedCtx(context.Background())
	defer cancel()
	out, err := e.runner.Run(ctx, []string{
		"display-message", "-p",
		"-F", "#{session_id}|#{window_id}|#{pane_id}|#{client_tty}",
	}, nil)
	if err != nil {
		return TargetContext{}, &EnvError{Reason: runnerMessage(err)}
	}
	line := strings.TrimRight(string(out), "\n")
	parts := strings.SplitN(line, "|", 4)
	if len(parts) != 4 {
		return TargetContext{}, &EnvError{Reason: fmt.Sprintf("unexpected display-message output: %q", line)}
	}
	return TargetContext{
		Session:   parts[0],
		Window:    parts[1],
		PaneID:    parts[2],
		ClientTTY: parts[3],
	}, nil
}

func (e *Exec) PaneExists(parent context.Context, paneID string) (bool, error) {
	// tmux display-message with a bogus -t can exit 0 with empty stdout on some
	// versions, so treat empty output as "does not exist." Only swallow runner
	// errors that match tmux's pane-not-found message; anything else (dead
	// server, timeout, permission) surfaces as EnvError so the caller can
	// distinguish a missing pane from a broken environment.
	ctx, cancel := e.timedCtx(parent)
	defer cancel()
	out, err := e.runner.Run(ctx, []string{
		"display-message", "-p", "-t", paneID, "-F", "#{pane_id}",
	}, nil)
	if err != nil {
		return false, classifyPaneProbeError(parent, err, paneID, false)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func (e *Exec) IsTargetSelected(parent context.Context, target TargetContext) (bool, error) {
	if target.ClientTTY != "" {
		ok, err := e.isClientOnTarget(parent, target)
		if err == nil {
			return ok, nil
		}
		if !isMissingClientError(err) {
			return false, err
		}
	}
	return e.isPaneForegroundSelected(parent, target)
}

func (e *Exec) CapturePaneTail(paneID string, lines int) (string, error) {
	ctx, cancel := e.timedCtx(context.Background())
	defer cancel()
	out, err := e.runner.Run(ctx, []string{
		"capture-pane", "-p", "-t", paneID, "-S", fmt.Sprintf("-%d", lines),
	}, nil)
	if err != nil {
		return "", &DeliveryError{Op: "capture-pane", Target: paneID, Message: runnerMessage(err)}
	}
	return string(out), nil
}

func (e *Exec) Paste(parent context.Context, target TargetContext, body string, pressEnter bool) error {
	bufName := fmt.Sprintf("tprompt-send-%d-%d", e.pid, e.now().UnixNano())
	ctx, cancel := e.timedCtx(parent)
	defer cancel()

	if _, err := e.runner.Run(ctx,
		[]string{"load-buffer", "-b", bufName, "-"},
		[]byte(body),
	); err != nil {
		return &DeliveryError{Op: "load-buffer", Target: target.PaneID, Message: runnerMessage(err), Cause: err}
	}

	if _, err := e.runner.Run(ctx,
		[]string{"paste-buffer", "-d", "-p", "-b", bufName, "-t", target.PaneID},
		nil,
	); err != nil {
		// paste-buffer -d only deletes on success, so clean up explicitly here.
		cleanupCtx, cleanupCancel := e.timedCtx(context.Background())
		_, _ = e.runner.Run(cleanupCtx, []string{"delete-buffer", "-b", bufName}, nil)
		cleanupCancel()
		return &DeliveryError{Op: "paste-buffer", Target: target.PaneID, Message: runnerMessage(err), Cause: err}
	}

	if pressEnter {
		if err := e.sendEnter(ctx, target.PaneID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Exec) Type(parent context.Context, target TargetContext, body string, pressEnter bool) error {
	ctx, cancel := e.timedCtx(parent)
	defer cancel()
	for _, chunk := range chunkByRunes(body, e.chunkSize) {
		if _, err := e.runner.Run(ctx,
			[]string{"send-keys", "-t", target.PaneID, "-l", "--", chunk},
			nil,
		); err != nil {
			return &DeliveryError{Op: "send-keys", Target: target.PaneID, Message: runnerMessage(err), Cause: err}
		}
	}
	if pressEnter {
		if err := e.sendEnter(ctx, target.PaneID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Exec) DisplayMessage(target MessageTarget, message string) error {
	ctx, cancel := e.timedCtx(context.Background())
	defer cancel()

	if target.ClientTTY != "" {
		clientArgv := []string{"display-message", "-c", target.ClientTTY, message}
		if _, err := e.runner.Run(ctx, clientArgv, nil); err == nil {
			return nil
		} else if !isMissingClientReason(runnerMessage(err)) {
			return &DeliveryError{Op: "display-message", Message: runnerMessage(err)}
		}
	}

	if _, err := e.runner.Run(ctx, displayMessageArgv(target, message), nil); err != nil {
		return &DeliveryError{Op: "display-message", Message: runnerMessage(err)}
	}
	return nil
}

func (e *Exec) timedCtx(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if e.timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, e.timeout)
}

func (e *Exec) sendEnter(ctx context.Context, paneID string) error {
	if _, err := e.runner.Run(ctx,
		[]string{"send-keys", "-t", paneID, "Enter"},
		nil,
	); err != nil {
		return &DeliveryError{Op: "send-keys", Target: paneID, Message: runnerMessage(err), Cause: err}
	}
	return nil
}

func (e *Exec) isClientOnTarget(parent context.Context, target TargetContext) (bool, error) {
	ctx, cancel := e.timedCtx(parent)
	defer cancel()
	out, err := e.runner.Run(ctx, []string{
		"display-message", "-p", "-c", target.ClientTTY,
		"-F", "#{session_id}|#{window_id}|#{pane_id}",
	}, nil)
	if err != nil {
		return false, classifyProbeError(parent, err)
	}
	current, err := parseSelectedClientContext(out)
	if err != nil {
		return false, err
	}
	return current.PaneID == target.PaneID &&
		matchesIfKnown(target.Window, current.Window) &&
		matchesIfKnown(target.Session, current.Session), nil
}

func (e *Exec) isPaneForegroundSelected(parent context.Context, target TargetContext) (bool, error) {
	ctx, cancel := e.timedCtx(parent)
	defer cancel()
	out, err := e.runner.Run(ctx, []string{
		"display-message", "-p", "-t", target.PaneID,
		"-F", "#{session_id}|#{window_id}|#{pane_id}|#{pane_active}|#{window_active}",
	}, nil)
	if err != nil {
		return false, classifyPaneProbeError(parent, err, target.PaneID, true)
	}
	line := strings.TrimRight(string(out), "\n")
	parts := strings.SplitN(line, "|", 5)
	if len(parts) != 5 {
		return false, &EnvError{Reason: fmt.Sprintf("unexpected display-message output: %q", line)}
	}
	if parts[2] != target.PaneID {
		return false, nil
	}
	if !matchesIfKnown(target.Window, parts[1]) || !matchesIfKnown(target.Session, parts[0]) {
		return false, nil
	}
	return parts[3] == "1" && parts[4] == "1", nil
}

func classifyProbeError(parent context.Context, err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) && parent != nil && errors.Is(parent.Err(), context.DeadlineExceeded) {
		return err
	}
	return &EnvError{Reason: runnerMessage(err)}
}

func classifyPaneProbeError(parent context.Context, err error, paneID string, missingPaneIsError bool) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) && parent != nil && errors.Is(parent.Err(), context.DeadlineExceeded) {
		return err
	}
	msg := runnerMessage(err)
	if strings.Contains(msg, "can't find pane") {
		if missingPaneIsError {
			return &PaneMissingError{PaneID: paneID}
		}
		return nil
	}
	return &EnvError{Reason: msg}
}

func isMissingClientError(err error) bool {
	var envErr *EnvError
	if !errors.As(err, &envErr) {
		return false
	}
	return isMissingClientReason(envErr.Reason)
}

func isMissingClientReason(reason string) bool {
	reason = strings.ToLower(reason)
	return strings.Contains(reason, "can't find client") ||
		strings.Contains(reason, "no current client")
}

func parseSelectedClientContext(out []byte) (TargetContext, error) {
	line := strings.TrimRight(string(out), "\n")
	parts := strings.SplitN(line, "|", 3)
	if len(parts) != 3 {
		return TargetContext{}, &EnvError{Reason: fmt.Sprintf("unexpected display-message output: %q", line)}
	}
	return TargetContext{
		Session: parts[0],
		Window:  parts[1],
		PaneID:  parts[2],
	}, nil
}

func matchesIfKnown(want, got string) bool {
	return want == "" || want == got
}

func displayMessageArgv(target MessageTarget, message string) []string {
	argv := []string{"display-message"}
	switch {
	case target.PaneID != "":
		argv = append(argv, "-t", target.PaneID)
	case target.Window != "":
		argv = append(argv, "-t", target.Window)
	case target.Session != "":
		argv = append(argv, "-t", target.Session)
	}
	return append(argv, message)
}
