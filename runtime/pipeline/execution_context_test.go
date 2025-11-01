package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutionContext_AddPendingToolCall(t *testing.T) {
	ctx := &ExecutionContext{
		Context: context.Background(),
	}

	toolCall := types.MessageToolCall{
		ID:   "call_123",
		Name: "send_email",
		Args: json.RawMessage(`{"to": "user@example.com"}`),
	}

	ctx.AddPendingToolCall(toolCall)

	assert.Len(t, ctx.PendingToolCalls, 1)
	assert.Equal(t, "call_123", ctx.PendingToolCalls[0].ID)
	assert.Equal(t, "send_email", ctx.PendingToolCalls[0].Name)
}

func TestExecutionContext_HasPendingToolCalls(t *testing.T) {
	ctx := &ExecutionContext{
		Context: context.Background(),
	}

	// Initially empty
	assert.False(t, ctx.HasPendingToolCalls())

	// After adding
	ctx.AddPendingToolCall(types.MessageToolCall{
		ID:   "call_123",
		Name: "test_tool",
		Args: json.RawMessage(`{}`),
	})
	assert.True(t, ctx.HasPendingToolCalls())

	// After clearing
	ctx.ClearPendingToolCalls()
	assert.False(t, ctx.HasPendingToolCalls())
}

func TestExecutionContext_GetPendingToolCall(t *testing.T) {
	ctx := &ExecutionContext{
		Context: context.Background(),
	}

	toolCall1 := types.MessageToolCall{
		ID:   "call_123",
		Name: "tool_one",
		Args: json.RawMessage(`{"a": 1}`),
	}
	toolCall2 := types.MessageToolCall{
		ID:   "call_456",
		Name: "tool_two",
		Args: json.RawMessage(`{"b": 2}`),
	}

	ctx.AddPendingToolCall(toolCall1)
	ctx.AddPendingToolCall(toolCall2)

	// Find existing
	result := ctx.GetPendingToolCall("call_123")
	require.NotNil(t, result)
	assert.Equal(t, "call_123", result.ID)
	assert.Equal(t, "tool_one", result.Name)

	// Find second one
	result = ctx.GetPendingToolCall("call_456")
	require.NotNil(t, result)
	assert.Equal(t, "call_456", result.ID)
	assert.Equal(t, "tool_two", result.Name)

	// Not found
	result = ctx.GetPendingToolCall("call_999")
	assert.Nil(t, result)
}

func TestExecutionContext_RemovePendingToolCall(t *testing.T) {
	ctx := &ExecutionContext{
		Context: context.Background(),
	}

	toolCall1 := types.MessageToolCall{
		ID:   "call_123",
		Name: "tool_one",
		Args: json.RawMessage(`{}`),
	}
	toolCall2 := types.MessageToolCall{
		ID:   "call_456",
		Name: "tool_two",
		Args: json.RawMessage(`{}`),
	}
	toolCall3 := types.MessageToolCall{
		ID:   "call_789",
		Name: "tool_three",
		Args: json.RawMessage(`{}`),
	}

	ctx.AddPendingToolCall(toolCall1)
	ctx.AddPendingToolCall(toolCall2)
	ctx.AddPendingToolCall(toolCall3)

	// Remove middle one
	removed := ctx.RemovePendingToolCall("call_456")
	assert.True(t, removed)
	assert.Len(t, ctx.PendingToolCalls, 2)
	assert.Equal(t, "call_123", ctx.PendingToolCalls[0].ID)
	assert.Equal(t, "call_789", ctx.PendingToolCalls[1].ID)

	// Remove first one
	removed = ctx.RemovePendingToolCall("call_123")
	assert.True(t, removed)
	assert.Len(t, ctx.PendingToolCalls, 1)
	assert.Equal(t, "call_789", ctx.PendingToolCalls[0].ID)

	// Remove non-existent
	removed = ctx.RemovePendingToolCall("call_999")
	assert.False(t, removed)
	assert.Len(t, ctx.PendingToolCalls, 1)

	// Remove last one
	removed = ctx.RemovePendingToolCall("call_789")
	assert.True(t, removed)
	assert.Len(t, ctx.PendingToolCalls, 0)
}

func TestExecutionContext_ClearPendingToolCalls(t *testing.T) {
	ctx := &ExecutionContext{
		Context: context.Background(),
	}

	// Add multiple
	ctx.AddPendingToolCall(types.MessageToolCall{
		ID:   "call_1",
		Name: "tool_1",
		Args: json.RawMessage(`{}`),
	})
	ctx.AddPendingToolCall(types.MessageToolCall{
		ID:   "call_2",
		Name: "tool_2",
		Args: json.RawMessage(`{}`),
	})
	ctx.AddPendingToolCall(types.MessageToolCall{
		ID:   "call_3",
		Name: "tool_3",
		Args: json.RawMessage(`{}`),
	})

	assert.Len(t, ctx.PendingToolCalls, 3)

	// Clear all
	ctx.ClearPendingToolCalls()
	assert.Len(t, ctx.PendingToolCalls, 0)
	assert.False(t, ctx.HasPendingToolCalls())

	// Clear when already empty (should be safe)
	ctx.ClearPendingToolCalls()
	assert.Len(t, ctx.PendingToolCalls, 0)
}

func TestExecutionContext_PendingToolCallsMultipleOperations(t *testing.T) {
	ctx := &ExecutionContext{
		Context: context.Background(),
	}

	// Add, check, remove, check again
	ctx.AddPendingToolCall(types.MessageToolCall{
		ID:   "call_1",
		Name: "tool_1",
		Args: json.RawMessage(`{"x": 1}`),
	})

	assert.True(t, ctx.HasPendingToolCalls())
	result := ctx.GetPendingToolCall("call_1")
	require.NotNil(t, result)

	removed := ctx.RemovePendingToolCall("call_1")
	assert.True(t, removed)
	assert.False(t, ctx.HasPendingToolCalls())

	result = ctx.GetPendingToolCall("call_1")
	assert.Nil(t, result)
}
