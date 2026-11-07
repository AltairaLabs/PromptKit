package openai

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// TestCapabilitiesWithoutTools_StillSupportTools pins that "tools" is a
// matching label only: a provider config whose capabilities list omits it
// still builds a provider that is offered tools, while capability matching
// reports the requirement as unmet.
func TestCapabilitiesWithoutTools_StillSupportTools(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	declared := []string{providers.CapabilityText}
	prov, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "text-only", Type: "openai", Model: "gpt-4o",
		BaseURL: "https://api.openai.com/v1", Capabilities: declared,
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	if _, ok := prov.(providers.ToolSupport); !ok {
		t.Fatalf("%T does not implement ToolSupport; a capabilities list must not remove tool support", prov)
	}
	if providers.HasCapabilities(declared, []string{providers.CapabilityTools}) {
		t.Error("matching a tools requirement against [text] should report it unmet")
	}
}
