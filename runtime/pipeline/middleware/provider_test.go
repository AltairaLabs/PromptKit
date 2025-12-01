package middleware

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockProvider implements providers.Provider for testing
type MockProvider struct {
	mock.Mock
}

func (m *MockProvider) ID() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(providers.PredictionResponse), args.Error(1)
}

func (m *MockProvider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(<-chan providers.StreamChunk), args.Error(1)
}

func (m *MockProvider) SupportsStreaming() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockProvider) ShouldIncludeRawOutput() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockProvider) Close() error {
	return nil
}

func (m *MockProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	args := m.Called(inputTokens, outputTokens, cachedTokens)
	return args.Get(0).(types.CostInfo)
}

func TestProviderMiddleware_SimpleResponse(t *testing.T) {
	mockProvider := new(MockProvider)

	response := providers.PredictionResponse{
		Content: "Hello, world!",
		CostInfo: &types.CostInfo{
			InputTokens:   10,
			OutputTokens:  5,
			InputCostUSD:  0.0001,
			OutputCostUSD: 0.0001,
			TotalCost:     0.0002,
		},
		Latency: 100 * time.Millisecond,
	}

	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	providerConfig := &ProviderMiddlewareConfig{
		Temperature: 0.7,
		MaxTokens:   100,
	}
	middleware := ProviderMiddleware(mockProvider, nil, nil, providerConfig)

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "You are a helpful assistant",
		Messages: []types.Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello"},
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)
	assert.NotNil(t, execCtx.Response)
	assert.Equal(t, "Hello, world!", execCtx.Response.Content)
	assert.Equal(t, 10, execCtx.CostInfo.InputTokens)
	assert.Equal(t, 5, execCtx.CostInfo.OutputTokens)
	assert.Equal(t, 0.0002, execCtx.CostInfo.TotalCost)

	mockProvider.AssertExpectations(t)
}

func TestProviderMiddleware_NoProvider(t *testing.T) {
	middleware := ProviderMiddleware(nil, nil, nil, nil)

	execCtx := &pipeline.ExecutionContext{}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no provider configured")
}

func TestProviderMiddleware_ProviderError(t *testing.T) {
	mockProvider := new(MockProvider)
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(
		providers.PredictionResponse{},
		assert.AnError,
	)

	middleware := ProviderMiddleware(mockProvider, nil, nil, nil)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "generation failed")
}

