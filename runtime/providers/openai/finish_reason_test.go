package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func newFinishReasonTestProvider(baseURL string) *Provider {
	return &Provider{
		BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
		model:        "gpt-4",
		baseURL:      baseURL,
		apiKey:       "test-key",
		defaults: providers.ProviderDefaults{
			Pricing: providers.Pricing{InputCostPer1K: 0.03, OutputCostPer1K: 0.06},
		},
	}
}

func TestOpenAI_Predict_NormalizesFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := openAIResponse{
			Model: "gpt-4",
			Choices: []openAIChoice{{
				Index:        0,
				Message:      openAIMessage{Role: "assistant", Content: "partial"},
				FinishReason: "length",
			}},
			Usage: openAIUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := newFinishReasonTestProvider(server.URL)
	resp, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Predict returned error: %v", err)
	}
	if resp.FinishReason != types.FinishReasonMaxOutputTokens {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, types.FinishReasonMaxOutputTokens)
	}
}

func TestOpenAI_PredictStream_NormalizesFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		chunks := []string{
			`data: {"choices":[{"delta":{"content":"partial"}}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			_, _ = w.Write([]byte(chunk + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := newFinishReasonTestProvider(server.URL)
	streamChan, err := provider.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("PredictStream returned error: %v", err)
	}

	var last providers.StreamChunk
	for chunk := range streamChan {
		if chunk.FinishReason != nil {
			last = chunk
		}
	}
	if last.FinishReason == nil {
		t.Fatal("expected a final chunk carrying a finish reason")
	}
	if *last.FinishReason != types.FinishReasonMaxOutputTokens {
		t.Errorf("FinishReason = %q, want %q", *last.FinishReason, types.FinishReasonMaxOutputTokens)
	}
}
