// Package picker wraps an external picker command for `tprompt pick`
// (DECISIONS.md §15 — not used by the TUI flow).
package picker

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Picker is the interface defined in docs/implementation/interfaces.md.
type Picker interface {
	Select(ids []string) (selectedID string, cancelled bool, err error)
}

// Command runs an external picker command. IDs are written to stdin, one per
// line, and the command's stdout must contain exactly one selected ID.
type Command struct {
	argv []string
}

// NewCommand returns a command-backed Picker.
func NewCommand(argv []string) *Command {
	return &Command{argv: append([]string(nil), argv...)}
}

// Select implements Picker.
func (p *Command) Select(ids []string) (string, bool, error) {
	if len(p.argv) == 0 {
		return "", false, errors.New("picker_command is empty")
	}

	cmd := exec.Command(p.argv[0], p.argv[1:]...) // #nosec G204 -- picker_command is explicit user configuration for this command.
	cmd.Stdin = strings.NewReader(strings.Join(ids, "\n") + "\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if isCancelExit(err) {
			return "", true, nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", false, fmt.Errorf("picker command failed: %s", msg)
	}

	selected, err := parseSelection(stdout.String(), ids)
	if err != nil {
		return "", false, err
	}
	if selected == "" {
		return "", true, nil
	}
	return selected, false, nil
}

func isCancelExit(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	switch exitErr.ExitCode() {
	case 1, 130:
		return true
	default:
		return false
	}
}

func parseSelection(output string, ids []string) (string, error) {
	selection := strings.TrimSpace(output)
	if selection == "" {
		return "", nil
	}
	if strings.Contains(selection, "\n") || strings.Contains(selection, "\r") {
		return "", fmt.Errorf("picker returned multiple lines")
	}
	for _, id := range ids {
		if selection == id {
			return selection, nil
		}
	}
	return "", fmt.Errorf("picker returned unknown prompt ID %q", selection)
}
