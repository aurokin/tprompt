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
// the daemon IPC error wrapper. It mirrors agentscan's
// macos_implicit_auto_start_disabled_reason shape (AUR-329) so the
// operator gets a single actionable line that names a concrete
// recovery command. The opt-out env var / flag are NOT mentioned: on
// darwin the platform refuses regardless of those settings, so
// suggesting them would be misleading.
func MacOSImplicitAutoStartDisabled(intent StartIntent) (disabled bool, reason string) {
	if intent != IntentImplicitTUI {
		return false, ""
	}
	return true, "daemon is not running; macOS does not implicitly auto-start the tprompt daemon. Start the daemon in a long-lived tmux pane with 'tprompt daemon run' (or 'tprompt daemon start' for a detached signed-release binary). The TUI requires a running daemon on macOS."
}
