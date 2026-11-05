package ollama

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func TestMaxContextTokens_OverrideAndUnknown(t *testing.T) {
	spec := providers.ProviderSpec{ID: "o", Type: "ollama", Model: "llama3", BaseURL: "http://localhost:11434"}
	p, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.(providers.ContextWindowProvider).MaxContextTokens(); got != 0 {
		t.Errorf("no override: window = %d, want 0 (caller default)", got)
	}
	spec.AdditionalConfig = map[string]any{"context_window": 32768}
	p, err = providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.(providers.ContextWindowProvider).MaxContextTokens(); got != 32768 {
		t.Errorf("override: window = %d, want 32768", got)
	}
}
