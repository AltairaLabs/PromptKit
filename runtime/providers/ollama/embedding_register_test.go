package ollama_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"

	// Side-effect import: trigger our embedding factory registration.
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/ollama"
)

func TestEmbeddingRegister_AllOptions(t *testing.T) {
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:    "ollama",
		Model:   "nomic-embed-text",
		BaseURL: "http://ollama.test",
		AdditionalConfig: map[string]any{
			"dimensions": int64(768),
		},
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if p == nil {
		t.Fatal("nil provider")
	}
}

func TestEmbeddingRegister_Defaults(t *testing.T) {
	if _, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "ollama",
	}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}
