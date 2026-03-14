package session

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncTestExecutor executes all tools synchronously with a canned result.
type syncTestExecutor struct {
	result json.RawMessage
}

func (e *syncTestExecutor) Name() string { return "test" }
func (e *syncTestExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	return e.result, nil
}

// pendingTestExecutor always returns ToolStatusPending.
type pendingTestExecutor struct{}

func (e *pendingTestExecutor) Name() string { return "pending" }
func (e *pendingTestExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	return nil, fmt.Errorf("should not be called via sync Execute")
}
func (e *pendingTestExecutor) ExecuteAsync(
	_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	return &tools.ToolExecutionResult{
		Status: tools.ToolStatusPending,
		PendingInfo: &tools.PendingToolInfo{
			Reason:   "client_tool_deferred",
			Message:  fmt.Sprintf("Client tool %q awaiting caller fulfillment", d.Name),
			ToolName: d.Name,
		},
	}, nil
}

// errorTestExecutor always returns an error for ExecuteAsync.
type errorTestExecutor struct{}

func (e *errorTestExecutor) Name() string { return "errmode" }
func (e *errorTestExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	return nil, fmt.Errorf("tool execution failed")
}

func makeRegistry(executors ...tools.Executor) *tools.Registry {
	r := tools.NewRegistry()
	for _, ex := range executors {
		r.RegisterExecutor(ex)
	}
	return r
}

func registerTool(t *testing.T, reg *tools.Registry, name, mode string) {
	t.Helper()
	err := reg.Register(&tools.ToolDescriptor{
		Name:        name,
		Description: "test tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode:        mode,
	})
	require.NoError(t, err)
}

func TestExecuteDuplexToolCalls_AllCompleted(t *testing.T) {
	reg := makeRegistry(&syncTestExecutor{result: json.RawMessage(`{"ok":true}`)})
	registerTool(t, reg, "tool_a", "test")
	registerTool(t, reg, "tool_b", "test")

	toolCalls := []types.MessageToolCall{
		{ID: "c1", Name: "tool_a", Args: json.RawMessage(`{}`)},
		{ID: "c2", Name: "tool_b", Args: json.RawMessage(`{}`)},
	}

	result := executeDuplexToolCalls(reg, toolCalls)

	assert.Len(t, result.Completed.ProviderResponses, 2)
	assert.Len(t, result.Completed.ResultMessages, 2)
	assert.Empty(t, result.Pending)

	assert.Equal(t, "c1", result.Completed.ProviderResponses[0].ToolCallID)
	assert.Equal(t, "c2", result.Completed.ProviderResponses[1].ToolCallID)
	assert.False(t, result.Completed.ProviderResponses[0].IsError)
}

func TestExecuteDuplexToolCalls_AllPending(t *testing.T) {
	reg := makeRegistry(&pendingTestExecutor{})
	registerTool(t, reg, "client_tool", "pending")

	toolCalls := []types.MessageToolCall{
		{ID: "c1", Name: "client_tool", Args: json.RawMessage(`{"key":"val"}`)},
		{ID: "c2", Name: "client_tool", Args: json.RawMessage(`{}`)},
	}

	result := executeDuplexToolCalls(reg, toolCalls)

	assert.Empty(t, result.Completed.ProviderResponses)
	assert.Empty(t, result.Completed.ResultMessages)
	assert.Len(t, result.Pending, 2)

	assert.Equal(t, "c1", result.Pending[0].CallID)
	assert.Equal(t, "client_tool", result.Pending[0].ToolName)
	assert.Equal(t, map[string]any{"key": "val"}, result.Pending[0].Args)
}

func TestExecuteDuplexToolCalls_Mixed(t *testing.T) {
	reg := makeRegistry(
		&syncTestExecutor{result: json.RawMessage(`{"done":true}`)},
		&pendingTestExecutor{},
	)
	registerTool(t, reg, "sync_tool", "test")
	registerTool(t, reg, "client_tool", "pending")

	toolCalls := []types.MessageToolCall{
		{ID: "c1", Name: "sync_tool", Args: json.RawMessage(`{}`)},
		{ID: "c2", Name: "client_tool", Args: json.RawMessage(`{}`)},
	}

	result := executeDuplexToolCalls(reg, toolCalls)

	assert.Len(t, result.Completed.ProviderResponses, 1)
	assert.Equal(t, "c1", result.Completed.ProviderResponses[0].ToolCallID)
	assert.Len(t, result.Pending, 1)
	assert.Equal(t, "c2", result.Pending[0].CallID)
}

func TestExecuteDuplexToolCalls_Error(t *testing.T) {
	reg := makeRegistry(&errorTestExecutor{})
	registerTool(t, reg, "bad_tool", "errmode")

	toolCalls := []types.MessageToolCall{
		{ID: "c1", Name: "bad_tool", Args: json.RawMessage(`{}`)},
	}

	result := executeDuplexToolCalls(reg, toolCalls) // Individual tool errors don't fail the batch

	assert.Len(t, result.Completed.ProviderResponses, 1)
	assert.True(t, result.Completed.ProviderResponses[0].IsError)
	assert.Contains(t, result.Completed.ProviderResponses[0].Result, "tool execution failed")
}

func TestExecuteDuplexToolCalls_UnknownTool(t *testing.T) {
	reg := makeRegistry()

	toolCalls := []types.MessageToolCall{
		{ID: "c1", Name: "nonexistent", Args: json.RawMessage(`{}`)},
	}

	result := executeDuplexToolCalls(reg, toolCalls)

	assert.Len(t, result.Completed.ProviderResponses, 1)
	assert.True(t, result.Completed.ProviderResponses[0].IsError)
}
