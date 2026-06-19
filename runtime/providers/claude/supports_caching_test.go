package claude

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestSupportsCaching_DefaultOnForCurrentModels verifies that supportsCaching returns true
// for all current Anthropic models when DisablePromptCaching is not set.
func TestSupportsCaching_DefaultOnForCurrentModels(t *testing.T) {
	currentModels := []string{
		"claude-haiku-4-5",
		"claude-opus-4-8",
		"claude-sonnet-4-6",
		"claude-future-model", // arbitrary unknown model — should default to on
	}

	for _, model := range currentModels {
		t.Run(model, func(t *testing.T) {
			p := &Provider{
				model:    model,
				defaults: providers.ProviderDefaults{},
			}
			if !p.supportsCaching() {
				t.Errorf("supportsCaching() = false for model %q; want true (caching should be on by default)", model)
			}
		})
	}
}

// TestSupportsCaching_DisabledWhenFlagSet verifies that setting DisablePromptCaching: true
// disables caching regardless of the model.
func TestSupportsCaching_DisabledWhenFlagSet(t *testing.T) {
	models := []string{
		"claude-haiku-4-5",
		"claude-opus-4-8",
		"claude-sonnet-4-6",
		"claude-3-5-sonnet-20241022", // previously hard-coded to true
		"claude-3-opus-20240229",     // previously hard-coded to true
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			p := &Provider{
				model: model,
				defaults: providers.ProviderDefaults{
					DisablePromptCaching: true,
				},
			}
			if p.supportsCaching() {
				t.Errorf("supportsCaching() = true for model %q with DisablePromptCaching=true; want false", model)
			}
		})
	}
}
