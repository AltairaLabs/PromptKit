package middleware

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestProviderMiddleware_MessageIndexTracking verifies that LLMCall.MessageIndex
// correctly points to the assistant message in execCtx.Messages
func TestProviderMiddleware_MessageIndexTracking(t *testing.T) {
	mockProvider := new(MockProvider)

	// Mock a simple response
	mockProvider.On("Chat", mock.Anything, mock.Anything).Return(
		providers.ChatResponse{
			Content: "Hello!",
			CostInfo: &types.CostInfo{
				InputTokens:  10,
				OutputTokens: 5,
				TotalCost:    0.001,
			},
			Latency: 100 * time.Millisecond,
		},
		nil,
	)

	providerConfig := &ProviderMiddlewareConfig{
		Temperature: 0.7,
		MaxTokens:   100,
		// DisableTrace not set - defaults to false (tracing enabled)
	}
	middleware := ProviderMiddleware(mockProvider, nil, nil, providerConfig)

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "You are a helpful assistant",
		Messages: []types.Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello", Timestamp: time.Now()},
		},
		Trace: pipeline.ExecutionTrace{
			LLMCalls:  []pipeline.LLMCall{},
			StartedAt: time.Now(),
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	require.NoError(t, err)

	// Verify trace was populated
	require.Len(t, execCtx.Trace.LLMCalls, 1, "Should have 1 LLM call")

	llmCall := execCtx.Trace.LLMCalls[0]

	// Verify MessageIndex points to the assistant message
	require.GreaterOrEqual(t, llmCall.MessageIndex, 0, "MessageIndex should be set")
	require.Less(t, llmCall.MessageIndex, len(execCtx.Messages), "MessageIndex should be within bounds")

	assistantMsg := execCtx.Messages[llmCall.MessageIndex]
	assert.Equal(t, "assistant", assistantMsg.Role, "MessageIndex should point to assistant message")
	assert.Equal(t, "Hello!", assistantMsg.Content, "MessageIndex should point to the correct assistant message")

	mockProvider.AssertExpectations(t)
}

