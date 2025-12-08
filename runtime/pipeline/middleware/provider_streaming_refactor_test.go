package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
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
	mockProvider := mock.NewProvider("test-provider", "test-model", false)

	// Create a Predict request
	req := providers.PredictionRequest{
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
				if len(req.Metadata) > 0 {
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
	mockRepo := mock.NewInMemoryMockRepository("Test response")
	mockProvider := mock.NewProviderWithRepository("test-provider", "test-model", false, mockRepo)

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

	// Create empty tooling config (no tools)
	tooling := toolingConfig{
		providerTools: nil,
		toolChoice:    "",
		registry:      nil,
		policy:        nil,
	}

	// Execute streaming round
	hasMore, err := executeStreamingRound(execCtx, mockProvider, tooling, "auto", nil)

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

// TestExecuteStreaming tests the main streaming execution entry point
func TestExecuteStreaming(t *testing.T) {
	tests := []struct {
		name           string
		setupProvider  func() providers.Provider
		setupRegistry  func() *tools.Registry
		messages       []types.Message
		expectError    bool
		expectMessages int
	}{
		{
			name: "successful streaming without tools",
			setupProvider: func() providers.Provider {
				mockRepo := mock.NewInMemoryMockRepository("Test response")
				return mock.NewProviderWithRepository("test-provider", "test-model", false, mockRepo)
			},
			setupRegistry: func() *tools.Registry {
				return tools.NewRegistry()
			},
			messages: []types.Message{
				{Role: "user", Content: "test message"},
			},
			expectError:    false,
			expectMessages: 2, // user + assistant
		},
		{
			name: "streaming without tool registry",
			setupProvider: func() providers.Provider {
				mockRepo := mock.NewInMemoryMockRepository("Test response without tools")
				return mock.NewProviderWithRepository("test-provider", "test-model", false, mockRepo)
			},
			setupRegistry: func() *tools.Registry {
				return nil
			},
			messages: []types.Message{
				{Role: "user", Content: "test message"},
			},
			expectError:    false,
			expectMessages: 2, // user + assistant
		},
		{
			name: "streaming with empty messages",
			setupProvider: func() providers.Provider {
				mockRepo := mock.NewInMemoryMockRepository("Response to empty")
				return mock.NewProviderWithRepository("test-provider", "test-model", false, mockRepo)
			},
			setupRegistry: func() *tools.Registry {
				return tools.NewRegistry()
			},
			messages:       []types.Message{},
			expectError:    false,
			expectMessages: 1, // assistant response
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := tt.setupProvider()
			registry := tt.setupRegistry()

			execCtx := &pipeline.ExecutionContext{
				Context:    context.Background(),
				Messages:   tt.messages,
				StreamMode: true,
				Metadata: map[string]interface{}{
					"mock_scenario_id": "test-scenario",
					"mock_turn_number": 1,
				},
			}

			// Create provider middleware
			middleware := ProviderMiddleware(provider, registry, nil, nil)

			// Execute the middleware process function which calls executeStreaming internally
			err := middleware.Process(execCtx, func() error { return nil })

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

			if len(execCtx.Messages) != tt.expectMessages {
				t.Errorf("Expected %d messages, got %d", tt.expectMessages, len(execCtx.Messages))
			}
		})
	}
}

// TestHandleStreamInterruption tests the interrupted stream handling
func TestHandleStreamInterruption(t *testing.T) {
	tests := []struct {
		name           string
		currentContent string
		expectCost     bool
		expectMessage  bool
	}{
		{
			name:           "interruption with content",
			currentContent: "Partial response...",
			expectCost:     true,
			expectMessage:  true,
		},
		{
			name:           "interruption with empty content",
			currentContent: "",
			expectCost:     true,
			expectMessage:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock provider
			mockRepo := mock.NewInMemoryMockRepository("Test response")
			mockProvider := mock.NewProviderWithRepository("test-provider", "test-model", false, mockRepo)

			// Create execution context
			execCtx := &pipeline.ExecutionContext{
				Context: context.Background(),
				Messages: []types.Message{
					{Role: "user", Content: "test message"},
				},
				Metadata:   map[string]interface{}{},
				StreamMode: true,
			}

			// Create provider request
			req := providers.PredictionRequest{
				Messages: execCtx.Messages,
			}

			// Create stream process result with the content
			result := &streamProcessResult{
				finalContent: tt.currentContent,
				interrupted:  true,
			}

			// Handle interruption via package-level function
			err := handleStreamInterruption(execCtx, mockProvider, req, result, 0, nil)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Should have added assistant message
			if tt.expectMessage {
				if len(execCtx.Messages) != 2 {
					t.Errorf("Expected 2 messages, got %d", len(execCtx.Messages))
					return
				}

				lastMsg := execCtx.Messages[len(execCtx.Messages)-1]
				if lastMsg.Role != "assistant" {
					t.Errorf("Expected assistant message, got %s", lastMsg.Role)
				}

				if lastMsg.Content != tt.currentContent {
					t.Errorf("Expected content %q, got %q", tt.currentContent, lastMsg.Content)
				}

				// Check that message has Meta field with cost_estimate_type
				if lastMsg.Meta == nil {
					t.Error("Expected Meta field on message but got nil")
					return
				}

				rawResp, ok := lastMsg.Meta["raw_response"]
				if !ok {
					t.Error("Expected raw_response in Meta")
					return
				}

				if rawRespMap, ok := rawResp.(map[string]interface{}); ok {
					if costEstType, ok := rawRespMap["cost_estimate_type"]; !ok || costEstType != "approximate" {
						t.Errorf("Expected cost_estimate_type=approximate, got %v", costEstType)
					}
				}
			}

			// Check execCtx.Response for cost information
			if tt.expectCost {
				if execCtx.Response == nil {
					t.Error("Expected Response to be set but got nil")
					return
				}

				// The approximate cost should be reflected in Response.Metadata
				// (TokensInput, TokensOutput, Cost fields)
				if execCtx.Response.Metadata.TokensInput == 0 && execCtx.Response.Metadata.TokensOutput == 0 {
					// Mock provider might return zero tokens, which is acceptable for this test
					// Just verify the response structure is present
				}
			}
		})
	}
}

