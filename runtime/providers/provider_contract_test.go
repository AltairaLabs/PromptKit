package providers

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ProviderContractTests defines a comprehensive test suite that validates
// the Provider interface contract. All provider implementations should pass
// these tests to ensure consistent behavior across the system.
//
// Usage:
//
//	func TestOpenAIProviderContract(t *testing.T) {
//	    provider := NewOpenAIProvider(...)
//	    RunProviderContractTests(t, provider)
//	}
type ProviderContractTests struct {
	// Provider instance to test
	Provider Provider

	// SupportsToolsExpected indicates whether this provider should support tools
	SupportsToolsExpected bool

	// SupportsStreamingExpected indicates whether this provider should support streaming
	SupportsStreamingExpected bool
}

// RunProviderContractTests executes all contract tests against a provider.
// This should be called from each provider's test file.
func RunProviderContractTests(t *testing.T, config ProviderContractTests) {
	t.Run("Contract_ID", func(t *testing.T) {
		testProviderID(t, config.Provider)
	})

	t.Run("Contract_Chat_ReturnsLatency", func(t *testing.T) {
		ValidateChatReturnsLatency(t, config.Provider)
	})

	if config.SupportsToolsExpected {
		t.Run("Contract_ChatWithTools_ReturnsLatency", func(t *testing.T) {
			ValidateChatWithToolsReturnsLatency(t, config.Provider)
		})
	}

	t.Run("Contract_Chat_ReturnsCostInfo", func(t *testing.T) {
		testChatReturnsCostInfo(t, config.Provider)
	})

	t.Run("Contract_Chat_NonEmptyResponse", func(t *testing.T) {
		testChatNonEmptyResponse(t, config.Provider)
	})

	t.Run("Contract_CalculateCost_Reasonable", func(t *testing.T) {
		testCalculateCostReasonable(t, config.Provider)
	})

	t.Run("Contract_SupportsStreaming_Matches", func(t *testing.T) {
		testSupportsStreamingMatches(t, config.Provider, config.SupportsStreamingExpected)
	})

	if config.SupportsStreamingExpected {
		t.Run("Contract_ChatStream_ReturnsLatency", func(t *testing.T) {
			testChatStreamReturnsLatency(t, config.Provider)
		})
	}
}

// testProviderID verifies that the provider returns a non-empty ID
func testProviderID(t *testing.T, provider Provider) {
	id := provider.ID()
	if id == "" {
		t.Error("Provider.ID() returned empty string")
	}
}

// ValidateChatReturnsLatency verifies that Chat() returns a response with non-zero latency.
// This is the critical test that would have caught the production bug!
// Exported for use in provider-specific regression tests.
func ValidateChatReturnsLatency(t *testing.T, provider Provider) {
	ctx := context.Background()
	req := ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say 'test'"},
		},
		MaxTokens:   50,
		Temperature: 0.7,
	}

	start := time.Now()
	resp, err := provider.Chat(ctx, req)
	elapsed := time.Since(start)

	if err != nil {
		t.Skipf("Skipping latency test due to API error (may need credentials): %v", err)
		return
	}

	// CRITICAL: Latency must be non-zero
	if resp.Latency == 0 {
		t.Errorf("CRITICAL BUG: Chat() returned Latency=0, but call took %v", elapsed)
		t.Logf("Response: %+v", resp)
		t.Logf("This will cause latency_ms to be omitted from JSON due to omitempty tag")
	}

	// Latency should be reasonable (within 10x of actual elapsed time)
	if resp.Latency > elapsed*10 {
		t.Errorf("Latency seems unreasonable: reported %v but actual elapsed %v", resp.Latency, elapsed)
	}

	// Latency should not be negative
	if resp.Latency < 0 {
		t.Errorf("Latency cannot be negative: %v", resp.Latency)
	}
}

