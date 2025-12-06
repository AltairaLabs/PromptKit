package middleware

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurnIndexMiddleware_ComputesCounters(t *testing.T) {
	mw := TurnIndexMiddleware()

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "system", Content: "You are helpful"},
			{Role: "user", Content: "Hi"},                  // user #1
			{Role: "assistant", Content: "Hello!"},         // assistant #1
			{Role: "user", Content: "How are you?"},        // user #2
			{Role: "assistant", Content: "Great, thanks!"}, // assistant #2
		},
	}

	called := false
	err := mw.Process(ctx, func() error { called = true; return nil })
	require.NoError(t, err)
	require.True(t, called)
	require.NotNil(t, ctx.Metadata)

	assert.Equal(t, 2, ctx.Metadata["arena_user_completed_turns"])      // two user msgs
	assert.Equal(t, 3, ctx.Metadata["arena_user_next_turn"])            // next user turn
	assert.Equal(t, 2, ctx.Metadata["arena_assistant_completed_turns"]) // two assistant msgs
	assert.Equal(t, 3, ctx.Metadata["arena_assistant_next_turn"])       // next assistant turn
}

func TestTurnIndexMiddleware_IdempotentWhenPresent(t *testing.T) {
	mw := TurnIndexMiddleware()

	// Pre-populate with non-default values to verify they remain unchanged
	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Hi"},
		},
		Metadata: map[string]interface{}{
			"arena_user_completed_turns":      10,
			"arena_user_next_turn":            11,
			"arena_assistant_completed_turns": 20,
			"arena_assistant_next_turn":       21,
		},
	}

	err := mw.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Should not overwrite existing authoritative values
	assert.Equal(t, 10, ctx.Metadata["arena_user_completed_turns"])
	assert.Equal(t, 11, ctx.Metadata["arena_user_next_turn"])
	assert.Equal(t, 20, ctx.Metadata["arena_assistant_completed_turns"])
	assert.Equal(t, 21, ctx.Metadata["arena_assistant_next_turn"])
}
