//go:build !darwin

package lifecycle

// MacOSImplicitAutoStartDisabled is a no-op outside darwin: implicit
// auto-start is permitted on Linux and other supported platforms.
// The two-return shape matches the darwin build so callers can be
// build-tag-free.
func MacOSImplicitAutoStartDisabled(StartIntent) (bool, string) {
	return false, ""
}
