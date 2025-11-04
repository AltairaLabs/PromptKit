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

// Test buildProviderTooling helper function
func TestBuildProviderTooling(t *testing.T) {
	t.Run("returns nil when no tool registry", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			AllowedTools: []string{"tool1"},
		}

		providerTools, toolChoice, err := buildProviderTooling(nil, nil, execCtx, nil)

		assert.NoError(t, err)
		assert.Nil(t, providerTools)
		assert.Empty(t, toolChoice)
	})

	t.Run("returns nil when no allowed tools", func(t *testing.T) {
		registry := tools.NewRegistry()
		execCtx := &pipeline.ExecutionContext{
			AllowedTools: []string{},
		}

		providerTools, toolChoice, err := buildProviderTooling(nil, registry, execCtx, nil)

		assert.NoError(t, err)
		assert.Nil(t, providerTools)
		assert.Empty(t, toolChoice)
	})

	t.Run("returns nil when provider doesn't support tools", func(t *testing.T) {
		mockProvider := new(MockProvider)
		registry := tools.NewRegistry()

		tool := &tools.ToolDescriptor{
			Name:        "test_tool",
			Description: "Test tool",
			Mode:        "mock",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			MockResult:  json.RawMessage(`{"result":"test"}`),
		}
		_ = registry.Register(tool)

		execCtx := &pipeline.ExecutionContext{
			AllowedTools: []string{"test_tool"},
		}

		providerTools, toolChoice, err := buildProviderTooling(mockProvider, registry, execCtx, nil)

		assert.NoError(t, err)
		assert.Nil(t, providerTools)
		assert.Empty(t, toolChoice)
	})

	t.Run("builds tools for provider with tool support", func(t *testing.T) {
		mockProvider := new(MockProviderWithTools)
		registry := tools.NewRegistry()

		tool := &tools.ToolDescriptor{
			Name:        "test_tool",
			Description: "Test tool",
			Mode:        "mock",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			MockResult:  json.RawMessage(`{"result":"test"}`),
		}
		_ = registry.Register(tool)

		// Use mock.MatchedBy to match any tool descriptor slice
		mockProvider.On("BuildTooling", mock.MatchedBy(func(descriptors []*providers.ToolDescriptor) bool {
			return len(descriptors) == 1 && descriptors[0].Name == "test_tool"
		})).Return([]string{"tool1"}, nil)

		execCtx := &pipeline.ExecutionContext{
			AllowedTools: []string{"test_tool"},
		}

		providerTools, toolChoice, err := buildProviderTooling(mockProvider, registry, execCtx, nil)

		assert.NoError(t, err)
		assert.NotNil(t, providerTools)
		assert.Equal(t, "auto", toolChoice) // Default
		mockProvider.AssertExpectations(t)
	})

	t.Run("respects policy tool choice", func(t *testing.T) {
		mockProvider := new(MockProviderWithTools)
		registry := tools.NewRegistry()

		tool := &tools.ToolDescriptor{
			Name:        "test_tool",
			Description: "Test tool",
			Mode:        "mock",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			MockResult:  json.RawMessage(`{"result":"test"}`),
		}
		_ = registry.Register(tool)

		mockProvider.On("BuildTooling", mock.MatchedBy(func(descriptors []*providers.ToolDescriptor) bool {
			return len(descriptors) == 1 && descriptors[0].Name == "test_tool"
		})).Return([]string{"tool1"}, nil)

		policy := &pipeline.ToolPolicy{
			ToolChoice: "required",
		}

		execCtx := &pipeline.ExecutionContext{
			AllowedTools: []string{"test_tool"},
		}

		_, toolChoice, err := buildProviderTooling(mockProvider, registry, execCtx, policy)

		assert.NoError(t, err)
		assert.Equal(t, "required", toolChoice)
		mockProvider.AssertExpectations(t)
	})
}

