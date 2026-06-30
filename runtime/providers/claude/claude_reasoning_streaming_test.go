package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestProvider_PredictStream_CapturesThinking verifies the streaming SSE parser
// routes thinking_delta to StreamChunk.Reasoning and signature_delta to
// OpaqueReasoning, keeping reasoning out of spoken content. Hermetic (httptest).
func TestProvider_PredictStream_CapturesThinking(t *testing.T) {
	const sse = "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"weigh the options\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-abc\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"the answer\"}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sse))
	}))
	t.Cleanup(server.Close)

	p := &Provider{
		BaseProvider: providers.NewBaseProvider("test", false, server.Client()),
		model:        "claude-sonnet-4-5",
		baseURL:      server.URL,
		apiKey:       "test-key",
		defaults:     providers.ProviderDefaults{MaxTokens: 256},
	}

	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "decide"}},
	})
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}

	var reasoning, content string
	var signature string
	for chunk := range ch {
		reasoning += chunk.Reasoning
		if chunk.Content != "" {
			content = chunk.Content // accumulated spoken text
		}
		for _, o := range chunk.OpaqueReasoning {
			if o.Kind == thinkingSignatureKind {
				signature = o.Data
			}
		}
	}

	if reasoning != "weigh the options" {
		t.Fatalf("reasoning = %q, want %q", reasoning, "weigh the options")
	}
	if signature != "sig-abc" {
		t.Fatalf("signature opaque token = %q, want %q", signature, "sig-abc")
	}
	if !strings.Contains(content, "the answer") {
		t.Fatalf("content = %q, want it to contain the spoken answer", content)
	}
	if strings.Contains(content, "weigh the options") {
		t.Fatalf("reasoning leaked into content: %q", content)
	}
}

// TestBuildBaseRequest_ExtendedThinking verifies the request-enable wiring: a
// configured thinking budget attaches the thinking block, omits temperature
// (Claude rejects a custom one with thinking), and guarantees answer headroom.
func TestBuildBaseRequest_ExtendedThinking(t *testing.T) {
	budget := 2048
	p := &Provider{
		model:          "claude-sonnet-4-5",
		defaults:       providers.ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
		thinkingBudget: &budget,
	}
	cr := p.buildBaseRequest(providers.PredictionRequest{Temperature: 0.7, MaxTokens: 1024}, nil)

	if cr.Thinking == nil {
		t.Fatal("thinking block must be set when a budget is configured")
	}
	if cr.Thinking.Type != "enabled" || cr.Thinking.BudgetTokens != 2048 {
		t.Fatalf("thinking = %+v, want {enabled 2048}", cr.Thinking)
	}
	if cr.MaxTokens <= cr.Thinking.BudgetTokens {
		t.Fatalf("max_tokens (%d) must exceed budget (%d) for answer headroom", cr.MaxTokens, cr.Thinking.BudgetTokens)
	}
	if cr.Temperature != 0 {
		t.Fatalf("temperature must be omitted with thinking, got %v", cr.Temperature)
	}
}

// TestApplyThinkingConfig reads the budget from additional_config.
func TestApplyThinkingConfig(t *testing.T) {
	p := &Provider{}
	applyThinkingConfig(p, providers.ProviderSpec{
		AdditionalConfig: map[string]any{"thinking_budget": 1024},
	})
	if p.thinkingBudget == nil || *p.thinkingBudget != 1024 {
		t.Fatalf("thinkingBudget = %v, want 1024", p.thinkingBudget)
	}

	none := &Provider{}
	applyThinkingConfig(none, providers.ProviderSpec{})
	if none.thinkingBudget != nil {
		t.Fatal("thinkingBudget must stay nil without config")
	}
}
