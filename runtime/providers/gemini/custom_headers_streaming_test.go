package gemini

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestStreamingPath_CustomHeaderCollision exercises the streaming request
// builder's custom-header collision check.
func TestStreamingPath_CustomHeaderCollision(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "test-key")

	spec := providers.ProviderSpec{
		ID:      "test-gemini-stream",
		Type:    "gemini",
		Model:   "gemini-1.5-flash",
		BaseURL: "https://example.invalid",
		Headers: map[string]string{
			"Content-Type": "text/plain",
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