func TestProviderMiddleware_WithToolCalls(t *testing.T) {
	mockProvider := new(MockProvider)

	// First response with tool call
	firstResponse := providers.PredictionResponse{
		Content: "I'll check the weather for you",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call1",
				Name: "get_weather",
				Args: json.RawMessage(`{"location":"NYC"}`),
			},
		},
		CostInfo: &types.CostInfo{
			InputTokens:  10,
			OutputTokens: 5,
			TotalCost:    0.0001,
		},
		Latency: 100 * time.Millisecond,
	}

	// Second response after tool execution
	secondResponse := providers.PredictionResponse{
		Content: "The weather in NYC is sunny and 72 degrees",
		CostInfo: &types.CostInfo{
			InputTokens:  15,
			OutputTokens: 8,
			TotalCost:    0.0002,
		},
		Latency: 150 * time.Millisecond,
	}

	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(firstResponse, nil).Once()
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(secondResponse, nil).Once()

	// Create mock tool registry
	toolRegistry := tools.NewRegistry()
	mockWeatherTool := &tools.ToolDescriptor{
		Name:         "get_weather",
		Description:  "Get weather",
		Mode:         "mock",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"temp":{"type":"number"},"condition":{"type":"string"}}}`),
		MockResult:   json.RawMessage(`{"temp":72,"condition":"sunny"}`),
	}
	_ = toolRegistry.Register(mockWeatherTool)
	toolRegistry.RegisterExecutor(tools.NewMockStaticExecutor())

	middleware := ProviderMiddleware(mockProvider, toolRegistry, nil, nil)

	execCtx := &pipeline.ExecutionContext{
		AllowedTools: []string{"get_weather"},
		Messages: []types.Message{
			{Role: "user", Content: "What's the weather?"},
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)
	assert.NotNil(t, execCtx.Response)

	// Final response should have no tool calls (second response)
	assert.Len(t, execCtx.Response.ToolCalls, 0)

	// Check that tool results were collected
	assert.Len(t, execCtx.ToolResults, 1, "Expected 1 tool result")

	if len(execCtx.ToolResults) > 0 {
		t.Logf("Tool result: ID=%s, Name=%s, Content='%s', Error='%s'",
			execCtx.ToolResults[0].ID,
			execCtx.ToolResults[0].Name,
			execCtx.ToolResults[0].Content,
			execCtx.ToolResults[0].Error)

		assert.Equal(t, "call1", execCtx.ToolResults[0].ID)
		assert.Equal(t, "get_weather", execCtx.ToolResults[0].Name)
		// MockResult should be returned
		assert.NotEmpty(t, execCtx.ToolResults[0].Content, "Tool result content should not be empty")
	}

	// Check cost accumulation (both calls)
	assert.Equal(t, 25, execCtx.CostInfo.InputTokens)               // 10 + 15
	assert.Equal(t, 13, execCtx.CostInfo.OutputTokens)              // 5 + 8
	assert.InDelta(t, 0.0003, execCtx.CostInfo.TotalCost, 0.000001) // 0.0001 + 0.0002 (allow floating point error)

	// Check final response
	assert.Equal(t, "The weather in NYC is sunny and 72 degrees", execCtx.Response.Content)

	// Check that messages were accumulated
	// Should have: user message, assistant (with tool call), tool result, assistant (final)
	assert.Len(t, execCtx.Messages, 4)

	mockProvider.AssertExpectations(t)
}

func TestProviderMiddleware_MaxRoundsExceeded(t *testing.T) {
	mockProvider := new(MockProvider)

	// Always return a tool call
	response := providers.PredictionResponse{
		Content: "Calling tool",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call1",
				Name: "infinite_tool",
				Args: json.RawMessage(`{}`),
			},
		},
		CostInfo: &types.CostInfo{
			TotalCost: 0.0001,
		},
	}

	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	// Create mock tool registry
	toolRegistry := tools.NewRegistry()
	mockTool := &tools.ToolDescriptor{
		Name:        "infinite_tool",
		Description: "Infinite tool",
		Mode:        "mock",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		MockResult:  json.RawMessage(`{"result":"done"}`),
	}
	_ = toolRegistry.Register(mockTool)
	toolRegistry.RegisterExecutor(tools.NewMockStaticExecutor())

	toolPolicy := &pipeline.ToolPolicy{
		MaxRounds: 2, // Only allow 2 rounds
	}
	middleware := ProviderMiddleware(mockProvider, toolRegistry, toolPolicy, nil)

	execCtx := &pipeline.ExecutionContext{
		AllowedTools: []string{"infinite_tool"},
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeded max rounds")
}

func TestProviderMiddleware_BlockedTool(t *testing.T) {
	mockProvider := new(MockProvider)

	// Response with blocked tool call
	response := providers.PredictionResponse{
		Content: "Calling blocked tool",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call1",
				Name: "blocked_tool",
				Args: json.RawMessage(`{}`),
			},
		},
		CostInfo: &types.CostInfo{
			TotalCost: 0.0001,
		},
	}

	// Second response after blocked tool
	response2 := providers.PredictionResponse{
		Content: "Tool was blocked",
		CostInfo: &types.CostInfo{
			TotalCost: 0.0001,
		},
	}

	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil).Once()
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response2, nil).Once()

	toolPolicy := &pipeline.ToolPolicy{
		Blocklist: []string{"blocked_tool"},
	}
	middleware := ProviderMiddleware(mockProvider, tools.NewRegistry(), toolPolicy, nil)

	execCtx := &pipeline.ExecutionContext{
		AllowedTools: []string{"blocked_tool"},
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)

	// Tool result should indicate it was blocked
	assert.Len(t, execCtx.ToolResults, 1)
	assert.Contains(t, execCtx.ToolResults[0].Error, "blocked by policy")
}

func TestProviderMiddleware_CostAccumulation(t *testing.T) {
	mockProvider := new(MockProvider)

	response := providers.PredictionResponse{
		Content: "Response",
		CostInfo: &types.CostInfo{
			InputTokens:   100,
			OutputTokens:  50,
			CachedTokens:  20,
			InputCostUSD:  0.001,
			OutputCostUSD: 0.002,
			CachedCostUSD: 0.0001,
			TotalCost:     0.0031,
		},
	}

	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	middleware := ProviderMiddleware(mockProvider, nil, nil, nil)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
		CostInfo: types.CostInfo{
			InputTokens:   10,
			OutputTokens:  5,
			CachedTokens:  2,
			InputCostUSD:  0.0001,
			OutputCostUSD: 0.0002,
			CachedCostUSD: 0.00001,
			TotalCost:     0.00031,
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)

	// Check accumulated costs
	assert.Equal(t, 110, execCtx.CostInfo.InputTokens)       // 10 + 100
	assert.Equal(t, 55, execCtx.CostInfo.OutputTokens)       // 5 + 50
	assert.Equal(t, 22, execCtx.CostInfo.CachedTokens)       // 2 + 20
	assert.Equal(t, 0.0011, execCtx.CostInfo.InputCostUSD)   // 0.0001 + 0.001
	assert.Equal(t, 0.0022, execCtx.CostInfo.OutputCostUSD)  // 0.0002 + 0.002
	assert.Equal(t, 0.00011, execCtx.CostInfo.CachedCostUSD) // 0.00001 + 0.0001
	assert.Equal(t, 0.00341, execCtx.CostInfo.TotalCost)     // 0.00031 + 0.0031

	mockProvider.AssertExpectations(t)
}

func TestProviderMiddleware_ExecutionTrace(t *testing.T) {
	mockProvider := new(MockProvider)
	toolRegistry := tools.NewRegistry()

	// Register a simple mock tool
	echoTool := &tools.ToolDescriptor{
		Name:        "echo",
		Description: "Echoes back the input",
		Mode:        "mock",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"message": {"type": "string"}
			},
			"required": ["message"]
		}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"result":{"type":"string"}}}`),
		MockResult:   json.RawMessage(`{"result":"Hello from tool!"}`),
	}
	_ = toolRegistry.Register(echoTool)
	toolRegistry.RegisterExecutor(tools.NewMockStaticExecutor())

	// First LLM call - returns tool call
	firstResponse := providers.PredictionResponse{
		Content: "Let me echo that for you.",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call_1",
				Name: "echo",
				Args: json.RawMessage(`{"message": "Hello from tool!"}`),
			},
		},
		CostInfo: &types.CostInfo{
			InputTokens:   10,
			OutputTokens:  5,
			InputCostUSD:  0.0001,
			OutputCostUSD: 0.0001,
			TotalCost:     0.0002,
		},
		Latency: 100 * time.Millisecond,
	}

	// Second LLM call - final response after tool execution
	secondResponse := providers.PredictionResponse{
		Content: "I've echoed your message: Hello from tool!",
		CostInfo: &types.CostInfo{
			InputTokens:   20,
			OutputTokens:  10,
			InputCostUSD:  0.0002,
			OutputCostUSD: 0.0002,
			TotalCost:     0.0004,
		},
		Latency: 150 * time.Millisecond,
	}

	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(firstResponse, nil).Once()
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(secondResponse, nil).Once()

	middleware := ProviderMiddleware(mockProvider, toolRegistry, nil, nil)

	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "Please echo: Hello from tool!"},
		},
		Trace: pipeline.ExecutionTrace{
			LLMCalls:  make([]pipeline.LLMCall, 0),
			Events:    make([]pipeline.TraceEvent, 0),
			StartedAt: time.Now(),
		},
	}

	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)

	// Verify execution trace
	assert.Len(t, execCtx.Trace.LLMCalls, 2, "Should have 2 LLM calls in trace")

	// First call
	firstCall := execCtx.Trace.LLMCalls[0]
	assert.Equal(t, 1, firstCall.Sequence)
	assert.Equal(t, "Let me echo that for you.", firstCall.Response.Content)
	assert.Len(t, firstCall.ToolCalls, 1)
	assert.Equal(t, "echo", firstCall.ToolCalls[0].Name)
	assert.Equal(t, 10, firstCall.Cost.InputTokens)
	assert.Equal(t, 5, firstCall.Cost.OutputTokens)
	assert.GreaterOrEqual(t, firstCall.Duration.Milliseconds(), int64(0))

	// Second call
	secondCall := execCtx.Trace.LLMCalls[1]
	assert.Equal(t, 2, secondCall.Sequence)
	assert.Equal(t, "I've echoed your message: Hello from tool!", secondCall.Response.Content)
	assert.Len(t, secondCall.ToolCalls, 0)
	assert.Equal(t, 20, secondCall.Cost.InputTokens)
	assert.Equal(t, 10, secondCall.Cost.OutputTokens)
	assert.GreaterOrEqual(t, secondCall.Duration.Milliseconds(), int64(0))

	// Verify Response convenience field points to last call
	assert.Equal(t, secondCall.Response, execCtx.Response)

	// Verify aggregate costs
	assert.Equal(t, 30, execCtx.CostInfo.InputTokens)              // 10 + 20
	assert.Equal(t, 15, execCtx.CostInfo.OutputTokens)             // 5 + 10
	assert.InDelta(t, 0.0006, execCtx.CostInfo.TotalCost, 0.00001) // 0.0002 + 0.0004

	mockProvider.AssertExpectations(t)
}

