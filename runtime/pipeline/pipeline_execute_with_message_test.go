package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteWithMessage_PreservesAllMessageFields verifies that ExecuteWithMessage
// preserves all fields from the input message (Meta, Timestamp, etc.)
func TestExecuteWithMessage_PreservesAllMessageFields(t *testing.T) {
	// Create a message with all fields populated
	timestamp := time.Now()
	metadata := map[string]interface{}{
		"persona":             "social-engineer",
		"role":                "self-play-user",
		"self_play_execution": true,
	}

	message := types.Message{
		Role:      "user",
		Content:   "Test message with metadata",
		Timestamp: timestamp,
		Meta: map[string]interface{}{
			"raw_response": metadata,
		},
	}

	// Create a middleware that verifies the message has all fields
	var capturedMessage types.Message
	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			require.Len(t, execCtx.Messages, 1, "Should have exactly one message")
			capturedMessage = execCtx.Messages[0]
			return next()
		},
	}

	p := NewPipeline(middleware)

	// Execute with the complete message
	result, err := p.ExecuteWithMessage(context.Background(), message)

	// Verify execution succeeded
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the message preserved all fields through middleware
	assert.Equal(t, "user", capturedMessage.Role)
	assert.Equal(t, "Test message with metadata", capturedMessage.Content)
	assert.Equal(t, timestamp, capturedMessage.Timestamp)
	assert.NotNil(t, capturedMessage.Meta["raw_response"])

	// Verify metadata content
	rawResponse, ok := capturedMessage.Meta["raw_response"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "social-engineer", rawResponse["persona"])
	assert.Equal(t, "self-play-user", rawResponse["role"])
	assert.Equal(t, true, rawResponse["self_play_execution"])

	// Verify result contains the message with all fields
	require.Len(t, result.Messages, 1)
	resultMessage := result.Messages[0]
	assert.Equal(t, message.Role, resultMessage.Role)
	assert.Equal(t, message.Content, resultMessage.Content)
	assert.Equal(t, message.Timestamp, resultMessage.Timestamp)
	assert.NotNil(t, resultMessage.Meta["raw_response"])
}

// TestExecuteWithMessage_EmptyMessage verifies that an empty message is handled correctly
func TestExecuteWithMessage_EmptyMessage(t *testing.T) {
	message := types.Message{}

	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			require.Len(t, execCtx.Messages, 1)
			assert.Equal(t, "", execCtx.Messages[0].Role)
			assert.Equal(t, "", execCtx.Messages[0].Content)
			return next()
		},
	}

	p := NewPipeline(middleware)

	result, err := p.ExecuteWithMessage(context.Background(), message)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Messages, 1)
}

// TestExecuteWithMessage_MiddlewareCanMutate verifies that middleware can still
// mutate messages even when using ExecuteWithMessage
func TestExecuteWithMessage_MiddlewareCanMutate(t *testing.T) {
	message := types.Message{
		Role:    "user",
		Content: "Original content",
	}

	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			// Middleware modifies the message
			execCtx.Messages[0].Content = "Modified content"
			execCtx.Messages[0].Meta = map[string]interface{}{
				"raw_response": map[string]interface{}{"added": "by_middleware"},
			}
			return next()
		},
	}

	p := NewPipeline(middleware)

	result, err := p.ExecuteWithMessage(context.Background(), message)

	require.NoError(t, err)
	require.Len(t, result.Messages, 1)

	// Verify middleware modifications are preserved
	assert.Equal(t, "Modified content", result.Messages[0].Content)
	assert.NotNil(t, result.Messages[0].Meta["raw_response"])
}

// TestExecuteWithMessage_WithToolCalls verifies that tool calls are preserved
func TestExecuteWithMessage_WithToolCalls(t *testing.T) {
	argsJSON := []byte(`{"city": "NYC"}`)
	message := types.Message{
		Role:    "assistant",
		Content: "Let me check that for you",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call_123",
				Name: "get_weather",
				Args: argsJSON,
			},
		},
	}

	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			require.Len(t, execCtx.Messages, 1)
			require.Len(t, execCtx.Messages[0].ToolCalls, 1)
			assert.Equal(t, "call_123", execCtx.Messages[0].ToolCalls[0].ID)
			assert.Equal(t, "get_weather", execCtx.Messages[0].ToolCalls[0].Name)
			return next()
		},
	}

	p := NewPipeline(middleware)

	result, err := p.ExecuteWithMessage(context.Background(), message)

	require.NoError(t, err)
	require.Len(t, result.Messages, 1)
	require.Len(t, result.Messages[0].ToolCalls, 1)
	assert.Equal(t, "call_123", result.Messages[0].ToolCalls[0].ID)
}

// TestExecuteWithMessage_WithCostInfo verifies that cost info is preserved
func TestExecuteWithMessage_WithCostInfo(t *testing.T) {
	message := types.Message{
		Role:    "assistant",
		Content: "Response",
		CostInfo: &types.CostInfo{
			InputTokens:  100,
			OutputTokens: 50,
			TotalCost:    0.001,
		},
	}

	p := NewPipeline(&noOpMiddleware{})

	result, err := p.ExecuteWithMessage(context.Background(), message)

	require.NoError(t, err)
	require.Len(t, result.Messages, 1)
	require.NotNil(t, result.Messages[0].CostInfo)
	assert.Equal(t, 100, result.Messages[0].CostInfo.InputTokens)
	assert.Equal(t, 50, result.Messages[0].CostInfo.OutputTokens)
}

// TestExecuteWithMessage_CompareWithExecute verifies that ExecuteWithMessage
// produces the same result as Execute for simple role/content cases
func TestExecuteWithMessage_CompareWithExecute(t *testing.T) {
	role := "user"
	content := "Test message"

	// Middleware that adds metadata
	addMetadata := func(execCtx *ExecutionContext, next func() error) error {
		execCtx.Metadata["test_key"] = "test_value"
		return next()
	}

	// Execute with original method
	p1 := NewPipeline(&funcMiddleware{fn: addMetadata})
	result1, err1 := p1.Execute(context.Background(), role, content)
	require.NoError(t, err1)

	// Execute with new method
	message := types.Message{Role: role, Content: content}
	p2 := NewPipeline(&funcMiddleware{fn: addMetadata})
	result2, err2 := p2.ExecuteWithMessage(context.Background(), message)
	require.NoError(t, err2)

	// Results should be equivalent
	assert.Equal(t, len(result1.Messages), len(result2.Messages))
	assert.Equal(t, result1.Messages[0].Role, result2.Messages[0].Role)
	assert.Equal(t, result1.Messages[0].Content, result2.Messages[0].Content)
	assert.Equal(t, result1.Metadata["test_key"], result2.Metadata["test_key"])
}

// TestExecuteWithMessage_ContextCancellation verifies that context cancellation works
func TestExecuteWithMessage_ContextCancellation(t *testing.T) {
	message := types.Message{Role: "user", Content: "test"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			// Check if context is cancelled
			select {
			case <-execCtx.Context.Done():
				return execCtx.Context.Err()
			default:
			}
			return next()
		},
	}

	p := NewPipeline(middleware)

	_, err := p.ExecuteWithMessage(ctx, message)

	// Should get context cancellation error
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
