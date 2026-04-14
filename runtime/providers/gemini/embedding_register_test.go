package gemini_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"

	// Side-effect import: trigger our embedding factory registration.
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
)

func TestEmbeddingRegister_AllOptions(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("stub")
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:       "gemini",
		Model:      "text-embedding-004",
		BaseURL:    "https://gemini.test",
		Credential: cred,
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if p == nil {
		t.Fatal("nil provider")
	}
}

func TestEmbeddingRegister_Defaults(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "stub")
	if _, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "gemini",
	}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}