// Test createAssistantMessage helper function
func TestCreateAssistantMessage(t *testing.T) {
	t.Run("creates basic message", func(t *testing.T) {
		content := "Hello, world!"
		duration := 100 * time.Millisecond

		msg := createAssistantMessage(content, nil, nil, duration)

		assert.Equal(t, "assistant", msg.Role)
		assert.Equal(t, "Hello, world!", msg.Content)
		assert.Equal(t, int64(100), msg.LatencyMs)
		assert.Equal(t, "pipeline", msg.Source)
		assert.False(t, msg.Timestamp.IsZero())
	})

	t.Run("includes tool calls", func(t *testing.T) {
		toolCalls := []types.MessageToolCall{
			{ID: "call1", Name: "tool1", Args: json.RawMessage(`{}`)},
		}

		msg := createAssistantMessage("content", toolCalls, nil, 0)

		assert.Len(t, msg.ToolCalls, 1)
		assert.Equal(t, "call1", msg.ToolCalls[0].ID)
	})

	t.Run("includes cost info", func(t *testing.T) {
		costInfo := &types.CostInfo{
			InputTokens:  10,
			OutputTokens: 5,
			TotalCost:    0.001,
		}

		msg := createAssistantMessage("content", nil, costInfo, 0)

		require.NotNil(t, msg.CostInfo)
		assert.Equal(t, 10, msg.CostInfo.InputTokens)
		assert.Equal(t, 5, msg.CostInfo.OutputTokens)
		assert.Equal(t, 0.001, msg.CostInfo.TotalCost)
	})
}

// Test recordLLMCall helper function
func TestRecordLLMCall(t *testing.T) {
	t.Run("does nothing when tracing disabled", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Messages: []types.Message{
				{Role: "user", Content: "test"},
			},
			Trace: pipeline.ExecutionTrace{
				LLMCalls: []pipeline.LLMCall{},
			},
		}

		config := &ProviderMiddlewareConfig{
			DisableTrace: true,
		}

		response := &pipeline.Response{Content: "response"}
		startTime := time.Now()
		duration := 100 * time.Millisecond

		recordLLMCall(execCtx, config, response, startTime, duration, nil, nil)

		assert.Len(t, execCtx.Trace.LLMCalls, 0)
	})

	t.Run("records call when tracing enabled", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Messages: []types.Message{
				{Role: "user", Content: "test"},
			},
			Trace: pipeline.ExecutionTrace{
				LLMCalls: []pipeline.LLMCall{},
			},
		}

		config := &ProviderMiddlewareConfig{
			DisableTrace: false,
		}

		response := &pipeline.Response{Content: "response"}
		startTime := time.Now()
		duration := 100 * time.Millisecond
		costInfo := &types.CostInfo{
			InputTokens:  10,
			OutputTokens: 5,
			TotalCost:    0.001,
		}

		messageIndex := len(execCtx.Messages)
		recordLLMCall(execCtx, config, response, startTime, duration, costInfo, nil)

		require.Len(t, execCtx.Trace.LLMCalls, 1)
		llmCall := execCtx.Trace.LLMCalls[0]

		assert.Equal(t, 1, llmCall.Sequence)
		assert.Equal(t, messageIndex, llmCall.MessageIndex)
		assert.Equal(t, response, llmCall.Response)
		assert.Equal(t, duration, llmCall.Duration)
		assert.Equal(t, 10, llmCall.Cost.InputTokens)
		assert.Equal(t, 5, llmCall.Cost.OutputTokens)
	})

	t.Run("increments sequence number", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Messages: []types.Message{
				{Role: "user", Content: "test"},
			},
			Trace: pipeline.ExecutionTrace{
				LLMCalls: []pipeline.LLMCall{
					{Sequence: 1},
					{Sequence: 2},
				},
			},
		}

		config := &ProviderMiddlewareConfig{}
		response := &pipeline.Response{Content: "response"}

		recordLLMCall(execCtx, config, response, time.Now(), 0, nil, nil)

		require.Len(t, execCtx.Trace.LLMCalls, 3)
		assert.Equal(t, 3, execCtx.Trace.LLMCalls[2].Sequence)
	})
}

