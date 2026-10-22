package openai

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// TestCreateProviderFromSpec_MessageMetadataTemplateSurvivesMediaView checks a
// provider embedding BaseProvider gets the spec's template, and that the
// per-conversation view WithMediaSettings returns keeps it.
func TestCreateProviderFromSpec_MessageMetadataTemplateSurvivesMediaView(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "o", Type: "openai", Model: "gpt-4o-mini", MessageMetadataTemplate: "[{{selection}}]",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ms, ok := p.(providers.MediaScoped)
	if !ok {
		t.Fatalf("%T is not MediaScoped", p)
	}
	for name, prov := range map[string]providers.Provider{
		"provider": p,
		"view":     ms.WithMediaSettings(providers.MediaSettings{}),
	} {
		mt, ok := prov.(providers.MessageMetadataTemplater)
		if !ok {
			t.Fatalf("%s: %T does not implement MessageMetadataTemplater", name, prov)
		}
		if got := mt.MessageMetadataTemplate(); got != "[{{selection}}]" {
			t.Fatalf("%s: template = %q", name, got)
		}
	}
}