func TestProviderMiddleware_SetsTimestamp(t *testing.T) {
	mockProvider := new(MockProvider)

	response := providers.PredictionResponse{
		Content: "Hello, world!",
		CostInfo: &types.CostInfo{
			InputTokens:   10,
			OutputTokens:  5,
			InputCostUSD:  0.0001,
			OutputCostUSD: 0.0001,
			TotalCost:     0.0002,
		},
		Latency: 100 * time.Millisecond,
	}

	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	providerConfig := &ProviderMiddlewareConfig{
		Temperature: 0.7,
		MaxTokens:   100,
	}
	middleware := ProviderMiddleware(mockProvider, nil, nil, providerConfig)

	// Record time before execution
	beforeExecution := time.Now()

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "You are a helpful assistant",
		Messages: []types.Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello", Timestamp: time.Now()},
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Provider middleware failed: %v", err)
	}

	// Record time after execution
	afterExecution := time.Now()

	assert.NoError(t, err)
	assert.NotNil(t, execCtx.Response)

	// Find the assistant message
	var assistantMsg *types.Message
	for i := range execCtx.Messages {
		if execCtx.Messages[i].Role == "assistant" {
			assistantMsg = &execCtx.Messages[i]
			break
		}
	}

	require.NotNil(t, assistantMsg, "Assistant message should be added to context")

	// Verify timestamp is set and within expected range
	assert.False(t, assistantMsg.Timestamp.IsZero(), "Assistant message timestamp should not be zero value")
	assert.True(t, assistantMsg.Timestamp.After(beforeExecution) || assistantMsg.Timestamp.Equal(beforeExecution),
		"Assistant message timestamp should be after or equal to execution start time")
	assert.True(t, assistantMsg.Timestamp.Before(afterExecution) || assistantMsg.Timestamp.Equal(afterExecution),
		"Assistant message timestamp should be before or equal to execution end time")

	mockProvider.AssertExpectations(t)
}

