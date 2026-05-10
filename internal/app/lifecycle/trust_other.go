//go:build !darwin

package lifecycle

// ProductionAssessor on non-darwin returns the no-op assessor: the
// macOS executable-trust gate has no analogue on Linux/BSD/Windows.
func ProductionAssessor() TrustAssessor {
	return noopAssessor{}
}
