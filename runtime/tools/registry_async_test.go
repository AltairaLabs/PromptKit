package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRegistryAsyncExecutor implements AsyncToolExecutor for testing registry
type mockRegistryAsyncExecutor struct {
	shouldPend bool
	shouldFail bool
}

func (m *mockRegistryAsyncExecutor) Name() string {
	return "mock-async"
}

func (m *mockRegistryAsyncExecutor) Execute(descriptor *ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	// Fallback to sync execution
	return json.RawMessage(`{"result": "sync"}`), nil
}

func (m *mockRegistryAsyncExecutor) ExecuteAsync(descriptor *ToolDescriptor, args json.RawMessage) (*ToolExecutionResult, error) {
	if m.shouldFail {
		return &ToolExecutionResult{
			Status: ToolStatusFailed,
			Error:  "async execution failed",
		}, nil
	}

	if m.shouldPend {
		return &ToolExecutionResult{
			Status: ToolStatusPending,
			PendingInfo: &PendingToolInfo{
				Reason:   "requires_approval",
				Message:  "This operation requires approval",
				ToolName: descriptor.Name,
				Args:     args,
			},
		}, nil
	}

	return &ToolExecutionResult{
		Status:  ToolStatusComplete,
		Content: json.RawMessage(`{"result": "async_complete"}`),
	}, nil
}

func TestRegistry_ExecuteAsync_Complete(t *testing.T) {
	registry := NewRegistry()

	// Register async executor
	asyncExecutor := &mockRegistryAsyncExecutor{shouldPend: false, shouldFail: false}
	registry.RegisterExecutor(asyncExecutor)

	// Register a tool that uses the async executor
	tool := &ToolDescriptor{
		Name:         "test_async_tool",
		Description:  "Test async tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"result": {"type": "string"}}}`),
		Mode:         "live", // Use "live" mode to trigger the mock-async executor
		TimeoutMs:    1000,
	}

	// Replace the "http" executor with our mock async executor
	registry.executors["http"] = asyncExecutor

	err := registry.Register(tool)
	require.NoError(t, err)

	// Execute with ExecuteAsync
	result, err := registry.ExecuteAsync("test_async_tool", json.RawMessage(`{"name": "test"}`))
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, ToolStatusComplete, result.Status)
	assert.Contains(t, string(result.Content), "async_complete")
	assert.Empty(t, result.Error)
	assert.Nil(t, result.PendingInfo)
}

func TestRegistry_ExecuteAsync_Pending(t *testing.T) {
	registry := NewRegistry()

	// Register async executor that returns pending
	asyncExecutor := &mockRegistryAsyncExecutor{shouldPend: true, shouldFail: false}
	registry.RegisterExecutor(asyncExecutor)

	// Register a tool
	tool := &ToolDescriptor{
		Name:         "test_pending_tool",
		Description:  "Test pending tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"result": {"type": "string"}}}`),
		Mode:         "live",
		TimeoutMs:    1000,
	}

	registry.executors["http"] = asyncExecutor
	err := registry.Register(tool)
	require.NoError(t, err)

	// Execute with ExecuteAsync
	result, err := registry.ExecuteAsync("test_pending_tool", json.RawMessage(`{"name": "test"}`))
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, ToolStatusPending, result.Status)
	assert.Empty(t, string(result.Content))
	assert.Empty(t, result.Error)
	require.NotNil(t, result.PendingInfo)
	assert.Equal(t, "requires_approval", result.PendingInfo.Reason)
	assert.Equal(t, "This operation requires approval", result.PendingInfo.Message)
	assert.Equal(t, "test_pending_tool", result.PendingInfo.ToolName)
}

func TestRegistry_ExecuteAsync_Failed(t *testing.T) {
	registry := NewRegistry()

	// Register async executor that fails
	asyncExecutor := &mockRegistryAsyncExecutor{shouldPend: false, shouldFail: true}
	registry.RegisterExecutor(asyncExecutor)

	// Register a tool
	tool := &ToolDescriptor{
		Name:         "test_failing_tool",
		Description:  "Test failing tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"result": {"type": "string"}}}`),
		Mode:         "live",
		TimeoutMs:    1000,
	}

	registry.executors["http"] = asyncExecutor
	err := registry.Register(tool)
	require.NoError(t, err)

	// Execute with ExecuteAsync
	result, err := registry.ExecuteAsync("test_failing_tool", json.RawMessage(`{"name": "test"}`))
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, ToolStatusFailed, result.Status)
	assert.Empty(t, string(result.Content))
	assert.Equal(t, "async execution failed", result.Error)
	assert.Nil(t, result.PendingInfo)
}

func TestRegistry_ExecuteAsync_FallbackToSync(t *testing.T) {
	registry := NewRegistry()

	// Register a regular (non-async) tool using mock-static executor
	tool := &ToolDescriptor{
		Name:         "test_sync_tool",
		Description:  "Test sync tool",
		InputSchema:  json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"result": {"type": "string"}}}`),
		Mode:         "mock",
		MockResult:   json.RawMessage(`{"result": "sync_mock"}`),
		TimeoutMs:    1000,
	}

	err := registry.Register(tool)
	require.NoError(t, err)

	// Execute with ExecuteAsync - should fall back to sync execution
	result, err := registry.ExecuteAsync("test_sync_tool", json.RawMessage(`{"name": "test"}`))
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, ToolStatusComplete, result.Status)
	assert.Contains(t, string(result.Content), "sync_mock")
	assert.Empty(t, result.Error)
	assert.Nil(t, result.PendingInfo)
}
