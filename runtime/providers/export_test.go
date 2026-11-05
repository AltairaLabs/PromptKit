package providers

// resetWarnOnceState forgets which once-per-process provider warnings were
// emitted, so tests that assert on them pass under -count>1.
func resetWarnOnceState() {
	unsentParamOnce.Clear()
	extraBodyCollisionsWarned.Clear()
}
