package middleware

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

// TestStateStoreLoadMiddleware_SetsSourceOnLoadedMessages verifies that messages
// loaded from StateStore have Source="statestore" field set.
func TestStateStoreLoadMiddleware_SetsSourceOnLoadedMessages(t *testing.T) {
	// Setup: Create store with existing conversation
	store := statestore.NewMemoryStore()
	existingState := &statestore.ConversationState{
		ID:     "test-conv",
		UserID: "user-123",
		Messages: []types.Message{
			{Role: "user", Content: "previous user message"},
			{Role: "assistant", Content: "previous assistant response"},
		},
	}
	err := store.Save(context.Background(), existingState)
	if err != nil {
		t.Fatalf("failed to setup test state: %v", err)
	}

	// Create execution context with new message (no Source set yet)
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "new user message"},
		},
	}

	// Create and execute middleware
	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-conv",
		UserID:         "user-123",
	}
	middleware := StateStoreLoadMiddleware(config)
	err = middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify: All loaded messages should have Source="statestore"
	// First 2 messages are from StateStore
	if len(execCtx.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(execCtx.Messages))
	}

	for i := 0; i < 2; i++ {
		assert.Equal(t, "statestore", execCtx.Messages[i].Source,
			"message[%d] (loaded from StateStore) should have Source='statestore'", i)
	}

	// Last message should have empty Source (not set by load middleware)
	assert.Empty(t, execCtx.Messages[2].Source,
		"message[2] (new message) should have empty Source")
}

// TestProviderMiddleware_SetsSourceOnCreatedMessages verifies that messages
// created by provider middleware have Source="pipeline" field set.
func TestProviderMiddleware_SetsSourceOnCreatedMessages(t *testing.T) {
	// Create a mock provider that returns a simple response
	mockProvider := &mockProviderForSourceTest{
		response: providers.PredictionResponse{
			Content: "Test response from LLM",
		},
	}

	// Create execution context with user message
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "Test question"},
		},
	}

	// Create and execute provider middleware
	middleware := ProviderMiddleware(mockProvider, nil, nil, nil)
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Provider middleware failed: %v", err)
	}

	// Verify: Assistant message created by provider should have Source="pipeline"
	if len(execCtx.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(execCtx.Messages))
	}

	assistantMsg := execCtx.Messages[1]
	assert.Equal(t, "assistant", assistantMsg.Role)
	assert.Equal(t, "pipeline", assistantMsg.Source,
		"assistant message created by provider should have Source='pipeline'")
}

// TestProviderMiddleware_SetsSourceOnToolMessages verifies that tool result
// messages created by provider middleware have Source="pipeline" field set.
func TestProviderMiddleware_SetsSourceOnToolMessages(t *testing.T) {
	// Create a mock provider that returns tool calls
	mockProvider := &mockProviderWithToolsForSourceTest{
		responses: []providers.PredictionResponse{
			// Round 1: LLM requests a tool call
			{
				Content: "I'll calculate that for you",
				ToolCalls: []types.MessageToolCall{
					{
						ID:   "call-1",
						Name: "calculate",
						Args: []byte(`{"operation": "add", "a": 2, "b": 3}`),
					},
				},
			},
			// Round 2: LLM responds with final answer
			{
				Content: "The result is 5",
			},
		},
	}

	// Create tool registry with mock tool
	registry := tools.NewRegistry()
	toolDesc := &tools.ToolDescriptor{
		Name:        "calculate",
		Description: "Performs basic arithmetic",
		InputSchema: []byte(`{"type": "object", "properties": {"operation": {"type": "string"}, "a": {"type": "number"}, "b": {"type": "number"}}}`),
		Mode:        "mock",
		MockResult:  []byte(`5`),
	}
	err := registry.Register(toolDesc)
	if err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	// Create execution context with user message
	execCtx := &pipeline.ExecutionContext{
		Context:      context.Background(),
		AllowedTools: []string{"calculate"},
		Messages: []types.Message{
			{Role: "user", Content: "What is 2 + 3?"},
		},
	}

	// Create and execute provider middleware
	middleware := ProviderMiddleware(mockProvider, registry, nil, nil)
	err = middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Provider middleware failed: %v", err)
	}

	// Verify message order and Source fields
	// Expected: [user, assistant with tool calls, tool result, assistant final]
	if len(execCtx.Messages) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(execCtx.Messages))
	}

	// Message 0: user message (no Source set)
	assert.Equal(t, "user", execCtx.Messages[0].Role)
	assert.Empty(t, execCtx.Messages[0].Source)

	// Message 1: assistant with tool calls (Source="pipeline")
	assert.Equal(t, "assistant", execCtx.Messages[1].Role)
	assert.NotEmpty(t, execCtx.Messages[1].ToolCalls)
	assert.Equal(t, "pipeline", execCtx.Messages[1].Source,
		"assistant message with tool calls should have Source='pipeline'")

	// Message 2: tool result (Source="pipeline")
	assert.Equal(t, "tool", execCtx.Messages[2].Role)
	assert.Equal(t, "pipeline", execCtx.Messages[2].Source,
		"tool result message should have Source='pipeline'")

	// Message 3: assistant final response (Source="pipeline")
	assert.Equal(t, "assistant", execCtx.Messages[3].Role)
	assert.Equal(t, "pipeline", execCtx.Messages[3].Source,
		"final assistant message should have Source='pipeline'")
}