// Test accumulateCost helper function
func TestAccumulateCost(t *testing.T) {
	t.Run("accumulates all cost fields", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			CostInfo: types.CostInfo{
				InputTokens:   10,
				OutputTokens:  5,
				CachedTokens:  2,
				InputCostUSD:  0.001,
				OutputCostUSD: 0.002,
				CachedCostUSD: 0.0001,
				TotalCost:     0.0031,
			},
		}

		newCost := &types.CostInfo{
			InputTokens:   20,
			OutputTokens:  10,
			CachedTokens:  4,
			InputCostUSD:  0.002,
			OutputCostUSD: 0.004,
			CachedCostUSD: 0.0002,
			TotalCost:     0.0062,
		}

		accumulateCost(execCtx, newCost)

		assert.Equal(t, 30, execCtx.CostInfo.InputTokens)
		assert.Equal(t, 15, execCtx.CostInfo.OutputTokens)
		assert.Equal(t, 6, execCtx.CostInfo.CachedTokens)
		assert.InDelta(t, 0.003, execCtx.CostInfo.InputCostUSD, 0.000001)
		assert.InDelta(t, 0.006, execCtx.CostInfo.OutputCostUSD, 0.000001)
		assert.InDelta(t, 0.0003, execCtx.CostInfo.CachedCostUSD, 0.000001)
		assert.InDelta(t, 0.0093, execCtx.CostInfo.TotalCost, 0.000001)
	})

	t.Run("handles nil cost info", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			CostInfo: types.CostInfo{
				TotalCost: 0.001,
			},
		}

		accumulateCost(execCtx, nil)

		// Should not panic, values unchanged
		assert.Equal(t, 0.001, execCtx.CostInfo.TotalCost)
	})
}

// Test checkRoundLimit helper function
func TestCheckRoundLimit(t *testing.T) {
	t.Run("allows execution within default limit", func(t *testing.T) {
		err := checkRoundLimit(5, nil)
		assert.NoError(t, err)
	})

	t.Run("blocks execution exceeding default limit", func(t *testing.T) {
		err := checkRoundLimit(11, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeded max rounds (10)")
	})

	t.Run("respects custom policy limit", func(t *testing.T) {
		policy := &pipeline.ToolPolicy{MaxRounds: 3}
		err := checkRoundLimit(3, policy)
		assert.NoError(t, err)

		err = checkRoundLimit(4, policy)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeded max rounds (3)")
	})
}

// Test checkToolCallLimit helper function
func TestCheckToolCallLimit(t *testing.T) {
	t.Run("no limit when policy is nil", func(t *testing.T) {
		err := checkToolCallLimit(100, nil)
		assert.NoError(t, err)
	})

	t.Run("no limit when MaxToolCallsPerTurn is 0", func(t *testing.T) {
		policy := &pipeline.ToolPolicy{MaxToolCallsPerTurn: 0}
		err := checkToolCallLimit(100, policy)
		assert.NoError(t, err)
	})

	t.Run("allows calls within limit", func(t *testing.T) {
		policy := &pipeline.ToolPolicy{MaxToolCallsPerTurn: 5}
		err := checkToolCallLimit(5, policy)
		assert.NoError(t, err)
	})

	t.Run("blocks calls exceeding limit", func(t *testing.T) {
		policy := &pipeline.ToolPolicy{MaxToolCallsPerTurn: 5}
		err := checkToolCallLimit(6, policy)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeded max tool calls per turn (5)")
	})
}

// Test addToolResultMessages helper function
func TestAddToolResultMessages(t *testing.T) {
	t.Run("adds tool result messages to context", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Messages: []types.Message{
				{Role: "user", Content: "test"},
			},
		}

		toolResults := []types.MessageToolResult{
			{
				ID:        "call1",
				Name:      "tool1",
				Content:   "result1",
				LatencyMs: 100,
			},
			{
				ID:        "call2",
				Name:      "tool2",
				Content:   "result2",
				Error:     "error2",
				LatencyMs: 200,
			},
		}

		addToolResultMessages(execCtx, toolResults)

		assert.Len(t, execCtx.Messages, 3)

		// Check first tool message
		toolMsg1 := execCtx.Messages[1]
		assert.Equal(t, "tool", toolMsg1.Role)
		assert.Equal(t, "result1", toolMsg1.Content)
		assert.Equal(t, "pipeline", toolMsg1.Source)
		require.NotNil(t, toolMsg1.ToolResult)
		assert.Equal(t, "call1", toolMsg1.ToolResult.ID)
		assert.Equal(t, "tool1", toolMsg1.ToolResult.Name)
		assert.Equal(t, int64(100), toolMsg1.ToolResult.LatencyMs)

		// Check second tool message
		toolMsg2 := execCtx.Messages[2]
		assert.Equal(t, "tool", toolMsg2.Role)
		assert.Equal(t, "result2", toolMsg2.Content)
		require.NotNil(t, toolMsg2.ToolResult)
		assert.Equal(t, "error2", toolMsg2.ToolResult.Error)
	})

	t.Run("handles empty results", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Messages: []types.Message{
				{Role: "user", Content: "test"},
			},
		}

		addToolResultMessages(execCtx, []types.MessageToolResult{})

		assert.Len(t, execCtx.Messages, 1)
	})
}

