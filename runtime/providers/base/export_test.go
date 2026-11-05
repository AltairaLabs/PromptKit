package base

// ResetUnpricedWarnings forgets which unpriced-unit warnings were emitted, so
// tests that assert on the once-per-process warning pass under -count>1.
func ResetUnpricedWarnings() { unpricedWarnOnce.Clear() }
