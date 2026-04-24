package app

import (
	"strings"
	"testing"

	"github.com/hsadler/tprompt/internal/tui"
)

func TestParseTestRenderer_Cancel(t *testing.T) {
	r, err := parseTestRenderer("cancel", nil)
	if err != nil {
		t.Fatalf("parseTestRenderer: %v", err)
	}
	got, err := r.Run(tui.State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Action != tui.ActionCancel {
		t.Errorf("Action = %q, want ActionCancel", got.Action)
	}
}

func TestParseTestRenderer_Clipboard(t *testing.T) {
	r, err := parseTestRenderer("clipboard:hello from tests", nil)
	if err != nil {
		t.Fatalf("parseTestRenderer: %v", err)
	}
	got, err := r.Run(tui.State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Action != tui.ActionClipboard {
		t.Errorf("Action = %q, want ActionClipboard", got.Action)
	}
	if string(got.ClipboardBody) != "hello from tests" {
		t.Errorf("ClipboardBody = %q", got.ClipboardBody)
	}
}

func TestParseTestRenderer_ClipboardEmptyBody(t *testing.T) {
	// `clipboard:` with empty body is a legal spec — the Submitter will surface
	// EmptyClipboardError, which is exactly what the oversize/empty testscripts
	// may want to exercise.
	r, err := parseTestRenderer("clipboard:", nil)
	if err != nil {
		t.Fatalf("parseTestRenderer: %v", err)
	}
	got, _ := r.Run(tui.State{})
	if got.Action != tui.ActionClipboard {
		t.Errorf("Action = %q, want ActionClipboard", got.Action)
	}
	if len(got.ClipboardBody) != 0 {
		t.Errorf("ClipboardBody = %q, want empty", got.ClipboardBody)
	}
}

func TestParseTestRenderer_Unknown(t *testing.T) {
	_, err := parseTestRenderer("nonsense", nil)
	if err == nil {
		t.Fatal("want error for unknown spec")
	}
	if !strings.Contains(err.Error(), "unsupported spec") {
		t.Errorf("error = %q, want 'unsupported spec'", err)
	}
}
