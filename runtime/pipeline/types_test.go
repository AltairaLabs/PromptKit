package pipeline

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMCall_JSONSerialization(t *testing.T) {
	// Test that LLMCall with error can be serialized to JSON
	startTime := time.Now()
	testError := errors.New("test error message")

	llmCall := LLMCall{
		Sequence:     1,
		MessageIndex: 0,
		Response: &Response{
			Content: "test response",
		},
		StartedAt: startTime,
		Duration:  100 * time.Millisecond,
		Cost: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.001,
		},
	}
	llmCall.SetError(testError)

	// Serialize to JSON
	jsonBytes, err := json.Marshal(llmCall)
	require.NoError(t, err, "should be able to marshal LLMCall to JSON")

	// Deserialize from JSON
	var deserialized LLMCall
	err = json.Unmarshal(jsonBytes, &deserialized)
	require.NoError(t, err, "should be able to unmarshal LLMCall from JSON")

	// Verify fields
	assert.Equal(t, llmCall.Sequence, deserialized.Sequence)
	assert.Equal(t, llmCall.MessageIndex, deserialized.MessageIndex)
	assert.Equal(t, llmCall.Response.Content, deserialized.Response.Content)

	// The error field should now serialize/deserialize properly
	assert.NotNil(t, deserialized.Error)
	assert.Equal(t, "test error message", *deserialized.Error)

	// Test GetError helper
	deserializedErr := deserialized.GetError()
	assert.NotNil(t, deserializedErr)
	assert.Equal(t, "test error message", deserializedErr.Error())
}

func TestLLMCall_JSONSerialization_NilError(t *testing.T) {
	// Test that LLMCall without error serializes properly
	startTime := time.Now()

	llmCall := LLMCall{
		Sequence:     1,
		MessageIndex: 0,
		Response: &Response{
			Content: "test response",
		},
		StartedAt: startTime,
		Duration:  100 * time.Millisecond,
		Cost: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.001,
		},
		Error: nil,
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(llmCall)
	require.NoError(t, err, "should be able to marshal LLMCall to JSON")

	// Check JSON doesn't contain error field
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonBytes, &jsonMap)
	require.NoError(t, err)

	// error field should be omitted when nil
	_, hasError := jsonMap["error"]
	assert.False(t, hasError, "error field should be omitted when nil")
}

