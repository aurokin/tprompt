//go:build !darwin

package lifecycle

// ProductionAssessor on non-darwin returns the no-op assessor: the
// macOS executable-trust gate has no analogue on Linux/BSD/Windows.
// The TPROMPT_UNSAFE_SKIP_TRUST_GATE override is darwin-only and has
// nothing to bypass here.
func ProductionAssessor() TrustAssessor {
	return noopAssessor{}
}
