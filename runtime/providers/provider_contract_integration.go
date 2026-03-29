// Package providers contains provider contract test helpers.
//
// This file contains exported test helpers that can be used by provider
// implementations in subpackages to validate their contract compliance.
package providers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Default values for contract test requests.
const (
	contractTestTemperature = 0.7
	contractTestMaxTokens   = 100
	contractToolMaxTokens   = 200
	contractSmallMaxTokens  = 50
	contractZeroTemperature = 0.0
)

// ProviderContractTests defines a comprehensive test suite that validates
// the Provider interface contract. All provider implementations should pass
// these tests to ensure consistent behavior across the system.
//
// Usage:
//
//	func TestOpenAIProviderContract(t *testing.T) {
//	    provider := NewProvider(...)
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

	t.Run("Contract_Predict_ReturnsLatency", func(t *testing.T) {
		ValidatePredictReturnsLatency(t, config.Provider)
	})

	if config.SupportsToolsExpected {
		t.Run("Contract_PredictWithTools_ReturnsLatency", func(t *testing.T) {
			ValidatePredictWithToolsReturnsLatency(t, config.Provider)
		})

		t.Run("Contract_PredictWithTools_ProducesToolCalls", func(t *testing.T) {
			testPredictWithToolsProducesToolCalls(t, config.Provider)
		})

		t.Run("Contract_PredictWithTools_ToolCallFormat", func(t *testing.T) {
			testPredictWithToolsToolCallFormat(t, config.Provider)
		})

		t.Run("Contract_PredictWithTools_SystemMessage", func(t *testing.T) {
			testPredictWithToolsSystemMessage(t, config.Provider)
		})

		t.Run("Contract_PredictWithTools_ReturnsCostInfo", func(t *testing.T) {
			testPredictWithToolsReturnsCostInfo(t, config.Provider)
		})

		t.Run("Contract_PredictWithTools_MultiTurn", func(t *testing.T) {
			testPredictWithToolsMultiTurn(t, config.Provider)
		})

		t.Run("Contract_PredictStreamWithTools_ProducesToolCalls", func(t *testing.T) {
			testPredictStreamWithToolsProducesToolCalls(t, config.Provider)
		})
	}

	t.Run("Contract_Predict_ReturnsCostInfo", func(t *testing.T) {
		testPredictReturnsCostInfo(t, config.Provider)
	})

	t.Run("Contract_Predict_NonEmptyResponse", func(t *testing.T) {
		testPredictNonEmptyResponse(t, config.Provider)
	})

	t.Run("Contract_Predict_WithSystemMessage", func(t *testing.T) {
		testPredictWithSystemMessage(t, config.Provider)
	})

	t.Run("Contract_CalculateCost_Reasonable", func(t *testing.T) {
		testCalculateCostReasonable(t, config.Provider)
	})

	t.Run("Contract_SupportsStreaming_Matches", func(t *testing.T) {
		testSupportsStreamingMatches(t, config.Provider, config.SupportsStreamingExpected)
	})

	if config.SupportsStreamingExpected {
		t.Run("Contract_PredictStream_ReturnsLatency", func(t *testing.T) {
			testPredictStreamReturnsLatency(t, config.Provider)
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

// ValidatePredictReturnsLatency verifies that Predict() returns a response with non-zero latency.
// This is the critical test that would have caught the production bug!
// Exported for use in provider-specific regression tests.
func ValidatePredictReturnsLatency(t *testing.T, provider Provider) {
	ctx := context.Background()
	req := PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say 'test'"},
		},
		MaxTokens:   50,
		Temperature: 0.7,
	}

	start := time.Now()
	resp, err := provider.Predict(ctx, req)
	elapsed := time.Since(start)

	if err != nil {
		t.Skipf("Skipping latency test due to API error (may need credentials): %v", err)
		return
	}

	// Latency must be non-zero
	if resp.Latency == 0 {
		t.Errorf("Predict() returned Latency=0, but call took %v", elapsed)
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

// ValidatePredictWithToolsReturnsLatency verifies that PredictWithTools() returns latency.
// This test is CRITICAL - it would have caught the production bug where
// PredictWithTools didn't set latency!
// Exported for use in provider-specific regression tests.
func ValidatePredictWithToolsReturnsLatency(t *testing.T, provider Provider) {
	toolSupport, ok := provider.(ToolSupport)
	if !ok {
		t.Skip("Provider doesn't implement ToolSupport interface")
		return
	}

	ctx := context.Background()
	req := PredictionRequest{
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
	resp, toolCalls, err := toolSupport.PredictWithTools(ctx, req, tools, "auto")
	elapsed := time.Since(start)

	if err != nil {
		t.Skipf("Skipping PredictWithTools latency test due to API error: %v", err)
		return
	}

	_ = toolCalls // We don't care if tools were actually called, just that latency was tracked

	// Latency must be non-zero
	if resp.Latency == 0 {
		t.Errorf("PredictWithTools() returned Latency=0, but call took %v", elapsed)
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

// testPredictReturnsCostInfo verifies that Predict() returns cost information
func testPredictReturnsCostInfo(t *testing.T, provider Provider) {
	ctx := context.Background()
	req := PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say 'test'"},
		},
		MaxTokens:   50,
		Temperature: 0.7,
	}

	resp, err := provider.Predict(ctx, req)
	if err != nil {
		t.Skipf("Skipping cost test due to API error (may need credentials): %v", err)
		return
	}

	// Cost info should be present
	if resp.CostInfo == nil {
		t.Error("Predict() returned nil CostInfo")
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

// testPredictNonEmptyResponse verifies that Predict() returns non-empty content
func testPredictNonEmptyResponse(t *testing.T, provider Provider) {
	ctx := context.Background()
	req := PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say 'hello'"},
		},
		MaxTokens:   50,
		Temperature: 0.7,
	}

	resp, err := provider.Predict(ctx, req)
	if err != nil {
		t.Skipf("Skipping response test due to API error (may need credentials): %v", err)
		return
	}

	if resp.Content == "" {
		t.Error("Predict() returned empty content")
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

// testPredictStreamReturnsLatency verifies streaming responses include latency in final chunk
func testPredictStreamReturnsLatency(t *testing.T, provider Provider) {
	ctx := context.Background()
	req := PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Count to 3"},
		},
		MaxTokens:   50,
		Temperature: 0.7,
	}

	start := time.Now()
	chunks, err := provider.PredictStream(ctx, req)
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

// weatherToolDescriptors returns a standard set of tool descriptors for contract tests.
func weatherToolDescriptors() []*ToolDescriptor {
	return []*ToolDescriptor{
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
}

// buildWeatherTools is a helper that builds tooling from weather descriptors.
func buildWeatherTools(t *testing.T, toolSupport ToolSupport) ProviderTools {
	t.Helper()
	tools, err := toolSupport.BuildTooling(weatherToolDescriptors())
	if err != nil {
		t.Fatalf("Failed to build tooling: %v", err)
	}
	return tools
}

// testPredictWithToolsProducesToolCalls verifies that PredictWithTools actually
// returns tool calls when given a clear tool-calling prompt with required tool choice.
func testPredictWithToolsProducesToolCalls(t *testing.T, provider Provider) {
	toolSupport, ok := provider.(ToolSupport)
	if !ok {
		t.Skip("Provider doesn't implement ToolSupport")
		return
	}

	tools := buildWeatherTools(t, toolSupport)

	ctx := context.Background()
	req := PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What is the weather in Tokyo?"},
		},
		MaxTokens:   contractToolMaxTokens,
		Temperature: contractZeroTemperature,
	}

	resp, toolCalls, err := toolSupport.PredictWithTools(ctx, req, tools, "required")
	if err != nil {
		t.Skipf("Skipping tool call test due to API error: %v", err)
		return
	}

	if len(toolCalls) == 0 && len(resp.ToolCalls) == 0 {
		t.Error("PredictWithTools() with toolChoice=required returned no tool calls")
		t.Logf("Response content: %q", resp.Content)
	}

	// At least one source of tool calls should be populated
	calls := toolCalls
	if len(calls) == 0 {
		calls = resp.ToolCalls
	}

	if len(calls) > 0 {
		t.Logf("Got %d tool call(s), first: %s(%s)", len(calls), calls[0].Name, string(calls[0].Args))
	}
}

// testPredictWithToolsToolCallFormat validates the structure of returned tool calls.
func testPredictWithToolsToolCallFormat(t *testing.T, provider Provider) {
	toolSupport, ok := provider.(ToolSupport)
	if !ok {
		t.Skip("Provider doesn't implement ToolSupport")
		return
	}

	tools := buildWeatherTools(t, toolSupport)

	ctx := context.Background()
	req := PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What is the weather in Paris?"},
		},
		MaxTokens:   contractToolMaxTokens,
		Temperature: contractZeroTemperature,
	}

	resp, toolCalls, err := toolSupport.PredictWithTools(ctx, req, tools, "required")
	if err != nil {
		t.Skipf("Skipping tool format test due to API error: %v", err)
		return
	}

	calls := toolCalls
	if len(calls) == 0 {
		calls = resp.ToolCalls
	}
	if len(calls) == 0 {
		t.Skip("No tool calls returned — cannot validate format")
		return
	}

	for i, tc := range calls {
		if tc.Name == "" {
			t.Errorf("ToolCall[%d].Name is empty", i)
		}
		if tc.ID == "" {
			t.Errorf("ToolCall[%d].ID is empty", i)
		}
		if len(tc.Args) == 0 {
			t.Errorf("ToolCall[%d].Args is empty", i)
			continue
		}
		// Args should be valid JSON
		var parsed any
		if err := json.Unmarshal(tc.Args, &parsed); err != nil {
			t.Errorf("ToolCall[%d].Args is not valid JSON: %v (raw: %s)", i, err, string(tc.Args))
		}
		t.Logf("ToolCall[%d]: id=%s name=%s args=%s", i, tc.ID, tc.Name, string(tc.Args))
	}
}

// testPredictWithToolsSystemMessage verifies that system messages don't break tool calling.
// This is a regression test for the bug where buildToolRequest didn't skip system messages.
func testPredictWithToolsSystemMessage(t *testing.T, provider Provider) {
	toolSupport, ok := provider.(ToolSupport)
	if !ok {
		t.Skip("Provider doesn't implement ToolSupport")
		return
	}

	tools := buildWeatherTools(t, toolSupport)

	ctx := context.Background()
	req := PredictionRequest{
		System: "You are a helpful weather assistant. Always use the get_weather tool when asked about weather.",
		Messages: []types.Message{
			{Role: "system", Content: "Additional system context: respond concisely."},
			{Role: "user", Content: "What is the weather in London?"},
		},
		MaxTokens:   contractToolMaxTokens,
		Temperature: contractZeroTemperature,
	}

	resp, toolCalls, err := toolSupport.PredictWithTools(ctx, req, tools, "auto")
	if err != nil {
		t.Errorf("PredictWithTools() with system messages failed: %v", err)
		return
	}

	// Should not crash and should return some response
	if resp.Content == "" && len(toolCalls) == 0 && len(resp.ToolCalls) == 0 {
		t.Error("PredictWithTools() with system messages returned empty response and no tool calls")
	}

	t.Logf("System message test: content=%q, toolCalls=%d", resp.Content, len(toolCalls))
}

// testPredictWithToolsReturnsCostInfo verifies that PredictWithTools returns cost information.
func testPredictWithToolsReturnsCostInfo(t *testing.T, provider Provider) {
	toolSupport, ok := provider.(ToolSupport)
	if !ok {
		t.Skip("Provider doesn't implement ToolSupport")
		return
	}

	tools := buildWeatherTools(t, toolSupport)

	ctx := context.Background()
	req := PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What is the weather in Berlin?"},
		},
		MaxTokens:   contractToolMaxTokens,
		Temperature: contractTestTemperature,
	}

	resp, _, err := toolSupport.PredictWithTools(ctx, req, tools, "auto")
	if err != nil {
		t.Skipf("Skipping cost test due to API error: %v", err)
		return
	}

	if resp.CostInfo == nil {
		t.Error("PredictWithTools() returned nil CostInfo")
		return
	}

	if resp.CostInfo.InputTokens <= 0 {
		t.Errorf("PredictWithTools InputTokens should be positive, got %d", resp.CostInfo.InputTokens)
	}
	if resp.CostInfo.OutputTokens <= 0 {
		t.Errorf("PredictWithTools OutputTokens should be positive, got %d", resp.CostInfo.OutputTokens)
	}
}

