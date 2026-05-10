// Package envutil holds small, dependency-free helpers for parsing
// environment-variable values consistently across the codebase. Only
// utilities with a single shared contract live here — anything with
// per-caller policy belongs near the caller.
package envutil

import "strings"

// TruthyOf reports whether the supplied value is a known-positive
// truthy form. Callers that already hold the raw env value (e.g.,
// because they need to echo it in an error message or audit log)
// should call this directly and skip Truthy's getenv indirection.
//
// Whitespace is trimmed before comparison; recognized values are
// 1, true, yes, on (case-insensitive). Empty or unrecognized values
// return false so a typo cannot silently flip behavior — callers that
// want to flag a malformed non-empty value should compare the raw
// value to "" themselves.
func TruthyOf(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Truthy is the getenv-based convenience wrapper around TruthyOf for
// the common case where the caller does not also need the raw value.
// A nil getenv is treated as "no env" and returns false. Both calls
// honor the same single shared promise across every TPROMPT_* boolean
// env var so the parsing rule cannot drift.
func Truthy(getenv func(string) string, key string) bool {
	if getenv == nil {
		return false
	}
	return TruthyOf(getenv(key))
}
