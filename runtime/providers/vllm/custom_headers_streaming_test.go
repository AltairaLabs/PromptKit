package vllm

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestStreamingPath_CustomHeaderCollision exercises the streaming request
// builder's custom-header collision check.
func TestStreamingPath_CustomHeaderCollision(t *testing.T) {
	spec := providers.ProviderSpec{
		ID:      "test-vllm-stream",
		Type:    "vllm",
		Model:   "test-model",
		BaseURL: "http://127.0.0.1:1",
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
