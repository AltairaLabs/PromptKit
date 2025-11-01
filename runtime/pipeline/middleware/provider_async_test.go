package middleware

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAsyncTool implements AsyncToolExecutor for testing
type mockAsyncTool struct {
	status tools.ToolExecutionStatus
}

func (m *mockAsyncTool) Name() string {
	return "mock-static" // Must match the executor name the registry uses
}

func (m *mockAsyncTool) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"result": "sync"}`), nil
}

func (m *mockAsyncTool) ExecuteAsync(descriptor *tools.ToolDescriptor, args json.RawMessage) (*tools.ToolExecutionResult, error) {
	switch m.status {
	case tools.ToolStatusPending:
		return &tools.ToolExecutionResult{
			Status: tools.ToolStatusPending,
			PendingInfo: &tools.PendingToolInfo{
				Reason:   "requires_approval",
				Message:  "This action requires approval",
				ToolName: descriptor.Name,
				Args:     args,
				Metadata: map[string]interface{}{
					"risk_level": "high",
				},
			},
		}, nil

	case tools.ToolStatusFailed:
		return &tools.ToolExecutionResult{
			Status: tools.ToolStatusFailed,
			Error:  "tool execution failed",
		}, nil

	default: // ToolStatusComplete
		return &tools.ToolExecutionResult{
			Status:  tools.ToolStatusComplete,
			Content: json.RawMessage(`{"result": "success"}`),
		}, nil
	}
}

func TestExecuteToolCalls_Pending(t *testing.T) {
	// Setup registry with async tool
	registry := tools.NewRegistry()
	asyncExecutor := &mockAsyncTool{status: tools.ToolStatusPending}

	// Register with name that will be used for "mock" mode tools
	registry.RegisterExecutor(asyncExecutor)

	tool := &tools.ToolDescriptor{
		Name:         "pending_tool",
		Description:  "Test pending tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"action": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"result": {"type": "string"}}}`),
		Mode:         "mock",                                // Use mock mode
		MockResult:   json.RawMessage(`{"result": "mock"}`), // Required for mock mode
		TimeoutMs:    1000,
	}

	err := registry.Register(tool)
	require.NoError(t, err)

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Metadata: make(map[string]interface{}),
	}

	// Create tool calls
	toolCalls := []types.MessageToolCall{
		{
			ID:   "call_123",
			Name: "pending_tool",
			Args: json.RawMessage(`{"action": "delete_user"}`),
		},
	}

	// Execute tool calls
	results, err := executeToolCalls(execCtx, registry, nil, toolCalls)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Verify result message
	assert.Equal(t, "call_123", results[0].ID)
	assert.Equal(t, "pending_tool", results[0].Name)
	assert.Contains(t, results[0].Content, "approval")
	assert.Empty(t, results[0].Error)

	// Verify pending tool was added to ExecutionContext
	assert.True(t, execCtx.HasPendingToolCalls())
	assert.Len(t, execCtx.PendingToolCalls, 1)
	assert.Equal(t, "call_123", execCtx.PendingToolCalls[0].ID)
	assert.Equal(t, "pending_tool", execCtx.PendingToolCalls[0].Name)

	// Verify metadata contains pending tool info
	require.Contains(t, execCtx.Metadata, "pending_tools")
	pendingTools := execCtx.Metadata["pending_tools"].([]interface{})
	require.Len(t, pendingTools, 1)

	pendingInfo := pendingTools[0].(*tools.PendingToolInfo)
	assert.Equal(t, "requires_approval", pendingInfo.Reason)
	assert.Equal(t, "This action requires approval", pendingInfo.Message)
	assert.Equal(t, "pending_tool", pendingInfo.ToolName)
	assert.Equal(t, "high", pendingInfo.Metadata["risk_level"])
}

