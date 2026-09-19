package stage

import "testing"

// TestProviderStage_StaysComparable pins the property the release gate checks:
// ProviderStage is an exported type in a published module, so a field that
// makes the struct non-comparable is an incompatible API change and blocks
// every subsequent minor release. A map field did exactly that (#2016, caught
// cutting v2.4.0); the set now lives behind a pointer.
//
// This fails to COMPILE, not at runtime, if someone adds a slice, map, func or
// non-comparable struct field directly to ProviderStage.
func TestProviderStage_StaysComparable(t *testing.T) {
	var a, b ProviderStage
	if a != b { //nolint:staticcheck // the comparison itself is the assertion
		t.Fatal("two zero ProviderStage values should be equal")
	}
}
