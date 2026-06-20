package stage

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func descriptorNames(ds []*providers.ToolDescriptor) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Name
	}
	return out
}

// Tools sent to a provider MUST be in a stable, sorted order across calls within
// a run. The tools array is part of the provider's cached prefix (Anthropic
// caches tools+system+first-message); capability tools are gathered in
// randomized Go map-iteration order, so without the sort the prefix changes
// every round, prompt caching never engages, and the full context is re-billed
// at full price every round (this cost a live codegen run ~$10). This guards the
// sort.
func TestCollectProviderDescriptors_DeterministicSortedOrder(t *testing.T) {
	reg := tools.NewRegistry()
	for _, n := range []string{
		"mcp__zebra", "mcp__alpha", "mcp__mike", "mcp__bravo",
		"mcp__yankee", "mcp__delta", "mcp__oscar", "mcp__charlie",
	} {
		if err := reg.Register(&tools.ToolDescriptor{
			Name: n, Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`),
		}); err != nil {
			t.Fatalf("register %s: %v", n, err)
		}
	}
	s := &ProviderStage{toolRegistry: reg}

	first := descriptorNames(s.collectProviderDescriptors(nil, nil))
	if len(first) != 8 {
		t.Fatalf("expected 8 descriptors, got %d: %v", len(first), first)
	}
	if !sort.StringsAreSorted(first) {
		t.Fatalf("descriptors must be sorted by name (cache stability), got %v", first)
	}
	// Map iteration order is randomized per range; every call must still agree.
	for i := range 100 {
		got := descriptorNames(s.collectProviderDescriptors(nil, nil))
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("non-deterministic tool order on call %d:\n got %v\nwant %v", i, got, first)
		}
	}
}