func TestExecuteToolCalls_Complete(t *testing.T) {
	// Setup registry with async tool that completes
	registry := tools.NewRegistry()
	asyncExecutor := &mockAsyncTool{status: tools.ToolStatusComplete}
	registry.RegisterExecutor(asyncExecutor)

	tool := &tools.ToolDescriptor{
		Name:         "complete_tool",
		Description:  "Test complete tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"action": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"result": {"type": "string"}}}`),
		Mode:         "mock",
		MockResult:   json.RawMessage(`{"result": "mock"}`),
		TimeoutMs:    1000,
	}

	err := registry.Register(tool)
	require.NoError(t, err)

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Metadata: make(map[string]interface{}),
	}

	// Create tool calls
	toolCalls := []types.MessageToolCall{
		{
			ID:   "call_456",
			Name: "complete_tool",
			Args: json.RawMessage(`{"action": "get_info"}`),
		},
	}

	// Execute tool calls
	results, err := executeToolCalls(execCtx, registry, nil, toolCalls)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Verify result
	assert.Equal(t, "call_456", results[0].ID)
	assert.Equal(t, "complete_tool", results[0].Name)
	assert.Contains(t, results[0].Content, "success")
	assert.Empty(t, results[0].Error)

	// Verify NO pending tools in ExecutionContext
	assert.False(t, execCtx.HasPendingToolCalls())
	assert.Len(t, execCtx.PendingToolCalls, 0)

	// Verify metadata does NOT contain pending_tools
	assert.NotContains(t, execCtx.Metadata, "pending_tools")
}

func TestExecuteToolCalls_Failed(t *testing.T) {
	// Setup registry with async tool that fails
	registry := tools.NewRegistry()
	asyncExecutor := &mockAsyncTool{status: tools.ToolStatusFailed}
	registry.RegisterExecutor(asyncExecutor)

	tool := &tools.ToolDescriptor{
		Name:         "failing_tool",
		Description:  "Test failing tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"action": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"result": {"type": "string"}}}`),
		Mode:         "mock",
		MockResult:   json.RawMessage(`{"result": "mock"}`),
		TimeoutMs:    1000,
	}

	err := registry.Register(tool)
	require.NoError(t, err)

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Metadata: make(map[string]interface{}),
	}

	// Create tool calls
	toolCalls := []types.MessageToolCall{
		{
			ID:   "call_789",
			Name: "failing_tool",
			Args: json.RawMessage(`{"action": "bad_action"}`),
		},
	}

	// Execute tool calls
	results, err := executeToolCalls(execCtx, registry, nil, toolCalls)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Verify result shows failure
	assert.Equal(t, "call_789", results[0].ID)
	assert.Equal(t, "failing_tool", results[0].Name)
	assert.Contains(t, results[0].Content, "failed")
	assert.Equal(t, "tool execution failed", results[0].Error)

	// Verify NO pending tools in ExecutionContext
	assert.False(t, execCtx.HasPendingToolCalls())
	assert.Len(t, execCtx.PendingToolCalls, 0)
}

func TestExecuteToolCalls_MultiplePending(t *testing.T) {
	// Setup registry with async tool
	registry := tools.NewRegistry()
	asyncExecutor := &mockAsyncTool{status: tools.ToolStatusPending}
	registry.RegisterExecutor(asyncExecutor)

	tool := &tools.ToolDescriptor{
		Name:         "multi_pending_tool",
		Description:  "Test multiple pending tools",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"action": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"result": {"type": "string"}}}`),
		Mode:         "mock",
		MockResult:   json.RawMessage(`{"result": "mock"}`),
		TimeoutMs:    1000,
	}

	err := registry.Register(tool)
	require.NoError(t, err)

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Metadata: make(map[string]interface{}),
	}

	// Create multiple tool calls
	toolCalls := []types.MessageToolCall{
		{
			ID:   "call_1",
			Name: "multi_pending_tool",
			Args: json.RawMessage(`{"action": "action_1"}`),
		},
		{
			ID:   "call_2",
			Name: "multi_pending_tool",
			Args: json.RawMessage(`{"action": "action_2"}`),
		},
		{
			ID:   "call_3",
			Name: "multi_pending_tool",
			Args: json.RawMessage(`{"action": "action_3"}`),
		},
	}

	// Execute tool calls
	results, err := executeToolCalls(execCtx, registry, nil, toolCalls)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Verify all are pending
	assert.True(t, execCtx.HasPendingToolCalls())
	assert.Len(t, execCtx.PendingToolCalls, 3)

	// Verify metadata contains all pending tool infos
	require.Contains(t, execCtx.Metadata, "pending_tools")
	pendingTools := execCtx.Metadata["pending_tools"].([]interface{})
	require.Len(t, pendingTools, 3)
}