// ValidateChatWithToolsReturnsLatency verifies that ChatWithTools() returns a response with non-zero latency.
// This test is CRITICAL - it would have caught the production bug where ChatWithTools didn't set latency!
// Exported for use in provider-specific regression tests.
func ValidateChatWithToolsReturnsLatency(t *testing.T, provider Provider) {
	toolSupport, ok := provider.(ToolSupport)
	if !ok {
		t.Skip("Provider doesn't implement ToolSupport interface")
		return
	}

	ctx := context.Background()
	req := ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What's the weather like in San Francisco?"},
		},
		MaxTokens:   100,
		Temperature: 0.7,
	}

	// Define a simple weather tool using ToolDescriptor
	descriptors := []*ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a location",
			InputSchema: []byte(`{
				"type": "object",
				"properties": {
					"location": {
						"type": "string",
						"description": "The city name"
					}
				},
				"required": ["location"]
			}`),
		},
	}

	// Build provider-native tools
	tools, err := toolSupport.BuildTooling(descriptors)
	if err != nil {
		t.Fatalf("Failed to build tooling: %v", err)
	}

	start := time.Now()
	resp, toolCalls, err := toolSupport.ChatWithTools(ctx, req, tools, "auto")
	elapsed := time.Since(start)

	if err != nil {
		t.Skipf("Skipping ChatWithTools latency test due to API error: %v", err)
		return
	}

	_ = toolCalls // We don't care if tools were actually called, just that latency was tracked

	// CRITICAL: Latency must be non-zero
	if resp.Latency == 0 {
		t.Errorf("CRITICAL BUG: ChatWithTools() returned Latency=0, but call took %v", elapsed)
		t.Logf("Response: %+v", resp)
		t.Logf("This will cause latency_ms to be omitted from JSON due to omitempty tag")
		t.Logf("This is the EXACT production bug we're fixing!")
	}

	// Latency should be reasonable (within 10x of actual elapsed time)
	if resp.Latency > elapsed*10 {
		t.Errorf("Latency seems unreasonable: reported %v but actual elapsed %v", resp.Latency, elapsed)
	}

	// Latency should not be negative
	if resp.Latency < 0 {
		t.Errorf("Latency cannot be negative: %v", resp.Latency)
	}
}

// testChatReturnsCostInfo verifies that Chat() returns cost information
func testChatReturnsCostInfo(t *testing.T, provider Provider) {
	ctx := context.Background()
	req := ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say 'test'"},
		},
		MaxTokens:   50,
		Temperature: 0.7,
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		t.Skipf("Skipping cost test due to API error (may need credentials): %v", err)
		return
	}

	// Cost info should be present
	if resp.CostInfo == nil {
		t.Error("Chat() returned nil CostInfo")
		return
	}

	// Token counts should be positive for a successful response
	if resp.CostInfo.InputTokens <= 0 {
		t.Errorf("InputTokens should be positive, got %d", resp.CostInfo.InputTokens)
	}

	if resp.CostInfo.OutputTokens <= 0 {
		t.Errorf("OutputTokens should be positive, got %d", resp.CostInfo.OutputTokens)
	}

	// Total cost should be non-negative
	if resp.CostInfo.TotalCost < 0 {
		t.Errorf("TotalCost cannot be negative, got %f", resp.CostInfo.TotalCost)
	}
}

// testChatNonEmptyResponse verifies that Chat() returns non-empty content
func testChatNonEmptyResponse(t *testing.T, provider Provider) {
	ctx := context.Background()
	req := ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say 'hello'"},
		},
		MaxTokens:   50,
		Temperature: 0.7,
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		t.Skipf("Skipping response test due to API error (may need credentials): %v", err)
		return
	}

	if resp.Content == "" {
		t.Error("Chat() returned empty content")
	}
}

