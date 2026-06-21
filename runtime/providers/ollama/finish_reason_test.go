package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestOllama_Predict_NormalizesFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := ollamaResponse{
			Model: "llama2",
			Choices: []ollamaChoice{{
				Index:        0,
				Message:      ollamaMessage{Role: "assistant", Content: "partial"},
				FinishReason: "length",
			}},
			Usage: ollamaUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "llama2", server.URL, providers.ProviderDefaults{MaxTokens: 100}, false, nil)
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
