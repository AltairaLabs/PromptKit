package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestProviderMiddleware_AssistantMessageLatency verifies that assistant messages
// have their LatencyMs field populated from the provider's response latency.
// This test reproduces the bug where assistant messages in production have null latency_ms.
func TestProviderMiddleware_AssistantMessageLatency(t *testing.T) {
	// Create a mock provider that returns a response with explicit latency
	mockProvider := &mockProviderWithLatency{
		response: providers.ChatResponse{
			Content: "Test response",
			Latency: 1234 * time.Millisecond, // Explicit latency value
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

	// Verify we have 2 messages now (user + assistant)
	if len(execCtx.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(execCtx.Messages))
	}

	// Get the assistant message (last message)
	assistantMsg := execCtx.Messages[1]

	// Verify it's an assistant message
	if assistantMsg.Role != "assistant" {
		t.Errorf("Expected assistant message, got role: %s", assistantMsg.Role)
	}

	// Verify content
	if assistantMsg.Content != "Test response" {
		t.Errorf("Expected 'Test response', got: %s", assistantMsg.Content)
	}

	// Verify cost info is present
	if assistantMsg.CostInfo == nil {
		t.Fatal("Expected cost info to be present")
	}

	// CRITICAL TEST: Verify latency is populated
	if assistantMsg.LatencyMs == 0 {
		t.Errorf("LATENCY BUG: Expected LatencyMs to be 1234, got 0")
		t.Logf("Provider response latency was: %v", mockProvider.response.Latency)
		t.Logf("Provider response latency in ms: %d", mockProvider.response.Latency.Milliseconds())
	} else {
		t.Logf("SUCCESS: LatencyMs is set to %d", assistantMsg.LatencyMs)
	}

	expectedLatencyMs := int64(1234)
	if assistantMsg.LatencyMs != expectedLatencyMs {
		t.Errorf("Expected LatencyMs to be %d, got %d", expectedLatencyMs, assistantMsg.LatencyMs)
		t.Logf("Provider response: %+v", mockProvider.response)
		t.Logf("Assistant message: %+v", assistantMsg)
	} else {
		t.Logf("VERIFIED: LatencyMs matches expected value %d", expectedLatencyMs)
	}
}

// TestProviderMiddleware_AssistantMessageLatency_WithoutCostInfo verifies that
// latency is set even when cost info is not available.
func TestProviderMiddleware_AssistantMessageLatency_WithoutCostInfo(t *testing.T) {
	// Create a mock provider that returns a response with latency but NO cost info
	mockProvider := &mockProviderWithLatency{
		response: providers.ChatResponse{
			Content:  "Test response without cost",
			Latency:  567 * time.Millisecond,
			CostInfo: nil, // No cost info
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

	// Verify cost info is NOT present
	if assistantMsg.CostInfo != nil {
		t.Error("Expected cost info to be nil when provider doesn't return it")
	}

	// CRITICAL: Latency should STILL be set even without cost info
	expectedLatencyMs := int64(567)
	if assistantMsg.LatencyMs != expectedLatencyMs {
		t.Errorf("Expected LatencyMs to be %d even without cost info, got %d", expectedLatencyMs, assistantMsg.LatencyMs)
		t.Logf("This is the bug we're trying to fix - latency should be independent of cost info")
	}
}

// mockProviderWithLatency is a test provider that returns a configurable response
type mockProviderWithLatency struct {
	response  providers.ChatResponse
	callCount int
}

func (m *mockProviderWithLatency) ID() string {
	return "mock-provider-latency-test"
}

func (m *mockProviderWithLatency) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	m.callCount++
	// Simulate some delay
	time.Sleep(10 * time.Millisecond)
	return m.response, nil
}

func (m *mockProviderWithLatency) ChatStream(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	// Not needed for this test
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

func (m *mockProviderWithLatency) ChatWithTools(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	// Just delegate to Chat for testing
	return m.Chat(ctx, req)
}

func (m *mockProviderWithLatency) ChatStreamWithTools(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	return m.ChatStream(ctx, req)
}

func (m *mockProviderWithLatency) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	return types.CostInfo{
		InputTokens:   tokensIn,
		OutputTokens:  tokensOut,
		CachedTokens:  cachedTokens,
		InputCostUSD:  float64(tokensIn) * 0.00001,
		OutputCostUSD: float64(tokensOut) * 0.00002,
		CachedCostUSD: float64(cachedTokens) * 0.000005,
		TotalCost:     float64(tokensIn)*0.00001 + float64(tokensOut)*0.00002 + float64(cachedTokens)*0.000005,
	}
}

func (m *mockProviderWithLatency) Close() error {
	return nil
}

func (m *mockProviderWithLatency) ShouldIncludeRawOutput() bool {
	return false
}

func (m *mockProviderWithLatency) SupportsStreaming() bool {
	return false
}

func (m *mockProviderWithLatency) SupportsTools() bool {
	return false
}
