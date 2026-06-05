package gemini

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestDeclaredCapabilities_OverrideDefaults verifies that a declared capability
// list is authoritative for Gemini's multimodal support: omitting audio/video
// turns them off even though Gemini's defaults enable them. With no
// declaration, the built-in defaults (all modalities) apply.
func TestDeclaredCapabilities_OverrideDefaults(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")

	build := func(caps []string) providers.MultimodalCapabilityProvider {
		spec := providers.ProviderSpec{
			ID: "test-caps", Type: "gemini", Model: "gemini-3.5-flash",
			BaseURL: "https://example.invalid", Capabilities: caps,
		}
		provider, err := providers.CreateProviderFromSpec(spec)
		if err != nil {
			t.Fatalf("CreateProviderFromSpec: %v", err)
		}
		return provider.(providers.MultimodalCapabilityProvider)
	}

	textOnly := build([]string{"text", "vision"}).GetMultimodalCapabilities()
	if textOnly.SupportsAudio {
		t.Error("omitting audio should disable audio support")
	}
	if textOnly.SupportsVideo {
		t.Error("omitting video should disable video support")
	}
	if !textOnly.SupportsImages {
		t.Error("declared vision should keep image support on")
	}

	defaults := build(nil).GetMultimodalCapabilities()
	if !defaults.SupportsAudio || !defaults.SupportsVideo || !defaults.SupportsImages {
		t.Error("default Gemini should support images, audio, and video")
	}
}
