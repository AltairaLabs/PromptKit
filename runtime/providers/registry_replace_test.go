package providers

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

func newNamedTestProvider(id string) *mockProviderForTest {
	return &mockProviderForTest{
		Implementation: *base.NewImplementation(id, base.ProviderTypeInference, nil),
		id:             id,
	}
}

// Re-registering an ID replaces the provider for every lookup: the SDK
// re-registers a conversation's media-scoped view of each pooled provider, and
// a typed lookup (mediagen's GetAll) must see that view, not the original.
func TestRegistry_RegisterReplacesInBothIndexes(t *testing.T) {
	r := NewRegistry()
	first := newNamedTestProvider("p")
	second := newNamedTestProvider("p")
	r.Register(first)
	r.Register(second)

	got, ok := r.Get("p")
	if !ok || got != Provider(second) {
		t.Fatalf("Get returned %p, want the replacement %p", got, second)
	}
	typed, err := r.GetByType("p", base.ProviderTypeInference)
	if err != nil {
		t.Fatalf("GetByType: %v", err)
	}
	if typed != base.Provider(second) {
		t.Fatalf("GetByType returned %p, want the replacement %p", typed, second)
	}
	all := r.GetAll(base.ProviderTypeInference)
	if len(all) != 1 || all[0] != base.Provider(second) {
		t.Fatalf("GetAll = %v, want only the replacement", all)
	}
}

// A replacement under the same ID but a different typed name drops the old
// typed entry, so GetAll never returns a provider Get can no longer reach.
func TestRegistry_RegisterDropsReplacedTypedEntry(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockProviderForTest{
		Implementation: *base.NewImplementation("old-name", base.ProviderTypeInference, nil),
		id:             "p",
	})
	replacement := &mockProviderForTest{
		Implementation: *base.NewImplementation("new-name", base.ProviderTypeInference, nil),
		id:             "p",
	}
	r.Register(replacement)

	if _, err := r.GetByType("old-name", base.ProviderTypeInference); err == nil {
		t.Fatal("the replaced provider's typed entry must be gone")
	}
	all := r.GetAll(base.ProviderTypeInference)
	if len(all) != 1 || all[0] != base.Provider(replacement) {
		t.Fatalf("GetAll = %v, want only the replacement", all)
	}
}
