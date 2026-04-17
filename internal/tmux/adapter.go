package tmux

import (
	"context"
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
	ctx, cancel := e.timedCtx()
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

func (e *Exec) PaneExists(paneID string) (bool, error) {
	// tmux display-message with a bogus -t can exit 0 with empty stdout on some
	// versions, so treat empty output as "does not exist." Only swallow runner
	// errors that match tmux's pane-not-found message; anything else (dead
	// server, timeout, permission) surfaces as EnvError so the caller can
	// distinguish a missing pane from a broken environment.
	ctx, cancel := e.timedCtx()
	defer cancel()
	out, err := e.runner.Run(ctx, []string{
		"display-message", "-p", "-t", paneID, "-F", "#{pane_id}",
	}, nil)
	if err != nil {
		msg := runnerMessage(err)
		if strings.Contains(msg, "can't find pane") {
			return false, nil
		}
		return false, &EnvError{Reason: msg}
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func (e *Exec) IsTargetSelected(target TargetContext) (bool, error) {
	ctx, cancel := e.timedCtx()
	defer cancel()
	out, err := e.runner.Run(ctx, []string{
		"display-message", "-p", "-t", target.PaneID, "-F", "#{pane_active}",
	}, nil)
	if err != nil {
		return false, &EnvError{Reason: runnerMessage(err)}
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

func (e *Exec) CapturePaneTail(paneID string, lines int) (string, error) {
	ctx, cancel := e.timedCtx()
	defer cancel()
	out, err := e.runner.Run(ctx, []string{
		"capture-pane", "-p", "-t", paneID, "-S", fmt.Sprintf("-%d", lines),
	}, nil)
	if err != nil {
		return "", &DeliveryError{Op: "capture-pane", Target: paneID, Message: runnerMessage(err)}
	}
	return string(out), nil
}

func (e *Exec) Paste(target TargetContext, body string, pressEnter bool) error {
	bufName := fmt.Sprintf("tprompt-send-%d-%d", e.pid, e.now().UnixNano())
	ctx, cancel := e.timedCtx()
	defer cancel()

	if _, err := e.runner.Run(ctx,
		[]string{"load-buffer", "-b", bufName, "-"},
		[]byte(body),
	); err != nil {
		return &DeliveryError{Op: "load-buffer", Target: target.PaneID, Message: runnerMessage(err)}
	}

	if _, err := e.runner.Run(ctx,
		[]string{"paste-buffer", "-d", "-p", "-b", bufName, "-t", target.PaneID},
		nil,
	); err != nil {
		// paste-buffer -d only deletes on success, so clean up explicitly here.
		_, _ = e.runner.Run(ctx, []string{"delete-buffer", "-b", bufName}, nil)
		return &DeliveryError{Op: "paste-buffer", Target: target.PaneID, Message: runnerMessage(err)}
	}

	if pressEnter {
		if err := e.sendEnter(ctx, target.PaneID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Exec) Type(target TargetContext, body string, pressEnter bool) error {
	ctx, cancel := e.timedCtx()
	defer cancel()
	for _, chunk := range chunkByRunes(body, e.chunkSize) {
		if _, err := e.runner.Run(ctx,
			[]string{"send-keys", "-t", target.PaneID, "-l", "--", chunk},
			nil,
		); err != nil {
			return &DeliveryError{Op: "send-keys", Target: target.PaneID, Message: runnerMessage(err)}
		}
	}
	if pressEnter {
		if err := e.sendEnter(ctx, target.PaneID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Exec) DisplayMessage(clientTTY, message string) error {
	argv := []string{"display-message"}
	if clientTTY != "" {
		argv = append(argv, "-c", clientTTY)
	}
	argv = append(argv, message)
	ctx, cancel := e.timedCtx()
	defer cancel()
	if _, err := e.runner.Run(ctx, argv, nil); err != nil {
		return &DeliveryError{Op: "display-message", Message: runnerMessage(err)}
	}
	return nil
}

func (e *Exec) timedCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), e.timeout)
}

func (e *Exec) sendEnter(ctx context.Context, paneID string) error {
	if _, err := e.runner.Run(ctx,
		[]string{"send-keys", "-t", paneID, "Enter"},
		nil,
	); err != nil {
		return &DeliveryError{Op: "send-keys", Target: paneID, Message: runnerMessage(err)}
	}
	return nil
}