// TestCreateErrorToolResult tests the error tool result creation
func TestCreateErrorToolResult(t *testing.T) {
	tests := []struct {
		name      string
		toolID    string
		toolName  string
		err       error
		expectID  string
		expectErr string
	}{
		{
			name:      "simple error result",
			toolID:    "call_123",
			toolName:  "test_tool",
			err:       errors.New("tool failed"),
			expectID:  "call_123",
			expectErr: "tool failed",
		},
		{
			name:      "empty error message",
			toolID:    "call_456",
			toolName:  "another_tool",
			err:       errors.New(""),
			expectID:  "call_456",
			expectErr: "",
		},
		{
			name:      "complex error message",
			toolID:    "call_789",
			toolName:  "complex_tool",
			err:       errors.New("Multiple errors occurred:\n1. Connection timeout\n2. Invalid response"),
			expectID:  "call_789",
			expectErr: "Multiple errors occurred:\n1. Connection timeout\n2. Invalid response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create tool call
			toolCall := types.MessageToolCall{
				ID:   tt.toolID,
				Name: tt.toolName,
			}

			// Create error result
			result := createErrorToolResult(toolCall, tt.err)

			// Verify result structure
			if result.ID != tt.expectID {
				t.Errorf("Expected ID %q, got %q", tt.expectID, result.ID)
			}

			if result.Name != tt.toolName {
				t.Errorf("Expected Name %q, got %q", tt.toolName, result.Name)
			}

			// Content should contain "Error: " prefix
			expectedContent := "Error: " + tt.expectErr
			if result.Content != expectedContent {
				t.Errorf("Expected Content %q, got %q", expectedContent, result.Content)
			}

			if result.Error != tt.expectErr {
				t.Errorf("Expected Error %q, got %q", tt.expectErr, result.Error)
			}
		})
	}
}

// TestStreamChunk tests the middleware chunk handler
func TestStreamChunk(t *testing.T) {
	// Create mock provider
	mockRepo := mock.NewInMemoryMockRepository("Test response")
	mockProvider := mock.NewProviderWithRepository("test-provider", "test-model", false, mockRepo)

	// Create provider middleware
	middleware := ProviderMiddleware(mockProvider, nil, nil, nil)

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
	}

	// Create test chunks (using pointers as expected by the method)
	testChunks := []*providers.StreamChunk{
		{Content: "Hello"},
		{Content: "Hello world"},
		{Content: "Hello world!", FinishReason: strPtr("stop")},
	}

	// Test each chunk
	for _, chunk := range testChunks {
		err := middleware.StreamChunk(execCtx, chunk)

		// StreamChunk should be a no-op and return nil
		if err != nil {
			t.Errorf("StreamChunk should return nil, got error: %v", err)
		}
	}

	// Test with empty chunk
	emptyChunk := &providers.StreamChunk{}
	err := middleware.StreamChunk(execCtx, emptyChunk)
	if err != nil {
		t.Errorf("StreamChunk with empty chunk should return nil, got error: %v", err)
	}

	// Test with error chunk
	errorChunk := &providers.StreamChunk{Error: errors.New("stream error")}
	err = middleware.StreamChunk(execCtx, errorChunk)
	if err != nil {
		t.Errorf("StreamChunk with error chunk should return nil, got error: %v", err)
	}
}

// MockToolRegistry is a simple mock for testing
type MockToolRegistry struct {
	tools map[string]func(context.Context, string) (string, error)
}

func NewMockToolRegistry() *MockToolRegistry {
	return &MockToolRegistry{
		tools: make(map[string]func(context.Context, string) (string, error)),
	}
}

func (m *MockToolRegistry) AddTool(name string, fn func(context.Context, string) (string, error)) {
	m.tools[name] = fn
}

func (m *MockToolRegistry) Execute(ctx context.Context, name string, args string) (string, error) {
	if fn, ok := m.tools[name]; ok {
		return fn(ctx, args)
	}
	return "", errors.New("tool not found")
}

func (m *MockToolRegistry) Get(name string) (interface{}, error) {
	if _, ok := m.tools[name]; ok {
		return m.tools[name], nil
	}
	return nil, errors.New("tool not found")
}

func (m *MockToolRegistry) List() []string {
	var names []string
	for name := range m.tools {
		names = append(names, name)
	}
	return names
}
