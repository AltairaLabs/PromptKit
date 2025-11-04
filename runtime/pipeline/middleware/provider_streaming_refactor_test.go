package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestProcessStreamChunks tests the processStreamChunks helper function
func TestProcessStreamChunks(t *testing.T) {
	tests := []struct {
		name           string
		chunks         []providers.StreamChunk
		expectError    bool
		expectContent  string
		expectToolCall bool
		expectFinal    bool
	}{
		{
			name: "successful stream with content",
			chunks: []providers.StreamChunk{
				{Content: "Hello"},
				{Content: "Hello world"},
				{Content: "Hello world!", FinishReason: strPtr("stop")},
			},
			expectError:   false,
			expectContent: "Hello world!",
			expectFinal:   true,
		},
		{
			name: "stream with error",
			chunks: []providers.StreamChunk{
				{Content: "Hello"},
				{Error: errors.New("stream error")},
			},
			expectError: true,
		},
		{
			name: "stream with tool calls",
			chunks: []providers.StreamChunk{
				{Content: "Processing"},
				{
					Content: "Processing...",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_1", Name: "test_tool"},
					},
					FinishReason: strPtr("tool_calls"),
				},
			},
			expectError:    false,
			expectContent:  "Processing...",
			expectToolCall: true,
			expectFinal:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create stream channel
			stream := make(chan providers.StreamChunk, len(tt.chunks))
			for _, chunk := range tt.chunks {
				stream <- chunk
			}
			close(stream)

			// Create execution context
			execCtx := &pipeline.ExecutionContext{
				Context: context.Background(),
			}

			// Process stream
			result, err := processStreamChunks(execCtx, stream)

			// Check error expectation
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Check result
			if result == nil {
				t.Fatal("Expected result but got nil")
			}

			if result.finalContent != tt.expectContent {
				t.Errorf("Expected content %q, got %q", tt.expectContent, result.finalContent)
			}

			if tt.expectToolCall && len(result.toolCalls) == 0 {
				t.Error("Expected tool calls but got none")
			}

			if tt.expectFinal && result.finalChunk == nil {
				t.Error("Expected final chunk but got nil")
			}
		})
	}
}

// TestCalculateApproximateCost tests the approximate cost calculation helper
func TestCalculateApproximateCost(t *testing.T) {
	// Create a mock provider with nil repository (uses default responses)
	mockProvider := providers.NewMockProvider("test-provider", "test-model", false)

	// Create a chat request
	req := providers.ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Hello world"},
		},
	}

	// Calculate approximate cost
	cost := calculateApproximateCost(mockProvider, req, "This is a response")

	// Mock provider should return cost info
	if cost == nil {
		t.Error("Expected cost info but got nil")
		return
	}

	// Check that some fields are populated
	if cost.InputTokens == 0 {
		t.Error("Expected input tokens > 0")
	}
}

// TestBuildProviderRequest_WithMetadata tests that metadata is copied correctly
func TestBuildProviderRequest_WithMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
	}{
		{
			name: "with string metadata",
			metadata: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "with mixed metadata",
			metadata: map[string]interface{}{
				"string_key": "value",
				"int_key":    42,
				"bool_key":   true,
			},
		},
		{
			name:     "with nil metadata",
			metadata: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create execution context with metadata
			execCtx := &pipeline.ExecutionContext{
				Context:  context.Background(),
				Messages: []types.Message{{Role: "user", Content: "test"}},
				Metadata: tt.metadata,
			}

			// Build request
			req := buildProviderRequest(execCtx, nil)

			// Check metadata was copied
			if tt.metadata == nil {
				if req.Metadata != nil && len(req.Metadata) > 0 {
					t.Errorf("Expected nil or empty metadata, got %v", req.Metadata)
				}
			} else {
				if req.Metadata == nil {
					t.Error("Expected metadata to be copied but got nil")
					return
				}

				for key, expectedValue := range tt.metadata {
					actualValue, ok := req.Metadata[key]
					if !ok {
						t.Errorf("Expected metadata key %q not found", key)
						continue
					}
					if actualValue != expectedValue {
						t.Errorf("Metadata[%q] = %v, want %v", key, actualValue, expectedValue)
					}
				}
			}
		})
	}
}

// TestStreamInterruption tests stream interruption handling
func TestStreamInterruption(t *testing.T) {
	t.Skip("Stream interruption testing requires middleware hooks - skipping for now")
}

// Helper function to create string pointer
func strPtr(s string) *string {
	return &s
}

// TestExecuteStreamingRound tests a single streaming round
func TestExecuteStreamingRound(t *testing.T) {
	// Create mock provider with in-memory repository
	mockRepo := providers.NewInMemoryMockRepository("Test response")
	mockProvider := providers.NewMockProviderWithRepository("test-provider", "test-model", false, mockRepo)

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "test message"},
		},
		Metadata: map[string]interface{}{
			"mock_scenario_id": "test-scenario",
			"mock_turn_number": 1,
		},
	}

	// Execute streaming round
	hasMore, err := executeStreamingRound(execCtx, mockProvider, nil, nil, nil)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
		return
	}

	// Should not have more rounds (no tool calls)
	if hasMore {
		t.Error("Expected no more rounds, but got hasMore=true")
	}

	// Should have added assistant message
	if len(execCtx.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(execCtx.Messages))
	}

	if execCtx.Messages[1].Role != "assistant" {
		t.Errorf("Expected assistant message, got %s", execCtx.Messages[1].Role)
	}
}

// TestStreamProcessResult tests the streamProcessResult structure
func TestStreamProcessResult(t *testing.T) {
	result := &streamProcessResult{
		finalContent: "test content",
		toolCalls: []types.MessageToolCall{
			{ID: "call_1", Name: "test_tool"},
		},
		interrupted: false,
		finalChunk: &providers.StreamChunk{
			Content:      "test content",
			FinishReason: strPtr("stop"),
		},
	}

	if result.finalContent != "test content" {
		t.Errorf("Expected content %q, got %q", "test content", result.finalContent)
	}

	if len(result.toolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(result.toolCalls))
	}

	if result.interrupted {
		t.Error("Expected interrupted to be false")
	}

	if result.finalChunk == nil {
		t.Error("Expected final chunk to be set")
	}
}
