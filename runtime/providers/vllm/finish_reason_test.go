package vllm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestVLLM_Predict_NormalizesFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := vllmChatResponse{
			Model: "test-model",
			Choices: []vllmChatChoice{{
				Index:        0,
				Message:      vllmMessage{Role: "assistant", Content: "partial"},
				FinishReason: "length",
			}},
			Usage: vllmUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL, providers.ProviderDefaults{MaxTokens: 100}, false, nil)
	resp, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}
	if resp.FinishReason != types.FinishReasonMaxOutputTokens {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, types.FinishReasonMaxOutputTokens)
	}
}