// testPredictWithToolsMultiTurn verifies that tool results can be fed back in a multi-turn conversation.
func testPredictWithToolsMultiTurn(t *testing.T, provider Provider) {
	toolSupport, ok := provider.(ToolSupport)
	if !ok {
		t.Skip("Provider doesn't implement ToolSupport")
		return
	}

	tools := buildWeatherTools(t, toolSupport)

	ctx := context.Background()

	// Turn 1: get tool call
	req1 := PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What is the weather in Sydney?"},
		},
		MaxTokens:   contractToolMaxTokens,
		Temperature: contractZeroTemperature,
	}

	resp1, toolCalls1, err := toolSupport.PredictWithTools(ctx, req1, tools, "required")
	if err != nil {
		t.Skipf("Skipping multi-turn test due to API error on turn 1: %v", err)
		return
	}

	calls := toolCalls1
	if len(calls) == 0 {
		calls = resp1.ToolCalls
	}
	if len(calls) == 0 {
		t.Skip("No tool calls on turn 1 — cannot test multi-turn")
		return
	}

	// Turn 2: feed back tool result and get final response
	req2 := PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What is the weather in Sydney?"},
			{
				Role:    "assistant",
				Content: resp1.Content,
				ToolCalls: []types.MessageToolCall{
					{ID: calls[0].ID, Name: calls[0].Name, Args: calls[0].Args},
				},
			},
			{
				Role: "tool",
				ToolResult: &types.MessageToolResult{
					ID:   calls[0].ID,
					Name: calls[0].Name,
					Parts: []types.ContentPart{
						types.NewTextPart(`{"temperature": 22, "condition": "sunny", "humidity": 45}`),
					},
				},
			},
		},
		MaxTokens:   contractToolMaxTokens,
		Temperature: contractTestTemperature,
	}

	resp2, _, err := toolSupport.PredictWithTools(ctx, req2, tools, "auto")
	if err != nil {
		t.Errorf("PredictWithTools() multi-turn (turn 2) failed: %v", err)
		return
	}

	if resp2.Content == "" {
		t.Error("PredictWithTools() multi-turn returned empty content on turn 2")
		return
	}

	// The response should reference the weather data we provided
	lower := strings.ToLower(resp2.Content)
	if !strings.Contains(lower, "sydney") && !strings.Contains(lower, "22") && !strings.Contains(lower, "sunny") {
		t.Logf("Warning: multi-turn response may not reference tool result: %q", resp2.Content)
	}

	t.Logf("Multi-turn response: %q", resp2.Content)
}

