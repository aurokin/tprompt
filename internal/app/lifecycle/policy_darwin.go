//go:build darwin

package lifecycle

// MacOSImplicitAutoStartDisabled reports whether the launcher must
// refuse this StartIntent because of the platform-locked policy: on
// darwin, IntentImplicitTUI is hardcoded off because validating
// executable trust before launchd hands the binary to the kernel
// triggered repeated kernel panics in AppleSystemPolicy / AMFI /
// syspolicyd in practice (AUR-326). Explicit intents
// (IntentExplicitStart, IntentExplicitRun) bypass the policy and
// reach the spawn path normally.
//
// The reason string is the user-visible explanation surfaced through
// the daemon IPC error wrapper. It deliberately points at the two
// recovery commands so the TUI failure banner reads as guidance, not
// as a dead end.
func MacOSImplicitAutoStartDisabled(intent StartIntent) (disabled bool, reason string) {
	if intent != IntentImplicitTUI {
		return false, ""
	}
	return true, "implicit daemon auto-start is disabled on macOS; run 'tprompt daemon start' (background) or 'tprompt daemon run' (foreground) to start the daemon explicitly"
}
