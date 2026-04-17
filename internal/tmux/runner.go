package tmux

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// Runner invokes a tmux subcommand and returns its stdout. Keeping exec behind
// this interface lets the adapter be unit-tested without a real tmux binary.
type Runner interface {
	Run(ctx context.Context, argv []string, stdin []byte) ([]byte, error)
}

// NewExecRunner returns a Runner that shells out to the given tmux binary.
// An empty binary name defaults to "tmux".
func NewExecRunner(binary string) Runner {
	if binary == "" {
		binary = "tmux"
	}
	return &execRunner{binary: binary}
}

type execRunner struct {
	binary string
}

func (r *execRunner) Run(ctx context.Context, argv []string, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binary, argv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), &runnerError{Err: err, Message: msg}
	}
	return stdout.Bytes(), nil
}

// runnerError carries both the underlying exec error and a human-readable
// message (tmux's stderr when present, otherwise the exec error text) so the
// adapter can surface tmux's own message to the user.
type runnerError struct {
	Err     error
	Message string
}

func (e *runnerError) Error() string { return e.Message }
func (e *runnerError) Unwrap() error { return e.Err }

func runnerMessage(err error) string {
	var re *runnerError
	if errors.As(err, &re) {
		return re.Message
	}
	return err.Error()
}
