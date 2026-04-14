package voyageai_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"

	// Side-effect import: trigger our embedding factory registration.
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/voyageai"
)

func TestEmbeddingRegister_AllOptions(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("stub")
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:       "voyageai",
		Model:      "voyage-3",
		BaseURL:    "https://voyage.test",
		Credential: cred,
		AdditionalConfig: map[string]any{
			"dimensions": 1024,
			"input_type": "query",
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
	t.Setenv("VOYAGE_API_KEY", "stub")
	if _, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "voyageai",
	}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}
