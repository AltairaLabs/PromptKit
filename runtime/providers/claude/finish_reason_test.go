package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func newFinishReasonTestProvider(baseURL string) *Provider {
	return &Provider{
		BaseProvider: providers.NewBaseProvider("test", false, &http.Client{}),
		model:        "claude-3-opus",
		baseURL:      baseURL,
		apiKey:       "test-key",
		defaults: providers.ProviderDefaults{
			MaxTokens: 100,
			Pricing:   providers.Pricing{InputCostPer1K: 0.003, OutputCostPer1K: 0.015},
		},
	}
}

func TestClaude_Predict_NormalizesFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_1","type":"message","role":"assistant",
			"content":[{"type":"text","text":"partial"}],
			"model":"claude-3-opus","stop_reason":"max_tokens",
			"usage":{"input_tokens":10,"output_tokens":5}
		}`))
	}))
	defer server.Close()

	provider := newFinishReasonTestProvider(server.URL)
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

func TestClaude_PredictStream_NormalizesFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n",
		}
		for _, e := range events {
			_, _ = w.Write([]byte(e))
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := newFinishReasonTestProvider(server.URL)
	streamChan, err := provider.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("PredictStream failed: %v", err)
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

func TestNormalizeFinishReason(t *testing.T) {
	cases := map[string]string{
		"end_turn":      types.FinishReasonStop,
		"stop_sequence": types.FinishReasonStop,
		"pause_turn":    types.FinishReasonStop,
		"max_tokens":    types.FinishReasonMaxOutputTokens,
		"tool_use":      types.FinishReasonToolUse,
		"refusal":       types.FinishReasonRefusal,
		"":              "",
		"future_reason": "future_reason",
	}
	for raw, want := range cases {
		if got := normalizeFinishReason(raw); got != want {
			t.Errorf("normalizeFinishReason(%q) = %q, want %q", raw, got, want)
		}
	}
}
