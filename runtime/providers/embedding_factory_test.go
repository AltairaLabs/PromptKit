package providers_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"

	// Side-effect imports register the factories under test.
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/ollama"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/voyageai"
)

// Each per-provider test exercises every conditional branch of the
// registered factory closure (model, base_url, credential, and any
// provider-specific extras) so coverage on the embedding_register.go
// files stays above the 80% gate. New optional fields require
// extending the corresponding test.

func TestCreateEmbeddingProviderFromSpec_OpenAI(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("sk-stub")
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		ID:         "rag",
		Type:       "openai",
		Model:      "text-embedding-3-small",
		BaseURL:    "https://example.test",
		Credential: cred,
	})
	if err != nil {
		t.Fatalf("CreateEmbeddingProviderFromSpec: %v", err)
	}
	if p == nil {
		t.Fatal("nil provider")
	}
}

func TestCreateEmbeddingProviderFromSpec_OpenAI_Defaults(t *testing.T) {
	// Hit the "no model / no base_url / no credential" branch.
	t.Setenv("OPENAI_API_KEY", "sk-fallback")
	if _, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "openai",
	}); err != nil {
		t.Fatalf("CreateEmbeddingProviderFromSpec: %v", err)
	}
}

func TestCreateEmbeddingProviderFromSpec_Gemini(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("stub")
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:       "gemini",
		Model:      "text-embedding-004",
		BaseURL:    "https://gemini.test",
		Credential: cred,
	})
	if err != nil {
		t.Fatalf("CreateEmbeddingProviderFromSpec: %v", err)
	}
	if p == nil {
		t.Fatal("nil provider")
	}
}

func TestCreateEmbeddingProviderFromSpec_Gemini_Defaults(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "stub")
	if _, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "gemini",
	}); err != nil {
		t.Fatalf("CreateEmbeddingProviderFromSpec: %v", err)
	}
}

func TestCreateEmbeddingProviderFromSpec_VoyageAI(t *testing.T) {
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
		t.Fatalf("CreateEmbeddingProviderFromSpec: %v", err)
	}
	if p == nil {
		t.Fatal("nil provider")
	}
}

func TestCreateEmbeddingProviderFromSpec_VoyageAI_Defaults(t *testing.T) {
	// All optional fields omitted exercises the "skip" branches.
	t.Setenv("VOYAGE_API_KEY", "stub")
	if _, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "voyageai",
	}); err != nil {
		t.Fatalf("CreateEmbeddingProviderFromSpec: %v", err)
	}
}

func TestCreateEmbeddingProviderFromSpec_Ollama(t *testing.T) {
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:    "ollama",
		Model:   "nomic-embed-text",
		BaseURL: "http://ollama.test",
		AdditionalConfig: map[string]any{
			"dimensions": int64(768),
		},
	})
	if err != nil {
		t.Fatalf("CreateEmbeddingProviderFromSpec: %v", err)
	}
	if p == nil {
		t.Fatal("nil provider")
	}
}

func TestCreateEmbeddingProviderFromSpec_Ollama_Defaults(t *testing.T) {
	if _, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "ollama",
	}); err != nil {
		t.Fatalf("CreateEmbeddingProviderFromSpec: %v", err)
	}
}

func TestCreateEmbeddingProviderFromSpec_UnknownType(t *testing.T) {
	_, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "no-such-provider",
	})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestCreateEmbeddingProviderFromSpec_OpenAIWithAPIKey(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("sk-test-key")
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:       "openai",
		Credential: cred,
	})
	if err != nil {
		t.Fatalf("CreateEmbeddingProviderFromSpec: %v", err)
	}
	if p == nil {
		t.Fatal("nil provider")
	}
}

func TestRegisterEmbeddingProviderFactory_OverwritesSilently(t *testing.T) {
	called := false
	providers.RegisterEmbeddingProviderFactory("test-overwrite",
		func(_ providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			called = true
			return nil, nil
		},
	)
	_, _ = providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{Type: "test-overwrite"})
	if !called {
		t.Fatal("registered factory was not called")
	}
}

func TestAPIKeyFromCredential(t *testing.T) {
	if got := providers.APIKeyFromCredential(nil); got != "" {
		t.Errorf("nil credential should yield empty key, got %q", got)
	}
	if got := providers.APIKeyFromCredential(&credentials.NoOpCredential{}); got != "" {
		t.Errorf("noop credential should yield empty key, got %q", got)
	}
	cred := credentials.NewAPIKeyCredential("sk-abc")
	if got := providers.APIKeyFromCredential(cred); got != "sk-abc" {
		t.Errorf("APIKey credential = %q, want sk-abc", got)
	}
}

func TestIntFromConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		key  string
		want int
		ok   bool
	}{
		{"missing", map[string]any{}, "k", 0, false},
		{"int", map[string]any{"k": 5}, "k", 5, true},
		{"int64", map[string]any{"k": int64(7)}, "k", 7, true},
		{"float64", map[string]any{"k": 3.0}, "k", 3, true},
		{"wrong type", map[string]any{"k": "5"}, "k", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := providers.IntFromConfig(tc.cfg, tc.key)
			if got != tc.want || ok != tc.ok {
				t.Errorf("IntFromConfig = (%d,%v), want (%d,%v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestResolveEmbeddingCredential_FromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-env-test")
	cred, err := providers.ResolveEmbeddingCredential(context.Background(), "openai", "", nil)
	if err != nil {
		t.Fatalf("ResolveEmbeddingCredential: %v", err)
	}
	if got := providers.APIKeyFromCredential(cred); got != "sk-env-test" {
		t.Errorf("resolved key = %q, want sk-env-test", got)
	}
}
