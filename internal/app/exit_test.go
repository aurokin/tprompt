package app

import (
	"errors"
	"testing"

	"github.com/hsadler/tprompt/internal/clipboard"
	"github.com/hsadler/tprompt/internal/config"
	"github.com/hsadler/tprompt/internal/daemon"
	"github.com/hsadler/tprompt/internal/keybind"
	"github.com/hsadler/tprompt/internal/sanitize"
	"github.com/hsadler/tprompt/internal/store"
	"github.com/hsadler/tprompt/internal/submitter"
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
		{"BodyTooLarge", &submitter.BodyTooLargeError{Bytes: 10, Limit: 5}},
		{"ClipboardEmpty", &clipboard.EmptyClipboardError{}},
		{"ClipboardInvalidUTF8", &clipboard.InvalidUTF8Error{}},
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

func TestExitCodeCobraUsageErrors(t *testing.T) {
	// Exact strings from cobra/pflag; kept here so drift in upstream wording
	// fails this test loudly rather than silently degrading exit code 2 → 1.
	cases := []string{
		`required flag(s) "target-pane" not set`,
		"unknown flag: --nope",
		"unknown shorthand flag: 'x' in -x",
		`unknown command "bogus" for "tprompt"`,
		"flag needs an argument: --config",
		"flag needs an argument: --target-pane",
		"bad flag syntax: --=x",
		`invalid argument "abc" for "--count" flag: strconv.ParseInt: parsing "abc": invalid syntax`,
		"accepts 1 arg(s), received 0",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if got := ExitCode(errors.New(msg)); got != ExitUsage {
				t.Fatalf("ExitCode(%q) = %d, want %d", msg, got, ExitUsage)
			}
		})
	}
}