// TestProviderMiddleware_MessageIndexWithTools verifies MessageIndex tracking
// when tools are involved (multiple LLM calls)
func TestProviderMiddleware_MessageIndexWithTools(t *testing.T) {
	mockProvider := new(MockProvider)

	// First response with tool call
	firstResponse := providers.ChatResponse{
		Content: "I'll check the weather",
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
	secondResponse := providers.ChatResponse{
		Content: "Weather is sunny",
		CostInfo: &types.CostInfo{
			InputTokens:  15,
			OutputTokens: 8,
			TotalCost:    0.0002,
		},
		Latency: 150 * time.Millisecond,
	}

	mockProvider.On("Chat", mock.Anything, mock.Anything).Return(firstResponse, nil).Once()
	mockProvider.On("Chat", mock.Anything, mock.Anything).Return(secondResponse, nil).Once()

	// Create tool registry
	toolRegistry := tools.NewRegistry()
	mockWeatherTool := &tools.ToolDescriptor{
		Name:         "get_weather",
		Description:  "Get weather",
		Mode:         "mock",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		MockResult:   json.RawMessage(`{"temp":72}`),
	}
	_ = toolRegistry.Register(mockWeatherTool)
	toolRegistry.RegisterExecutor(tools.NewMockStaticExecutor())

	middleware := ProviderMiddleware(mockProvider, toolRegistry, nil, &ProviderMiddlewareConfig{
		// DisableTrace not set - defaults to false (tracing enabled)
	})

	execCtx := &pipeline.ExecutionContext{
		Context:      context.Background(),
		AllowedTools: []string{"get_weather"},
		Messages: []types.Message{
			{Role: "user", Content: "What's the weather?"},
		},
		Trace: pipeline.ExecutionTrace{},
	}

	err := middleware.Process(execCtx, func() error { return nil })
	assert.NoError(t, err)

	// Verify that we have 2 LLM calls (one with tool, one final)
	assert.Len(t, execCtx.Trace.LLMCalls, 2, "Expected 2 LLM calls")

	// Verify MessageIndex for first LLM call
	// Messages: [user, assistant-with-tools, tool-result, assistant-final]
	// First assistant message should be at index 1
	assert.Equal(t, 1, execCtx.Trace.LLMCalls[0].MessageIndex, "First LLM call should point to message index 1")

	// Second assistant message should be at index 3
	assert.Equal(t, 3, execCtx.Trace.LLMCalls[1].MessageIndex, "Second LLM call should point to message index 3")

	// Verify the messages at these indices are actually assistant messages
	assert.Equal(t, "assistant", execCtx.Messages[1].Role)
	assert.Equal(t, "assistant", execCtx.Messages[3].Role)

	mockProvider.AssertExpectations(t)
}

// TestProviderMiddleware_TraceDisabled verifies that when EnableTrace is false,
// the trace is not populated
func TestProviderMiddleware_TraceDisabled(t *testing.T) {
	mockProvider := new(MockProvider)

	mockProvider.On("Chat", mock.Anything, mock.Anything).Return(
		providers.ChatResponse{
			Content: "Hello!",
			CostInfo: &types.CostInfo{
				InputTokens:  10,
				OutputTokens: 5,
				TotalCost:    0.001,
			},
			Latency: 100 * time.Millisecond,
		},
		nil,
	)

	providerConfig := &ProviderMiddlewareConfig{
		Temperature:  0.7,
		MaxTokens:    100,
		DisableTrace: true, // Explicitly disable tracing
	}
	middleware := ProviderMiddleware(mockProvider, nil, nil, providerConfig)

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "You are a helpful assistant",
		Messages: []types.Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello", Timestamp: time.Now()},
		},
		Trace: pipeline.ExecutionTrace{
			LLMCalls:  []pipeline.LLMCall{},
			StartedAt: time.Now(),
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	require.NoError(t, err)

	// Verify trace was NOT populated
	assert.Len(t, execCtx.Trace.LLMCalls, 0, "Should have no LLM calls when tracing is disabled")

	// But message should still be added
	var foundAssistant bool
	for _, msg := range execCtx.Messages {
		if msg.Role == "assistant" && msg.Content == "Hello!" {
			foundAssistant = true
			break
		}
	}
	assert.True(t, foundAssistant, "Assistant message should still be added even when tracing is disabled")

	mockProvider.AssertExpectations(t)
}

// TestProviderMiddleware_TraceDefaultEnabled verifies that tracing is enabled by default
func TestProviderMiddleware_TraceDefaultEnabled(t *testing.T) {
	mockProvider := new(MockProvider)

	mockProvider.On("Chat", mock.Anything, mock.Anything).Return(
		providers.ChatResponse{
			Content: "Hello!",
			CostInfo: &types.CostInfo{
				InputTokens:  10,
				OutputTokens: 5,
				TotalCost:    0.001,
			},
			Latency: 100 * time.Millisecond,
		},
		nil,
	)

	// Don't set EnableTrace - test default behavior
	providerConfig := &ProviderMiddlewareConfig{
		Temperature: 0.7,
		MaxTokens:   100,
		// EnableTrace not set - should default to true
	}
	middleware := ProviderMiddleware(mockProvider, nil, nil, providerConfig)

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "You are a helpful assistant",
		Messages: []types.Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello", Timestamp: time.Now()},
		},
		Trace: pipeline.ExecutionTrace{
			LLMCalls:  []pipeline.LLMCall{},
			StartedAt: time.Now(),
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	require.NoError(t, err)

	// Verify trace WAS populated (default is enabled)
	assert.Len(t, execCtx.Trace.LLMCalls, 1, "Should have 1 LLM call when tracing is enabled by default")

	mockProvider.AssertExpectations(t)
}
