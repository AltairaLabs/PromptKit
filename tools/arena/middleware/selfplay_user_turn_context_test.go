package middleware

import (
    "testing"

    "github.com/AltairaLabs/PromptKit/pkg/config"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/types"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSelfPlayUserTurnContextMiddleware_UsesTurnIndexCounters(t *testing.T) {
    scenario := &config.Scenario{ID: "sp-scenario"}
    mw := SelfPlayUserTurnContextMiddleware(scenario)

    // TurnIndexMiddleware would have computed these from two user messages
    ctx := &pipeline.ExecutionContext{
        Messages: []types.Message{
            {Role: "user", Content: "Turn 1"},
            {Role: "assistant", Content: "Resp 1"},
            {Role: "user", Content: "Turn 2"},
        },
        Metadata: map[string]interface{}{
            "arena_user_completed_turns":      2,
            "arena_user_next_turn":            3,
            "arena_assistant_completed_turns": 1,
            "arena_assistant_next_turn":       2,
        },
    }

    err := mw.Process(ctx, func() error { return nil })
    require.NoError(t, err)

    require.NotNil(t, ctx.Metadata)
    assert.Equal(t, 2, ctx.Metadata["arena_user_completed_turns"]) // preserved
    assert.Equal(t, 3, ctx.Metadata["arena_user_next_turn"])       // preserved
    assert.Equal(t, "self_play_user", ctx.Metadata["arena_role"])  
    assert.Equal(t, "sp-scenario", ctx.Metadata["mock_scenario_id"])
    // Back-compat for repository selection
    assert.Equal(t, 3, ctx.Metadata["mock_turn_number"]) // next user turn
}

func TestSelfPlayUserTurnContextMiddleware_FallbackFromMessages(t *testing.T) {
    scenario := &config.Scenario{ID: "sp-scenario"}
    mw := SelfPlayUserTurnContextMiddleware(scenario)

    // No precomputed counters; should compute from messages
    ctx := &pipeline.ExecutionContext{
        Messages: []types.Message{
            {Role: "user", Content: "Only one user"},
        },
    }

    err := mw.Process(ctx, func() error { return nil })
    require.NoError(t, err)

    require.NotNil(t, ctx.Metadata)
    assert.Equal(t, 1, ctx.Metadata["arena_user_completed_turns"]) 
    assert.Equal(t, 2, ctx.Metadata["arena_user_next_turn"])       
    assert.Equal(t, 2, ctx.Metadata["mock_turn_number"])           // next user turn
    assert.Equal(t, "self_play_user", ctx.Metadata["arena_role"]) 
}
