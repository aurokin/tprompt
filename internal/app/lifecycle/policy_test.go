package lifecycle

import (
	"runtime"
	"strings"
	"testing"
)

func TestMacOSImplicitAutoStartDisabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		intent           StartIntent
		wantDisabledMac  bool
		wantReasonSubstr string
	}{
		{
			name:             "implicit_tui",
			intent:           IntentImplicitTUI,
			wantDisabledMac:  true,
			wantReasonSubstr: "macOS does not implicitly auto-start the tprompt daemon",
		},
		{
			name:            "explicit_start",
			intent:          IntentExplicitStart,
			wantDisabledMac: false,
		},
		{
			name:            "explicit_run",
			intent:          IntentExplicitRun,
			wantDisabledMac: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			disabled, reason := MacOSImplicitAutoStartDisabled(tt.intent)
			wantDisabled := tt.wantDisabledMac && runtime.GOOS == "darwin"
			if disabled != wantDisabled {
				t.Fatalf("MacOSImplicitAutoStartDisabled(%v) disabled = %v, want %v (GOOS=%s)",
					tt.intent, disabled, wantDisabled, runtime.GOOS)
			}
			if !disabled {
				if reason != "" {
					t.Fatalf("MacOSImplicitAutoStartDisabled(%v) reason = %q, want empty when not disabled",
						tt.intent, reason)
				}
				return
			}
			if !strings.Contains(reason, tt.wantReasonSubstr) {
				t.Fatalf("MacOSImplicitAutoStartDisabled(%v) reason = %q, want substring %q",
					tt.intent, reason, tt.wantReasonSubstr)
			}
			for _, want := range []string{"tprompt daemon start", "tprompt daemon run", "daemon is not running", "TUI requires a running daemon"} {
				if !strings.Contains(reason, want) {
					t.Fatalf("MacOSImplicitAutoStartDisabled(%v) reason = %q, want substring %q (recovery hint)",
						tt.intent, reason, want)
				}
			}
		})
	}
}

// TestMacOSImplicitAutoStartDisabledOmitsOptOutHint locks AUR-329's
// decision NOT to mention TPROMPT_NO_AUTO_START or
// --no-daemon-auto-start in the implicit-disabled reason: on darwin
// the platform refuses regardless of those settings, so suggesting
// them would mislead the operator into thinking the env var or flag
// re-enables auto-start (it does not — it short-circuits the attempt
// upstream, which is a different surface).
func TestMacOSImplicitAutoStartDisabledOmitsOptOutHint(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("policy only fires on darwin")
	}
	_, reason := MacOSImplicitAutoStartDisabled(IntentImplicitTUI)
	for _, forbidden := range []string{"TPROMPT_NO_AUTO_START", "--no-daemon-auto-start", "silence"} {
		if strings.Contains(reason, forbidden) {
			t.Fatalf("reason = %q, must not mention %q (would mislead — opt-outs do not bypass platform refusal)",
				reason, forbidden)
		}
	}
}
