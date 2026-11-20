package base

// ResetUnpricedWarnings forgets which unpriced-unit and unknown-model warnings
// were emitted, so tests that assert on the once-per-process warnings pass
// under -count>1.
func ResetUnpricedWarnings() {
	unpricedWarnOnce.Clear()
	unknownPricingWarnOnce.Clear()
	unknownWindowWarnOnce.Clear()
}