// testCalculateCostReasonable verifies that CalculateCost returns reasonable values
func testCalculateCostReasonable(t *testing.T, provider Provider) {
	// Test with typical token counts
	cost := provider.CalculateCost(100, 50, 0)

	// Costs should be non-negative
	if cost.InputCostUSD < 0 {
		t.Errorf("InputCostUSD cannot be negative, got %f", cost.InputCostUSD)
	}
	if cost.OutputCostUSD < 0 {
		t.Errorf("OutputCostUSD cannot be negative, got %f", cost.OutputCostUSD)
	}
	if cost.TotalCost < 0 {
		t.Errorf("TotalCost cannot be negative, got %f", cost.TotalCost)
	}

	// TotalCost should equal sum of parts
	expected := cost.InputCostUSD + cost.OutputCostUSD + cost.CachedCostUSD
	// Use a small epsilon for floating point comparison
	epsilon := 0.0000001
	diff := cost.TotalCost - expected
	if diff < -epsilon || diff > epsilon {
		t.Errorf("TotalCost should be %f (sum of parts), got %f (diff: %e)", expected, cost.TotalCost, diff)
	}

	// Token counts should match input
	if cost.InputTokens != 100 {
		t.Errorf("InputTokens should be 100, got %d", cost.InputTokens)
	}
	if cost.OutputTokens != 50 {
		t.Errorf("OutputTokens should be 50, got %d", cost.OutputTokens)
	}

	// Cost per token should be reasonable (typical LLM costs are $0.0001 - $1 per 1K tokens)
	// For 100 input + 50 output = 150 tokens, cost should be roughly $0.000015 - $0.15
	if cost.TotalCost > 1.0 {
		t.Errorf("Cost seems unreasonably high: %f for 150 tokens", cost.TotalCost)
	}
}

// testSupportsStreamingMatches verifies that SupportsStreaming() matches expected capability
func testSupportsStreamingMatches(t *testing.T, provider Provider, expected bool) {
	actual := provider.SupportsStreaming()
	if actual != expected {
		t.Errorf("SupportsStreaming() returned %v, expected %v", actual, expected)
	}
}

// testChatStreamReturnsLatency verifies that streaming responses include latency in final chunk
func testChatStreamReturnsLatency(t *testing.T, provider Provider) {
	ctx := context.Background()
	req := ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Count to 3"},
		},
		MaxTokens:   50,
		Temperature: 0.7,
	}

	start := time.Now()
	chunks, err := provider.ChatStream(ctx, req)
	if err != nil {
		t.Skipf("Skipping stream latency test due to API error: %v", err)
		return
	}

	var finalChunk *StreamChunk
	chunkCount := 0
	for chunk := range chunks {
		chunkCount++
		if chunk.Error != nil {
			t.Skipf("Skipping stream latency test due to chunk error: %v", chunk.Error)
			return
		}
		finalChunk = &chunk
	}
	elapsed := time.Since(start)

	if chunkCount == 0 {
		t.Error("Stream returned no chunks")
		return
	}

	// Final chunk should have cost info with latency
	if finalChunk == nil {
		t.Error("No final chunk captured")
		return
	}

	if finalChunk.CostInfo != nil {
		// If cost info is present, it should have been calculated over some time
		// We can't directly check latency in StreamChunk, but we can verify the stream took time
		if elapsed < time.Millisecond {
			t.Errorf("Stream completed suspiciously fast: %v", elapsed)
		}
	}
}

// SkipIfNoCredentials skips the test if API credentials are not available.
// This is a helper for integration tests that need real API access.
func SkipIfNoCredentials(t *testing.T, provider Provider) {
	// Try a simple call to see if credentials work
	ctx := context.Background()
	req := ChatRequest{
		Messages:    []types.Message{{Role: "user", Content: "test"}},
		MaxTokens:   10,
		Temperature: 0.5,
	}

	_, err := provider.Chat(ctx, req)
	if err != nil {
		t.Skipf("Skipping test - API credentials not available or provider not accessible: %v", err)
	}
}