// TestCallProviderWithoutTools_RegularContent tests provider call with regular text content
func TestCallProviderWithoutTools_RegularContent(t *testing.T) {
	t.Parallel()

	mockProvider := new(MockProvider)
	ctx := context.Background()

	messages := []types.Message{
		{Role: "user", Content: "test"},
	}

	req := providers.PredictionRequest{
		Messages:    messages,
		MaxTokens:   100,
		Temperature: 0.7,
	}

	expectedResp := providers.PredictionResponse{Content: "response"}
	mockProvider.On("Predict", ctx, req).Return(expectedResp, nil)

	resp, err := callProviderWithoutTools(ctx, mockProvider, req)

	assert.NoError(t, err)
	assert.Equal(t, "response", resp.Content)
	mockProvider.AssertExpectations(t)
}

// TestCallProviderWithoutTools_MultimodalWithSupport tests multimodal provider with multimodal content
func TestCallProviderWithoutTools_MultimodalWithSupport(t *testing.T) {
	t.Parallel()

	mockProvider := &MockMultimodalProvider{}
	ctx := context.Background()

	url := "data:image/png;base64,abc"
	messages := []types.Message{
		{
			Role:    "user",
			Content: "",
			Parts: []types.ContentPart{
				{Type: "text", Text: stringPtr("test")},
				{Type: "image", Media: &types.MediaContent{URL: &url, MIMEType: "image/png"}},
			},
		},
	}

	req := providers.PredictionRequest{
		Messages:    messages,
		MaxTokens:   100,
		Temperature: 0.7,
	}

	expectedResp := providers.PredictionResponse{Content: "multimodal response"}
	mockProvider.On("PredictMultimodal", ctx, req).Return(expectedResp, nil)

	resp, err := callProviderWithoutTools(ctx, mockProvider, req)

	assert.NoError(t, err)
	assert.Equal(t, "multimodal response", resp.Content)
	mockProvider.AssertExpectations(t)
}

