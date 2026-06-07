package gemini

import (
	"net/http"
	"testing"
)

func TestGeminiNewEmbeddingProvider_PlatformAuthSkipsKeyGuard(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	p, err := NewEmbeddingProvider(
		WithGeminiEmbeddingHTTPClient(&http.Client{}),
		WithGeminiEmbeddingPlatformAuth(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("provider is nil")
	}
}
