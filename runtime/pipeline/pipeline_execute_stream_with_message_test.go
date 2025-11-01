package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteStreamWithMessage_PreservesAllMessageFields verifies that ExecuteStreamWithMessage
// preserves all fields from the input message in streaming mode
func TestExecuteStreamWithMessage_PreservesAllMessageFields(t *testing.T) {
	timestamp := time.Now()
	metadata := map[string]interface{}{
		"persona":             "social-engineer",
		"role":                "self-play-user",
		"self_play_execution": true,
	}

	message := types.Message{
		Role:      "user",
		Content:   "Test streaming message with metadata",
		Timestamp: timestamp,
		Meta: map[string]interface{}{
			"raw_response": metadata,
		},
	}

	// Create a middleware that verifies the message and emits a chunk
	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			require.True(t, execCtx.IsStreaming(), "Should be in streaming mode")
			require.Len(t, execCtx.Messages, 1, "Should have exactly one message")

			msg := execCtx.Messages[0]
			assert.Equal(t, "user", msg.Role)
			assert.Equal(t, "Test streaming message with metadata", msg.Content)
			assert.Equal(t, timestamp, msg.Timestamp)
			assert.NotNil(t, msg.Meta["raw_response"])

			// Emit a test chunk
			finishReason := "stop"
			execCtx.EmitStreamChunk(providers.StreamChunk{
				Content:      "Test chunk",
				FinishReason: &finishReason,
			})

			return next()
		},
	}

	p := NewPipeline(middleware)

	streamChan, err := p.ExecuteStreamWithMessage(context.Background(), message)
	require.NoError(t, err)
	require.NotNil(t, streamChan)

	// Collect all chunks
	var chunks []providers.StreamChunk
	for chunk := range streamChan {
		chunks = append(chunks, chunk)
	}

	// Should have at least 2 chunks: test chunk + final chunk with result
	require.GreaterOrEqual(t, len(chunks), 2)

	// Verify final chunk has result with preserved message
	finalChunk := chunks[len(chunks)-1]
	require.NotNil(t, finalChunk.FinalResult)

	// Type assert to get the actual ExecutionResult
	result, ok := finalChunk.FinalResult.(*ExecutionResult)
	require.True(t, ok, "FinalResult should be *ExecutionResult")
	require.Len(t, result.Messages, 1)

	resultMessage := result.Messages[0]
	assert.Equal(t, message.Role, resultMessage.Role)
	assert.Equal(t, message.Content, resultMessage.Content)
	assert.Equal(t, message.Timestamp, resultMessage.Timestamp)
	assert.NotNil(t, resultMessage.Meta["raw_response"])
}

// TestExecuteStreamWithMessage_EmptyMessage verifies empty message handling in streaming mode
func TestExecuteStreamWithMessage_EmptyMessage(t *testing.T) {
	message := types.Message{}

	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			require.Len(t, execCtx.Messages, 1)
			assert.Equal(t, "", execCtx.Messages[0].Role)
			return next()
		},
	}

	p := NewPipeline(middleware)

	streamChan, err := p.ExecuteStreamWithMessage(context.Background(), message)
	require.NoError(t, err)

	// Drain the channel
	for range streamChan {
	}
}

// TestExecuteStreamWithMessage_WithToolCalls verifies tool calls are preserved in streaming
func TestExecuteStreamWithMessage_WithToolCalls(t *testing.T) {
	argsJSON := []byte(`{"city": "NYC"}`)
	message := types.Message{
		Role:    "assistant",
		Content: "Let me check that",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call_456",
				Name: "get_weather",
				Args: argsJSON,
			},
		},
	}

	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			require.Len(t, execCtx.Messages[0].ToolCalls, 1)
			assert.Equal(t, "call_456", execCtx.Messages[0].ToolCalls[0].ID)
			return next()
		},
	}

	p := NewPipeline(middleware)

	streamChan, err := p.ExecuteStreamWithMessage(context.Background(), message)
	require.NoError(t, err)

	// Get final result
	var finalResult *ExecutionResult
	for chunk := range streamChan {
		if chunk.FinalResult != nil {
			result, ok := chunk.FinalResult.(*ExecutionResult)
			if ok {
				finalResult = result
			}
		}
	}

	require.NotNil(t, finalResult)
	require.Len(t, finalResult.Messages, 1)
	require.Len(t, finalResult.Messages[0].ToolCalls, 1)
	assert.Equal(t, "call_456", finalResult.Messages[0].ToolCalls[0].ID)
}

