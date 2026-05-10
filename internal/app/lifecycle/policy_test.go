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
			wantReasonSubstr: "implicit daemon auto-start is disabled on macOS",
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
			for _, want := range []string{"tprompt daemon start", "tprompt daemon run"} {
				if !strings.Contains(reason, want) {
					t.Fatalf("MacOSImplicitAutoStartDisabled(%v) reason = %q, want substring %q (recovery hint)",
						tt.intent, reason, want)
				}
			}
		})
	}
}
