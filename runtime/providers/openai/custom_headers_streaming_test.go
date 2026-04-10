package openai

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestStreamingPath_CustomHeaderCollision exercises the streaming request
// builder's custom-header collision check. It fails fast before any
// network I/O happens, so it doesn't need an httptest server.
func TestStreamingPath_CustomHeaderCollision(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	spec := providers.ProviderSpec{
		ID:      "test-openai-stream",
		Type:    "openai",
		Model:   "gpt-4o-mini",
		BaseURL: "https://example.invalid",
		Headers: map[string]string{
			"Authorization": "Bearer conflict",
		},
	}

	provider, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}

	_, err = provider.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("expected collision error from PredictStream, got nil")
	}
}