// TestExecuteStreamWithMessage_ChunkProcessing verifies middleware can process chunks
func TestExecuteStreamWithMessage_ChunkProcessing(t *testing.T) {
	message := types.Message{Role: "user", Content: "Test"}

	var chunkCount int

	streamMiddleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			// Emit multiple chunks
			for i := 0; i < 3; i++ {
				execCtx.EmitStreamChunk(providers.StreamChunk{
					Content: "chunk",
				})
			}
			return next()
		},
	}

	p := NewPipeline(streamMiddleware)

	streamChan, err := p.ExecuteStreamWithMessage(context.Background(), message)
	require.NoError(t, err)

	// Count chunks received
	for range streamChan {
		chunkCount++
	}

	// Verify we got multiple chunks (3 emitted + 1 final chunk)
	assert.GreaterOrEqual(t, chunkCount, 4)
}

// TestExecuteStreamWithMessage_CompareWithExecuteStream verifies compatibility
func TestExecuteStreamWithMessage_CompareWithExecuteStream(t *testing.T) {
	role := "user"
	content := "Test streaming"

	emitChunk := func(execCtx *ExecutionContext, next func() error) error {
		execCtx.EmitStreamChunk(providers.StreamChunk{Content: "test"})
		return next()
	}

	// Execute with original method
	p1 := NewPipeline(&funcMiddleware{fn: emitChunk})
	stream1, err1 := p1.ExecuteStream(context.Background(), role, content)
	require.NoError(t, err1)

	var result1 *ExecutionResult
	for chunk := range stream1 {
		if chunk.FinalResult != nil {
			if r, ok := chunk.FinalResult.(*ExecutionResult); ok {
				result1 = r
			}
		}
	}

	// Execute with new method
	message := types.Message{Role: role, Content: content}
	p2 := NewPipeline(&funcMiddleware{fn: emitChunk})
	stream2, err2 := p2.ExecuteStreamWithMessage(context.Background(), message)
	require.NoError(t, err2)

	var result2 *ExecutionResult
	for chunk := range stream2 {
		if chunk.FinalResult != nil {
			if r, ok := chunk.FinalResult.(*ExecutionResult); ok {
				result2 = r
			}
		}
	}

	// Results should be equivalent
	require.NotNil(t, result1)
	require.NotNil(t, result2)
	assert.Equal(t, len(result1.Messages), len(result2.Messages))
	assert.Equal(t, result1.Messages[0].Role, result2.Messages[0].Role)
	assert.Equal(t, result1.Messages[0].Content, result2.Messages[0].Content)
}

// TestExecuteStreamWithMessage_ContextCancellation verifies context cancellation in streaming
func TestExecuteStreamWithMessage_ContextCancellation(t *testing.T) {
	message := types.Message{Role: "user", Content: "test"}

	ctx, cancel := context.WithCancel(context.Background())

	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			// Cancel context during execution
			cancel()
			// Note: EmitStreamChunk may or may not succeed depending on timing
			// (goroutine may not have processed cancellation yet)
			// Just verify we don't panic and execution completes
			execCtx.EmitStreamChunk(providers.StreamChunk{Content: "test"})
			return next()
		},
	}

	p := NewPipeline(middleware)

	streamChan, err := p.ExecuteStreamWithMessage(ctx, message)
	require.NoError(t, err)

	// Channel should close (possibly with no chunks due to cancellation)
	for range streamChan {
	}

	// Main verification: execution should complete without panic
}
