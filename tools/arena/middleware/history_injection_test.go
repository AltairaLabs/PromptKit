package middleware

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryInjectionMiddleware_EmptyHistory(t *testing.T) {
	middleware := HistoryInjectionMiddleware([]types.Message{})

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "new message"},
		},
	}

	called := false
	next := func() error {
		called = true
		return nil
	}

	err := middleware.Process(ctx, next)
	require.NoError(t, err)
	assert.True(t, called)

	// Should have only the original message
	assert.Len(t, ctx.Messages, 1)
	assert.Equal(t, "new message", ctx.Messages[0].Content)
}

func TestHistoryInjectionMiddleware_WithHistory(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "first message"},
		{Role: "assistant", Content: "first response"},
		{Role: "user", Content: "second message"},
	}

	middleware := HistoryInjectionMiddleware(history)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "new response"},
		},
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Should have history prepended to existing messages
	assert.Len(t, ctx.Messages, 4)
	assert.Equal(t, "first message", ctx.Messages[0].Content)
	assert.Equal(t, "first response", ctx.Messages[1].Content)
	assert.Equal(t, "second message", ctx.Messages[2].Content)
	assert.Equal(t, "new response", ctx.Messages[3].Content)
}

func TestHistoryInjectionMiddleware_NoExistingMessages(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "history 1"},
		{Role: "assistant", Content: "history 2"},
	}

	middleware := HistoryInjectionMiddleware(history)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{},
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Should have only history messages
	assert.Len(t, ctx.Messages, 2)
	assert.Equal(t, "history 1", ctx.Messages[0].Content)
	assert.Equal(t, "history 2", ctx.Messages[1].Content)
}

func TestHistoryInjectionMiddleware_PreservesMessageOrder(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "A"},
		{Role: "assistant", Content: "B"},
	}

	middleware := HistoryInjectionMiddleware(history)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "C"},
			{Role: "assistant", Content: "D"},
		},
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Check order: history first, then existing
	assert.Len(t, ctx.Messages, 4)
	assert.Equal(t, "A", ctx.Messages[0].Content)
	assert.Equal(t, "B", ctx.Messages[1].Content)
	assert.Equal(t, "C", ctx.Messages[2].Content)
	assert.Equal(t, "D", ctx.Messages[3].Content)
}

func TestHistoryInjectionMiddleware_StreamChunk(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "test"},
	}

	middleware := HistoryInjectionMiddleware(history)

	ctx := &pipeline.ExecutionContext{}
	chunk := &providers.StreamChunk{}

	// StreamChunk should be a no-op and not return an error
	err := middleware.StreamChunk(ctx, chunk)
	assert.NoError(t, err)
}

func TestHistoryInjectionMiddleware_NilHistory(t *testing.T) {
	middleware := HistoryInjectionMiddleware(nil)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "only message"},
		},
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Should preserve existing messages unchanged
	assert.Len(t, ctx.Messages, 1)
	assert.Equal(t, "only message", ctx.Messages[0].Content)
}
