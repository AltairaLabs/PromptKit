package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

type fakeCred struct{ applied bool }

func (f *fakeCred) Apply(_ context.Context, req *http.Request) error {
	f.applied = true
	req.Header.Set("Authorization", "Bearer faketoken")
	return nil
}
func (f *fakeCred) Type() string { return "fake" }

// NewEmbeddingProvider must succeed with no API key when platform auth is active.
func TestNewEmbeddingProvider_PlatformAuthSkipsKeyGuard(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_TOKEN", "")
	p, err := NewEmbeddingProvider(
		WithEmbeddingBaseURL("https://x/openai/deployments/m"),
		WithEmbeddingHTTPClient(&http.Client{}),
		WithEmbeddingPlatformAuth(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("provider is nil")
	}
}

// Without platform auth and without a key, the guard still fires.
func TestNewEmbeddingProvider_NoKeyStillErrors(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_TOKEN", "")
	if _, err := NewEmbeddingProvider(); err == nil {
		t.Fatal("expected API-key-not-found error")
	}
}

// The openai factory, given an Azure platform spec, produces a provider
// that injects the credential's bearer token + api-version on Embed().
func TestOpenAIEmbeddingFactory_AzurePlatform(t *testing.T) {
	var gotAuth, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(embeddingResponse{
			Object: "list",
			Data:   []embeddingData{{Object: "embedding", Index: 0, Embedding: fixtureVector(dimensions3Small, 0.1, 0.2)}},
			Model:  "text-embedding-3-small",
			Usage:  embeddingUsage{PromptTokens: 1, TotalTokens: 1},
		})
	}))
	defer srv.Close()

	emb, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:           "openai",
		Model:          "text-embedding-3-small",
		Platform:       "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: srv.URL},
		Credential:     &fakeCred{},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = emb.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hello"}})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if gotAuth != "Bearer faketoken" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if !strings.Contains(gotQuery, "api-version=") {
		t.Fatalf("query = %q, want api-version", gotQuery)
	}
}