// Test processToolCallRound helper function
func TestProcessToolCallRound(t *testing.T) {
	t.Run("returns false when no tool calls", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
		}
		registry := tools.NewRegistry()

		hasMore, err := processToolCallRound(execCtx, registry, nil, []types.MessageToolCall{})

		assert.NoError(t, err)
		assert.False(t, hasMore)
	})

	t.Run("returns true and executes tools", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
			Messages: []types.Message{
				{Role: "user", Content: "test"},
			},
		}

		registry := tools.NewRegistry()
		tool := &tools.ToolDescriptor{
			Name:        "test_tool",
			Description: "Test tool",
			Mode:        "mock",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			MockResult:  json.RawMessage(`{"result":"success"}`),
		}
		_ = registry.Register(tool)
		registry.RegisterExecutor(tools.NewMockStaticExecutor())

		toolCalls := []types.MessageToolCall{
			{ID: "call1", Name: "test_tool", Args: json.RawMessage(`{}`)},
		}

		hasMore, err := processToolCallRound(execCtx, registry, nil, toolCalls)

		assert.NoError(t, err)
		assert.True(t, hasMore)
		assert.Len(t, execCtx.ToolResults, 1)
		assert.Len(t, execCtx.Messages, 2) // user + tool result
	})

	t.Run("respects tool call limit", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
		}
		registry := tools.NewRegistry()
		policy := &pipeline.ToolPolicy{MaxToolCallsPerTurn: 1}

		toolCalls := []types.MessageToolCall{
			{ID: "call1", Name: "tool1", Args: json.RawMessage(`{}`)},
			{ID: "call2", Name: "tool2", Args: json.RawMessage(`{}`)},
		}

		_, err := processToolCallRound(execCtx, registry, policy, toolCalls)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeded max tool calls per turn")
	})

	t.Run("stops on pending tools", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
			Messages: []types.Message{
				{Role: "user", Content: "test"},
			},
			Metadata: make(map[string]interface{}),
		}

		// Set up a mock tool that returns pending status
		registry := tools.NewRegistry()
		mockTool := &tools.ToolDescriptor{
			Name:        "pending_tool",
			Description: "Tool that requires approval",
			Mode:        "mock",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			MockResult:  json.RawMessage(`{"result":"mock"}`),
		}
		_ = registry.Register(mockTool)

		// Register async executor that returns pending
		asyncExecutor := &mockAsyncTool{status: tools.ToolStatusPending}
		registry.RegisterExecutor(asyncExecutor)

		toolCalls := []types.MessageToolCall{
			{ID: "call1", Name: "pending_tool", Args: json.RawMessage(`{}`)},
		}

		// Process the tool call - it should set pending state and then detect it
		_, err := processToolCallRound(execCtx, registry, nil, toolCalls)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pending tool calls require approval")
		assert.True(t, execCtx.HasPendingToolCalls())
	})
}

// Mock provider with tool support for testing
type MockProviderWithTools struct {
	MockProvider
}

func (m *MockProviderWithTools) BuildTooling(descriptors []*providers.ToolDescriptor) (interface{}, error) {
	args := m.Called(descriptors)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (m *MockProviderWithTools) ChatWithTools(ctx context.Context, req providers.ChatRequest, tooling interface{}, toolChoice string) (providers.ChatResponse, []types.MessageToolCall, error) {
	args := m.Called(ctx, req, tooling, toolChoice)
	return args.Get(0).(providers.ChatResponse), args.Get(1).([]types.MessageToolCall), args.Error(2)
}
