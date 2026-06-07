package sdk

import (
	"net/http"
	"net/http/httptest"
	"testing"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/config"
)

// A declarative embedding provider with platform=azure must build a
// platform-aware provider (no API key required). We assert construction
// succeeds with no credentials set — the static path would error
// "API key not found".
func TestApplyEmbeddingProviders_AzurePlatform(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_TOKEN", "")

	c := &config{}
	err := applyEmbeddingProviders(c, []pkgconfig.EmbeddingProviderConfig{{
		ID:    "rag",
		Type:  "openai",
		Model: "text-embedding-3-small",
		Platform: &pkgconfig.PlatformConfig{
			Type:     "azure",
			Endpoint: srv.URL,
		},
	}})
	if err != nil {
		t.Fatalf("applyEmbeddingProviders error: %v", err)
	}
	if _, ok := c.embeddingProviders["rag"]; !ok {
		t.Fatal("embedding provider not registered")
	}
}
