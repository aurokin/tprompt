package app

import (
	"errors"
	"testing"

	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/keybind"
	"github.com/hsadler/tprompt/internal/sanitize"
	"github.com/hsadler/tprompt/internal/store"
)

func TestExitCodeNilIsZero(t *testing.T) {
	if got := ExitCode(nil); got != ExitOK {
		t.Fatalf("ExitCode(nil) = %d, want %d", got, ExitOK)
	}
}

func TestExitCodeConfigValidation(t *testing.T) {
	err := &config.ValidationError{Field: "prompts_dir", Message: "must be set"}
	if got := ExitCode(err); got != ExitUsage {
		t.Fatalf("ExitCode(ValidationError) = %d, want %d", got, ExitUsage)
	}
}

func TestExitCodeWrappedConfigValidation(t *testing.T) {
	inner := &config.ValidationError{Field: "sanitize", Message: "bad"}
	err := errors.Join(errors.New("config failed"), inner)
	if got := ExitCode(err); got != ExitUsage {
		t.Fatalf("ExitCode(wrapped ValidationError) = %d, want %d", got, ExitUsage)
	}
}

func TestExitCodePromptErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"DuplicatePromptID", &store.DuplicatePromptIDError{ID: "x", Paths: []string{"a", "b"}}},
		{"NotFound", &store.NotFoundError{ID: "x"}},
		{"InvalidPromptMode", &store.InvalidPromptModeError{Path: "x", Value: "y"}},
		{"DuplicateKeybind", &keybind.DuplicateKeybindError{Key: 'a'}},
		{"ReservedKeybind", &keybind.ReservedKeybindError{Key: 'p', Action: "clipboard"}},
		{"MalformedKeybind", &keybind.MalformedKeybindError{Value: "ctrl+x"}},
		{"StrictReject", &sanitize.StrictRejectError{Class: "OSC", Offset: 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.err); got != ExitPrompt {
				t.Fatalf("ExitCode(%T) = %d, want %d", tc.err, got, ExitPrompt)
			}
		})
	}
}

func TestExitCodePromptsDirMissing(t *testing.T) {
	err := &store.PromptsDirMissingError{Path: "/nope"}
	if got := ExitCode(err); got != ExitUsage {
		t.Fatalf("ExitCode(PromptsDirMissingError) = %d, want %d", got, ExitUsage)
	}
}

func TestExitCodeDaemonErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"SocketUnavailable", &daemon.SocketUnavailableError{Path: "/tmp/x.sock"}},
		{"IPC", &daemon.IPCError{Path: "/tmp/x.sock", Op: "read response", Reason: "EOF"}},
		{"Timeout", &daemon.TimeoutError{TimeoutMS: 5000}},
		{"InvalidPolicy", &daemon.InvalidPolicyError{Field: "timeout_ms", Value: 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.err); got != ExitDaemon {
				t.Fatalf("ExitCode(%T) = %d, want %d", tc.err, got, ExitDaemon)
			}
		})
	}
}

func TestExitCodeUnknownError(t *testing.T) {
	if got := ExitCode(errors.New("something unexpected")); got != ExitGeneral {
		t.Fatalf("ExitCode(generic) = %d, want %d", got, ExitGeneral)
	}
}