// TestCallProviderWithoutTools_MultimodalFallback tests regular provider falls back to Predict for multimodal
func TestCallProviderWithoutTools_MultimodalFallback(t *testing.T) {
	t.Parallel()

	mockProvider := new(MockProvider)
	ctx := context.Background()

	url := "data:image/png;base64,abc"
	messages := []types.Message{
		{
			Role:    "user",
			Content: "",
			Parts: []types.ContentPart{
				{Type: "text", Text: stringPtr("test")},
				{Type: "image", Media: &types.MediaContent{URL: &url, MIMEType: "image/png"}},
			},
		},
	}

	req := providers.PredictionRequest{
		Messages:    messages,
		MaxTokens:   100,
		Temperature: 0.7,
	}

	expectedResp := providers.PredictionResponse{Content: "regular response"}
	mockProvider.On("Predict", ctx, req).Return(expectedResp, nil)

	resp, err := callProviderWithoutTools(ctx, mockProvider, req)

	assert.NoError(t, err)
	assert.Equal(t, "regular response", resp.Content)
	mockProvider.AssertExpectations(t)
}

// stringPtr returns a pointer to a string
func stringPtr(s string) *string {
	return &s
}

// TestExecuteAndEmitToolCall_Events tests event emission during tool execution
func TestExecuteAndEmitToolCall_Events(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		toolName        string
		toolResult      *tools.ToolExecutionResult
		toolErr         error
		expectStarted   bool
		expectCompleted bool
		expectFailed    bool
		expectedStatus  string
	}{
		{
			name:            "successful tool execution",
			toolName:        "test_tool",
			toolResult:      &tools.ToolExecutionResult{Status: tools.ToolStatusComplete, Content: json.RawMessage(`"success"`)},
			expectStarted:   true,
			expectCompleted: true,
			expectFailed:    false,
			expectedStatus:  "success",
		},
		{
			name:            "tool returns error in result",
			toolName:        "test_tool",
			toolResult:      &tools.ToolExecutionResult{Status: tools.ToolStatusFailed, Error: "execution failed"},
			expectStarted:   true,
			expectCompleted: true,
			expectFailed:    false,
			expectedStatus:  "error",
		},
		{
			name:            "pending tool execution",
			toolName:        "test_tool",
			toolResult:      &tools.ToolExecutionResult{Status: tools.ToolStatusPending, PendingInfo: &tools.PendingToolInfo{Message: "pending"}},
			expectStarted:   true,
			expectCompleted: true,
			expectFailed:    false,
			expectedStatus:  "pending",
		},
		{
			name:            "tool with error result",
			toolName:        "test_tool",
			toolResult:      &tools.ToolExecutionResult{Status: tools.ToolStatusFailed, Error: "tool error"},
			expectStarted:   true,
			expectCompleted: true,
			expectFailed:    false,
			expectedStatus:  "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup tool registry
			registry := tools.NewRegistry()
			if tt.toolResult != nil {
				descriptor := &tools.ToolDescriptor{
					Name:         tt.toolName,
					Description:  "test tool",
					Mode:         "mock",
					InputSchema:  json.RawMessage(`{"type":"object"}`),
					OutputSchema: json.RawMessage(`{"type":"object"}`),
					MockResult:   json.RawMessage(`{"result":"success"}`),
				}
				err := registry.Register(descriptor)
				require.NoError(t, err)
				registry.RegisterExecutor(&MockToolExecutor{result: tt.toolResult, err: tt.toolErr})
			}

			// Setup event bus and emitter to verify events are fired
			bus := events.NewEventBus()
			emitter := events.NewEmitter(bus, "test-run", "test-session", "test-conv")

			var capturedEvents []*events.Event
			bus.SubscribeAll(func(event *events.Event) {
				capturedEvents = append(capturedEvents, event)
			})

			execCtx := &pipeline.ExecutionContext{
				EventEmitter: emitter,
				Messages:     []types.Message{},
			}

			call := types.MessageToolCall{
				ID:   "call-1",
				Name: tt.toolName,
				Args: json.RawMessage(`{}`),
			}

			// Execute
			_, _, err := executeAndEmitToolCall(execCtx, registry, nil, call)

			// Verify
			if tt.toolErr != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Wait for async event publication
			time.Sleep(50 * time.Millisecond)

			// Verify events were emitted
			if tt.expectStarted {
				found := false
				for _, e := range capturedEvents {
					if e.Type == events.EventToolCallStarted {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected ToolCallStarted event")
			}
			if tt.expectCompleted {
				found := false
				for _, e := range capturedEvents {
					if e.Type == events.EventToolCallCompleted {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected ToolCallCompleted event")
			}
			if tt.expectFailed {
				found := false
				for _, e := range capturedEvents {
					if e.Type == events.EventToolCallFailed {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected ToolCallFailed event")
			}
		})
	}
}

// TestExecuteAndEmitToolCall_NoEventEmitter tests execution without event emitter
func TestExecuteAndEmitToolCall_NoEventEmitter(t *testing.T) {
	t.Parallel()

	registry := tools.NewRegistry()
	descriptor := &tools.ToolDescriptor{
		Name:         "test_tool",
		Description:  "test tool",
		Mode:         "mock",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		MockResult:   json.RawMessage(`{"result":"success"}`),
	}
	err := registry.Register(descriptor)
	require.NoError(t, err)

	execCtx := &pipeline.ExecutionContext{
		EventEmitter: nil, // No emitter
		Messages:     []types.Message{},
	}

	call := types.MessageToolCall{
		ID:   "call-1",
		Name: "test_tool",
		Args: json.RawMessage(`{}`),
	}

	// Should not panic without event emitter
	result, _, err := executeAndEmitToolCall(execCtx, registry, nil, call)
	assert.NoError(t, err)
	assert.Equal(t, "call-1", result.ID)
}

// TestFormatToolResult tests tool result formatting
func TestFormatToolResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "string value",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "integer value",
			input:    42,
			expected: "42",
		},
		{
			name:     "int64 value",
			input:    int64(12345),
			expected: "12345",
		},
		{
			name:     "float64 value",
			input:    3.14,
			expected: "3.14",
		},
		{
			name:     "boolean true",
			input:    true,
			expected: "true",
		},
		{
			name:     "boolean false",
			input:    false,
			expected: "false",
		},
		{
			name:     "nil value",
			input:    nil,
			expected: "<nil>",
		},
		{
			name:     "map value",
			input:    map[string]interface{}{"key": "value", "num": 123},
			expected: `{"key":"value","num":123}`,
		},
		{
			name:     "slice value",
			input:    []string{"a", "b", "c"},
			expected: `["a","b","c"]`,
		},
		{
			name:     "struct value",
			input:    struct{ Name string }{"test"},
			expected: `{"Name":"test"}`,
		},
		{
			name:     "empty map",
			input:    map[string]interface{}{},
			expected: `{}`,
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: `[]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToolResult(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// MockMultimodalProvider implements both Provider and MultimodalSupport
type MockMultimodalProvider struct {
	mock.Mock
}

func (m *MockMultimodalProvider) ID() string {
	return "mock-multimodal"
}

func (m *MockMultimodalProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(providers.PredictionResponse), args.Error(1)
}

func (m *MockMultimodalProvider) PredictMultimodal(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(providers.PredictionResponse), args.Error(1)
}

func (m *MockMultimodalProvider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	return providers.MultimodalCapabilities{
		SupportsImages: true,
		SupportsAudio:  true,
		SupportsVideo:  true,
	}
}

func (m *MockMultimodalProvider) PredictMultimodalStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
}

func (m *MockMultimodalProvider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(<-chan providers.StreamChunk), args.Error(1)
}

func (m *MockMultimodalProvider) SupportsStreaming() bool {
	return false
}

func (m *MockMultimodalProvider) ShouldIncludeRawOutput() bool {
	return false
}

func (m *MockMultimodalProvider) Close() error {
	return nil
}

func (m *MockMultimodalProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}

// MockToolExecutor implements tools.AsyncToolExecutor interface
type MockToolExecutor struct {
	result *tools.ToolExecutionResult
	err    error
}

func (m *MockToolExecutor) Name() string {
	return "mock-test"
}

func (m *MockToolExecutor) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result.Content, nil
}

func (m *MockToolExecutor) ExecuteAsync(descriptor *tools.ToolDescriptor, args json.RawMessage) (*tools.ToolExecutionResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}