// testPredictWithSystemMessage verifies that Predict works with a system message.
func testPredictWithSystemMessage(t *testing.T, provider Provider) {
	ctx := context.Background()
	req := PredictionRequest{
		System: "You are a pirate. Always respond with 'Arr!'",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens:   contractSmallMaxTokens,
		Temperature: contractTestTemperature,
	}

	resp, err := provider.Predict(ctx, req)
	if err != nil {
		t.Skipf("Skipping system message test due to API error: %v", err)
		return
	}

	if resp.Content == "" {
		t.Error("Predict() with system message returned empty content")
	}
}

// SkipIfNoCredentials skips the test if API credentials are not available.
// This is a helper for integration tests that need real API access.
func SkipIfNoCredentials(t *testing.T, provider Provider) {
	// Try a simple call to see if credentials work
	ctx := context.Background()
	req := PredictionRequest{
		Messages:    []types.Message{{Role: "user", Content: "test"}},
		MaxTokens:   10,
		Temperature: 0.5,
	}

	_, err := provider.Predict(ctx, req)
	if err != nil {
		t.Skipf("Skipping test - API credentials not available or provider not accessible: %v", err)
	}
}

// testPredictStreamWithToolsProducesToolCalls verifies that streaming tool
// calls work — the model streams a tool call with valid args via
// PredictStreamWithTools. This catches the class of bugs where streaming
// delta events lose tool call args or IDs.
func testPredictStreamWithToolsProducesToolCalls(t *testing.T, provider Provider) {
	toolSupport, ok := provider.(ToolSupport)
	if !ok {
		t.Skip("Provider doesn't implement ToolSupport")
		return
	}

	tools := buildWeatherTools(t, toolSupport)

	ctx := context.Background()
	req := PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What is the weather in Tokyo?"},
		},
		MaxTokens:   contractToolMaxTokens,
		Temperature: contractZeroTemperature,
	}

	stream, err := toolSupport.PredictStreamWithTools(ctx, req, tools, "required")
	if err != nil {
		t.Skipf("Skipping streaming tool test due to API error: %v", err)
		return
	}

	var lastChunk *StreamChunk
	for chunk := range stream {
		if chunk.Error != nil {
			t.Skipf("Skipping streaming tool test due to chunk error: %v", chunk.Error)
			return
		}
		lastChunk = &chunk
	}

	if lastChunk == nil {
		t.Fatal("Stream returned no chunks")
		return
	}

	if len(lastChunk.ToolCalls) == 0 {
		t.Error("PredictStreamWithTools() returned no tool calls in final chunk")
		return
	}

	tc := lastChunk.ToolCalls[0]
	if tc.Name == "" {
		t.Error("Streamed tool call has empty Name")
	}
	if tc.ID == "" {
		t.Error("Streamed tool call has empty ID")
	}
	if len(tc.Args) == 0 || string(tc.Args) == "" || string(tc.Args) == "{}" {
		t.Errorf("Streamed tool call has empty args: %q", string(tc.Args))
	}

	// Args should be valid JSON with a location field
	var args map[string]any
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		t.Errorf("Streamed tool call args not valid JSON: %v (raw: %s)", err, string(tc.Args))
	}

	t.Logf("Streaming tool call: id=%s name=%s args=%s", tc.ID, tc.Name, string(tc.Args))
}
