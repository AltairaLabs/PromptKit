package openai_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"

	// Side-effect import: trigger our embedding factory registration.
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)

// TestEmbeddingRegister_AllOptions exercises every conditional branch
// in the registered openai embedding factory. Coverage is measured
// per-package, so this lives here rather than in
// runtime/providers/embedding_factory_test.go.
func TestEmbeddingRegister_AllOptions(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("sk-stub")
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:       "openai",
		Model:      "text-embedding-3-small",
		BaseURL:    "https://example.test",
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
	t.Setenv("OPENAI_API_KEY", "sk-fallback")
	if _, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "openai",
	}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}