func TestExecutionTrace_JSONSerialization(t *testing.T) {
	// Test that ExecutionTrace with LLMCalls containing errors serializes properly
	startTime := time.Now()
	completedTime := startTime.Add(1 * time.Second)

	llmCall1 := LLMCall{
		Sequence:     1,
		MessageIndex: 0,
		Response: &Response{
			Content: "success",
		},
		StartedAt: startTime,
		Duration:  100 * time.Millisecond,
	}

	llmCall2 := LLMCall{
		Sequence:     2,
		MessageIndex: 1,
		Response:     nil,
		StartedAt:    startTime.Add(200 * time.Millisecond),
		Duration:     50 * time.Millisecond,
	}
	llmCall2.SetError(errors.New("API rate limit exceeded"))

	trace := ExecutionTrace{
		StartedAt:   startTime,
		CompletedAt: &completedTime,
		LLMCalls: []LLMCall{
			llmCall1,
			llmCall2,
		},
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(trace)
	require.NoError(t, err, "should be able to marshal ExecutionTrace to JSON")

	// Deserialize from JSON
	var deserialized ExecutionTrace
	err = json.Unmarshal(jsonBytes, &deserialized)
	require.NoError(t, err, "should be able to unmarshal ExecutionTrace from JSON")

	// Verify structure
	assert.Len(t, deserialized.LLMCalls, 2)
	assert.Equal(t, 1, deserialized.LLMCalls[0].Sequence)
	assert.Equal(t, 2, deserialized.LLMCalls[1].Sequence)

	// Verify error handling
	assert.Nil(t, deserialized.LLMCalls[0].Error)
	assert.NotNil(t, deserialized.LLMCalls[1].Error)
	assert.Contains(t, *deserialized.LLMCalls[1].Error, "rate limit")

	// Test GetError helper
	err1 := deserialized.LLMCalls[0].GetError()
	assert.Nil(t, err1)

	err2 := deserialized.LLMCalls[1].GetError()
	assert.NotNil(t, err2)
	assert.Contains(t, err2.Error(), "rate limit")
}

func TestLLMCall_SetError_NilError(t *testing.T) {
	llmCall := LLMCall{
		Sequence: 1,
	}

	// Set an error first
	llmCall.SetError(errors.New("some error"))
	assert.NotNil(t, llmCall.Error)

	// Now set nil - should clear the error
	llmCall.SetError(nil)
	assert.Nil(t, llmCall.Error)
	assert.Nil(t, llmCall.GetError())
}

func TestExecutionContext_InterruptStream(t *testing.T) {
	ctx := &ExecutionContext{
		StreamMode: true,
	}

	assert.False(t, ctx.StreamInterrupted)
	assert.Equal(t, "", ctx.InterruptReason)

	// Interrupt the stream
	ctx.InterruptStream("rate limit exceeded")

	assert.True(t, ctx.StreamInterrupted)
	assert.Equal(t, "rate limit exceeded", ctx.InterruptReason)
}

func TestExecutionContext_PendingToolCalls(t *testing.T) {
	ctx := &ExecutionContext{}

	// Initially no pending tool calls
	assert.False(t, ctx.HasPendingToolCalls())
	assert.Nil(t, ctx.GetPendingToolCall("nonexistent"))

	// Add a pending tool call
	toolCall1 := types.MessageToolCall{
		ID:   "call-1",
		Name: "search",
	}
	ctx.AddPendingToolCall(toolCall1)

	assert.True(t, ctx.HasPendingToolCalls())
	assert.Len(t, ctx.PendingToolCalls, 1)

	// Retrieve the tool call
	retrieved := ctx.GetPendingToolCall("call-1")
	assert.NotNil(t, retrieved)
	assert.Equal(t, "call-1", retrieved.ID)
	assert.Equal(t, "search", retrieved.Name)

	// Add another tool call
	toolCall2 := types.MessageToolCall{
		ID:   "call-2",
		Name: "calculator",
	}
	ctx.AddPendingToolCall(toolCall2)
	assert.Len(t, ctx.PendingToolCalls, 2)

	// Remove a tool call
	removed := ctx.RemovePendingToolCall("call-1")
	assert.True(t, removed)
	assert.Len(t, ctx.PendingToolCalls, 1)
	assert.Nil(t, ctx.GetPendingToolCall("call-1"))
	assert.NotNil(t, ctx.GetPendingToolCall("call-2"))

	// Try to remove non-existent tool call
	removed = ctx.RemovePendingToolCall("nonexistent")
	assert.False(t, removed)
	assert.Len(t, ctx.PendingToolCalls, 1)

	// Clear all pending tool calls
	ctx.ClearPendingToolCalls()
	assert.False(t, ctx.HasPendingToolCalls())
	assert.Len(t, ctx.PendingToolCalls, 0)
}

func TestExecutionContext_IsStreaming(t *testing.T) {
	ctx := &ExecutionContext{
		StreamMode: false,
	}
	assert.False(t, ctx.IsStreaming())

	ctx.StreamMode = true
	assert.True(t, ctx.IsStreaming())
}

func TestExecutionContext_RecordLLMCall(t *testing.T) {
	ctx := &ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	startTime := time.Now()
	duration := 100 * time.Millisecond
	response := &Response{
		Content: "Hi there",
	}
	costInfo := &types.CostInfo{
		InputTokens:  10,
		OutputTokens: 20,
		TotalCost:    0.001,
	}

	// Record an LLM call
	ctx.RecordLLMCall(false, response, startTime, duration, costInfo, nil)

	assert.Len(t, ctx.Trace.LLMCalls, 1)
	llmCall := ctx.Trace.LLMCalls[0]
	assert.Equal(t, 1, llmCall.Sequence)
	assert.Equal(t, 1, llmCall.MessageIndex) // Current length before append
	assert.Equal(t, response, llmCall.Response)
	assert.Equal(t, startTime, llmCall.StartedAt)
	assert.Equal(t, duration, llmCall.Duration)
	assert.Equal(t, 10, llmCall.Cost.InputTokens)
	assert.Equal(t, 20, llmCall.Cost.OutputTokens)
	assert.Equal(t, 0.001, llmCall.Cost.TotalCost)

	// Record another call
	ctx.RecordLLMCall(false, response, startTime, duration, costInfo, nil)
	assert.Len(t, ctx.Trace.LLMCalls, 2)
	assert.Equal(t, 2, ctx.Trace.LLMCalls[1].Sequence)
}

func TestExecutionContext_RecordLLMCall_DisableTrace(t *testing.T) {
	ctx := &ExecutionContext{}

	// Record with tracing disabled
	ctx.RecordLLMCall(true, &Response{Content: "test"}, time.Now(), time.Second, nil, nil)

	// Should not record anything
	assert.Len(t, ctx.Trace.LLMCalls, 0)
}

func TestExecutionContext_RecordLLMCall_NilCostInfo(t *testing.T) {
	ctx := &ExecutionContext{}

	// Record with nil cost info
	ctx.RecordLLMCall(false, &Response{Content: "test"}, time.Now(), time.Second, nil, nil)

	assert.Len(t, ctx.Trace.LLMCalls, 1)
	// Cost should be zero-initialized
	assert.Equal(t, 0, ctx.Trace.LLMCalls[0].Cost.InputTokens)
	assert.Equal(t, 0, ctx.Trace.LLMCalls[0].Cost.OutputTokens)
	assert.Equal(t, 0.0, ctx.Trace.LLMCalls[0].Cost.TotalCost)
}
