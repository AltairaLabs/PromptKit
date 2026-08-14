package providers

import (
	"slices"
	"testing"
)

// TestRegisteredProviderTypes asserts the lister reads the live completion
// (chat) factory map rather than a fresh map or a construction-time snapshot.
// This is the registry WithLLMProvider/WithImageProvider resolve against, so
// it is what a caller must consult to know whether those options will
// construct.
func TestRegisteredProviderTypes(t *testing.T) {
	const fake = "fakelisted"
	original, had := providerFactories[fake]
	t.Cleanup(func() {
		if had {
			providerFactories[fake] = original
			return
		}
		delete(providerFactories, fake)
	})

	before := RegisteredProviderTypes()
	if slices.Contains(before, fake) {
		t.Fatalf("RegisteredProviderTypes() = %v, already contains %q", before, fake)
	}
	if !slices.IsSorted(before) {
		t.Errorf("RegisteredProviderTypes() = %v, not sorted", before)
	}

	RegisterProviderFactory(fake, func(_ ProviderSpec) (Provider, error) { return nil, nil })

	after := RegisteredProviderTypes()
	if !slices.Contains(after, fake) {
		t.Errorf("after registration, RegisteredProviderTypes() = %v, missing %q", after, fake)
	}
	if !slices.IsSorted(after) {
		t.Errorf("RegisteredProviderTypes() = %v, not sorted", after)
	}
}
