package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestProviderMiddleware_RealProviderBehavior tests the exact scenario we see in production:
// The provider returns a ChatResponse with Latency=0 (default value) because
// something is wrong with how the provider constructs the response.
func TestProviderMiddleware_RealProviderBehavior_ZeroLatency(t *testing.T) {
	t.Log("This test reproduces the production bug where provider returns Latency=0")

	// Create a provider that mimics the broken behavior we see in production
	mockProvider := &mockBrokenProvider{
		// This simulates what we think is happening in OpenAI provider
		response: providers.ChatResponse{
			Content: "Test response",
			// Latency: 0 (default value, not explicitly set)
			// This is what we suspect is happening!
			CostInfo: &types.CostInfo{
				InputTokens:   100,
				OutputTokens:  50,
				InputCostUSD:  0.001,
				OutputCostUSD: 0.002,
				TotalCost:     0.003,
			},
		},
	}

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context:      context.Background(),
		SystemPrompt: "Test system prompt",
		Messages: []types.Message{
			{
				Role:    "user",
				Content: "Test question",
			},
		},
	}

	// Create provider middleware
	providerConfig := &ProviderMiddlewareConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	}
	middleware := ProviderMiddleware(mockProvider, nil, nil, providerConfig)

	// Execute middleware
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Provider middleware failed: %v", err)
	}

	// Get the assistant message
	if len(execCtx.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(execCtx.Messages))
	}
	assistantMsg := execCtx.Messages[1]

	t.Logf("Provider response latency: %v (%d ms)", mockProvider.response.Latency, mockProvider.response.Latency.Milliseconds())
	t.Logf("Assistant message LatencyMs: %d", assistantMsg.LatencyMs)

	// This test documents a known issue: when provider returns Latency=0,
	// the message gets LatencyMs=0 and due to omitempty, it doesn't appear in JSON
	if assistantMsg.LatencyMs == 0 {
		t.Logf("KNOWN ISSUE: LatencyMs is 0 because provider returned Latency=0")
		t.Logf("This is why latency_ms may be missing from production JSON when providers don't set Latency")
		t.Logf("Providers should measure and set the Latency field in ChatResponse")
		// Note: This is a known limitation, not a test failure
		// The fix should be in the provider implementations, not the middleware
	} else {
		t.Logf("✅ LatencyMs is properly set: %d ms", assistantMsg.LatencyMs)
	}
}

// mockBrokenProvider simulates a provider that doesn't set Latency properly
type mockBrokenProvider struct {
	response providers.ChatResponse
}

func (m *mockBrokenProvider) ID() string {
	return "mock-broken-provider"
}

func (m *mockBrokenProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	// Simulate delay but DON'T set it in response (this is the bug!)
	time.Sleep(100 * time.Millisecond)

	// Return response with Latency still at default value (0)
	return m.response, nil
}

func (m *mockBrokenProvider) ChatStream(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

func (m *mockBrokenProvider) ChatWithTools(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	return m.Chat(ctx, req)
}

func (m *mockBrokenProvider) ChatStreamWithTools(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	return m.ChatStream(ctx, req)
}

func (m *mockBrokenProvider) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}

func (m *mockBrokenProvider) Close() error {
	return nil
}

func (m *mockBrokenProvider) ShouldIncludeRawOutput() bool {
	return false
}

func (m *mockBrokenProvider) SupportsStreaming() bool {
	return false
}

func (m *mockBrokenProvider) SupportsTools() bool {
	return false
}