// TestIntegration_SourceFieldAcrossMiddleware verifies Source field is maintained
// correctly when StateStore and Provider middleware work together.
func TestIntegration_SourceFieldAcrossMiddleware(t *testing.T) {
	// Setup: Create store with existing conversation
	store := statestore.NewMemoryStore()
	existingState := &statestore.ConversationState{
		ID:     "test-conv",
		UserID: "user-123",
		Messages: []types.Message{
			{Role: "user", Content: "first question"},
			{Role: "assistant", Content: "first answer"},
		},
	}
	err := store.Save(context.Background(), existingState)
	if err != nil {
		t.Fatalf("failed to setup test state: %v", err)
	}

	// Create mock provider
	mockProvider := &mockProviderForSourceTest{
		response: providers.PredictionResponse{
			Content: "second answer",
		},
	}

	// Create execution context with new message
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "second question"},
		},
	}

	// Execute StateStore load middleware
	stateConfig := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-conv",
		UserID:         "user-123",
	}
	loadMiddleware := StateStoreLoadMiddleware(stateConfig)

	// Execute provider middleware in next() chain
	providerMiddleware := ProviderMiddleware(mockProvider, nil, nil, nil)

	err = loadMiddleware.Process(execCtx, func() error {
		return providerMiddleware.Process(execCtx, func() error {
			return nil
		})
	})
	if err != nil {
		t.Fatalf("middleware chain failed: %v", err)
	}

	// Verify: Should have 4 messages total
	// [statestore user, statestore assistant, new user, pipeline assistant]
	if len(execCtx.Messages) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(execCtx.Messages))
	}

	// Messages 0-1: loaded from StateStore
	assert.Equal(t, "statestore", execCtx.Messages[0].Source,
		"loaded user message should have Source='statestore'")
	assert.Equal(t, "statestore", execCtx.Messages[1].Source,
		"loaded assistant message should have Source='statestore'")

	// Message 2: new user message (no Source)
	assert.Empty(t, execCtx.Messages[2].Source,
		"new user message should have empty Source")

	// Message 3: created by provider
	assert.Equal(t, "pipeline", execCtx.Messages[3].Source,
		"assistant message from provider should have Source='pipeline'")
}

// Mock provider for Source tests
type mockProviderForSourceTest struct {
	response providers.PredictionResponse
}

func (m *mockProviderForSourceTest) ID() string { return "mock-source-test" }
func (m *mockProviderForSourceTest) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	return m.response, nil
}
func (m *mockProviderForSourceTest) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}
func (m *mockProviderForSourceTest) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}
func (m *mockProviderForSourceTest) Close() error                 { return nil }
func (m *mockProviderForSourceTest) ShouldIncludeRawOutput() bool { return false }
func (m *mockProviderForSourceTest) SupportsStreaming() bool      { return false }
func (m *mockProviderForSourceTest) SupportsTools() bool          { return false }

// Mock provider with tool support for Source tests
type mockProviderWithToolsForSourceTest struct {
	responses []providers.PredictionResponse
	callIndex int
}

func (m *mockProviderWithToolsForSourceTest) ID() string { return "mock-tools-source-test" }
func (m *mockProviderWithToolsForSourceTest) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	resp := m.responses[m.callIndex]
	m.callIndex++
	return resp, nil
}
func (m *mockProviderWithToolsForSourceTest) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}
func (m *mockProviderWithToolsForSourceTest) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}
func (m *mockProviderWithToolsForSourceTest) Close() error                 { return nil }
func (m *mockProviderWithToolsForSourceTest) ShouldIncludeRawOutput() bool { return false }
func (m *mockProviderWithToolsForSourceTest) SupportsStreaming() bool      { return false }
func (m *mockProviderWithToolsForSourceTest) SupportsTools() bool          { return true }

func (m *mockProviderWithToolsForSourceTest) BuildTooling(tools []*providers.ToolDescriptor) (interface{}, error) {
	return tools, nil
}

func (m *mockProviderWithToolsForSourceTest) PredictWithTools(ctx context.Context, req providers.PredictionRequest, tools interface{}, toolChoice string) (providers.PredictionResponse, []types.MessageToolCall, error) {
	resp := m.responses[m.callIndex]
	m.callIndex++
	return resp, resp.ToolCalls, nil
}
