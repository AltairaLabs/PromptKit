package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// claudeAPIResponse is a minimal Claude API response for testing.
const claudeAPIResponse = `{
	"id": "msg_test",
	"type": "message",
	"role": "assistant",
	"content": [{"type": "text", "text": "Hello from Bedrock"}],
	"model": "anthropic.claude-3-5-haiku-20241022-v1:0",
	"stop_reason": "end_turn",
	"usage": {"input_tokens": 10, "output_tokens": 5}
}`

// claudeToolCallResponse is a Claude API response with tool calls.
const claudeToolCallResponse = `{
	"id": "msg_test",
	"type": "message",
	"role": "assistant",
	"content": [
		{"type": "text", "text": "Let me search"},
		{"type": "tool_use", "id": "toolu_1", "name": "search", "input": {"q": "test"}}
	],
	"model": "anthropic.claude-3-5-haiku-20241022-v1:0",
	"stop_reason": "tool_use",
	"usage": {"input_tokens": 15, "output_tokens": 10}
}`

func newBedrockTestProvider(t *testing.T, response string) *Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)

	return &Provider{
		BaseProvider: providers.NewBaseProvider("test-bedrock", false, server.Client()),
		model:        "anthropic.claude-3-5-haiku-20241022-v1:0",
		baseURL:      server.URL,
		apiKey:       "test-key",
		platform:     "bedrock",
		defaults:     providers.ProviderDefaults{MaxTokens: 1024},
	}
}

func TestBedrockPredictStreamFallback(t *testing.T) {
	provider := newBedrockTestProvider(t, claudeAPIResponse)

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}
	ch, err := provider.bedrockPredictStreamFallback(context.Background(), &req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []providers.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != "Hello from Bedrock" {
		t.Errorf("expected content 'Hello from Bedrock', got %q", chunks[0].Content)
	}
	if chunks[0].FinishReason == nil || *chunks[0].FinishReason != "stop" {
		t.Errorf("expected finish reason 'stop'")
	}
}

func TestBedrockMultimodalStreamFallback(t *testing.T) {
	provider := newBedrockTestProvider(t, claudeAPIResponse)

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}
	ch, err := provider.bedrockMultimodalStreamFallback(context.Background(), &req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []providers.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != "Hello from Bedrock" {
		t.Errorf("expected content 'Hello from Bedrock', got %q", chunks[0].Content)
	}
}

func TestBedrockToolStreamFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(claudeToolCallResponse))
	}))
	defer server.Close()

	baseProvider := &Provider{
		BaseProvider: providers.NewBaseProvider("test-bedrock", false, server.Client()),
		model:        "anthropic.claude-3-5-haiku-20241022-v1:0",
		baseURL:      server.URL,
		apiKey:       "test-key",
		platform:     "bedrock",
		defaults:     providers.ProviderDefaults{MaxTokens: 1024},
	}
	toolProvider := &ToolProvider{Provider: baseProvider}

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "search for test"}},
		MaxTokens: 100,
	}
	ch, err := toolProvider.bedrockStreamFallback(context.Background(), &req, nil, "auto")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []providers.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if len(chunks[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(chunks[0].ToolCalls))
	}
	if chunks[0].ToolCalls[0].Name != "search" {
		t.Errorf("expected tool name 'search', got %q", chunks[0].ToolCalls[0].Name)
	}
}
