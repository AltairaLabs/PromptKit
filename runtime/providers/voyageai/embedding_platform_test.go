package voyageai

import (
	"net/http"
	"testing"
)

func TestVoyageNewEmbeddingProvider_PlatformAuthSkipsKeyGuard(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "")
	p, err := NewEmbeddingProvider(
		WithHTTPClient(&http.Client{}),
		WithPlatformAuth(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("provider is nil")
	}
}
