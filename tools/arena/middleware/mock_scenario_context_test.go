package middleware

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockScenarioContextMiddleware_WithScenarioID(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario-1",
	}

	middleware := MockScenarioContextMiddleware(scenario)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
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

	// Check that metadata was added
	require.NotNil(t, ctx.Metadata)
	assert.Equal(t, "test-scenario-1", ctx.Metadata["mock_scenario_id"])
	assert.Equal(t, 1, ctx.Metadata["mock_turn_number"])
}

func TestMockScenarioContextMiddleware_TurnNumberCalculation(t *testing.T) {
	tests := []struct {
		name            string
		messages        []types.Message
		expectedTurnNum int
		description     string
	}{
		{
			name: "single user message",
			messages: []types.Message{
				{Role: "user", Content: "First turn"},
			},
			expectedTurnNum: 1,
			description:     "First user message is turn 1",
		},
		{
			name: "user and assistant messages",
			messages: []types.Message{
				{Role: "user", Content: "First turn"},
				{Role: "assistant", Content: "Response 1"},
				{Role: "user", Content: "Second turn"},
			},
			expectedTurnNum: 2,
			description:     "Two user messages means turn 2",
		},
		{
			name: "multiple turns",
			messages: []types.Message{
				{Role: "user", Content: "Turn 1"},
				{Role: "assistant", Content: "Response 1"},
				{Role: "user", Content: "Turn 2"},
				{Role: "assistant", Content: "Response 2"},
				{Role: "user", Content: "Turn 3"},
			},
			expectedTurnNum: 3,
			description:     "Three user messages means turn 3",
		},
		{
			name: "only assistant messages",
			messages: []types.Message{
				{Role: "assistant", Content: "Response"},
			},
			expectedTurnNum: 2,
			description:     "Assistant message implies we are advancing to next turn",
		},
		{
			name:            "no messages",
			messages:        []types.Message{},
			expectedTurnNum: 0,
			description:     "Empty messages means turn 0",
		},
		{
			name: "mixed roles including system",
			messages: []types.Message{
				{Role: "system", Content: "System prompt"},
				{Role: "user", Content: "Turn 1"},
				{Role: "assistant", Content: "Response 1"},
				{Role: "user", Content: "Turn 2"},
			},
			expectedTurnNum: 2,
			description:     "System messages don't count, only user messages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scenario := &config.Scenario{
				ID: "test-scenario",
			}

			middleware := MockScenarioContextMiddleware(scenario)

			ctx := &pipeline.ExecutionContext{
				Messages: tt.messages,
			}

			err := middleware.Process(ctx, func() error { return nil })
			require.NoError(t, err)

			// Check turn number
			require.NotNil(t, ctx.Metadata)
			assert.Equal(t, tt.expectedTurnNum, ctx.Metadata["mock_turn_number"],
				"Test: %s - %s", tt.name, tt.description)
		})
	}
}

func TestMockScenarioContextMiddleware_NilScenario(t *testing.T) {
	middleware := MockScenarioContextMiddleware(nil)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Should not add metadata when scenario is nil
	assert.Nil(t, ctx.Metadata)
}

func TestMockScenarioContextMiddleware_EmptyScenarioID(t *testing.T) {
	scenario := &config.Scenario{
		ID: "", // Empty ID
	}

	middleware := MockScenarioContextMiddleware(scenario)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Should not add metadata when scenario ID is empty
	assert.Nil(t, ctx.Metadata)
}

func TestMockScenarioContextMiddleware_PreservesExistingMetadata(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	middleware := MockScenarioContextMiddleware(scenario)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Metadata: map[string]interface{}{
			"existing_key": "existing_value",
			"another_key":  123,
		},
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Check that existing metadata is preserved
	require.NotNil(t, ctx.Metadata)
	assert.Equal(t, "existing_value", ctx.Metadata["existing_key"])
	assert.Equal(t, 123, ctx.Metadata["another_key"])

	// Check that new metadata was added
	assert.Equal(t, "test-scenario", ctx.Metadata["mock_scenario_id"])
	assert.Equal(t, 1, ctx.Metadata["mock_turn_number"])
}

func TestMockScenarioContextMiddleware_OverwritesExistingMockData(t *testing.T) {
	scenario := &config.Scenario{
		ID: "new-scenario",
	}

	middleware := MockScenarioContextMiddleware(scenario)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "First"},
			{Role: "assistant", Content: "Response"},
			{Role: "user", Content: "Second"},
		},
		Metadata: map[string]interface{}{
			"mock_scenario_id": "old-scenario",
			"mock_turn_number": 99,
			"other_data":       "preserved",
		},
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Check that mock metadata was overwritten
	assert.Equal(t, "new-scenario", ctx.Metadata["mock_scenario_id"])
	assert.Equal(t, 2, ctx.Metadata["mock_turn_number"])

	// Check that other metadata was preserved
	assert.Equal(t, "preserved", ctx.Metadata["other_data"])
}

func TestMockScenarioContextMiddleware_InitializesMetadataMap(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	middleware := MockScenarioContextMiddleware(scenario)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
		// Metadata is nil
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Check that metadata map was initialized
	require.NotNil(t, ctx.Metadata)
	assert.Equal(t, "test-scenario", ctx.Metadata["mock_scenario_id"])
	assert.Equal(t, 1, ctx.Metadata["mock_turn_number"])
}

func TestMockScenarioContextMiddleware_StreamChunk(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	middleware := MockScenarioContextMiddleware(scenario)

	ctx := &pipeline.ExecutionContext{}
	chunk := &providers.StreamChunk{
		Delta: "test chunk",
	}

	// StreamChunk should be a no-op and not return an error
	err := middleware.StreamChunk(ctx, chunk)
	assert.NoError(t, err)
}

func TestMockScenarioContextMiddleware_StreamChunkWithNilChunk(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	middleware := MockScenarioContextMiddleware(scenario)

	ctx := &pipeline.ExecutionContext{}

	// StreamChunk should handle nil chunk gracefully
	err := middleware.StreamChunk(ctx, nil)
	assert.NoError(t, err)
}

func TestMockScenarioContextMiddleware_CompleteScenario(t *testing.T) {
	// Test with a more complete scenario configuration
	scenario := &config.Scenario{
		ID:          "comprehensive-test",
		Description: "A comprehensive test scenario",
		TaskType:    "predict",
		Mode:        "interactive",
	}

	middleware := MockScenarioContextMiddleware(scenario)

	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "How are you?"},
		},
		Metadata: map[string]interface{}{
			"request_id": "12345",
		},
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	// Verify metadata
	require.NotNil(t, ctx.Metadata)
	assert.Equal(t, "comprehensive-test", ctx.Metadata["mock_scenario_id"])
	assert.Equal(t, 2, ctx.Metadata["mock_turn_number"]) // 2 user messages
	assert.Equal(t, "12345", ctx.Metadata["request_id"]) // Preserved
}

func TestMockScenarioContextMiddleware_PrefersAuthoritativeCounters(t *testing.T) {
	scenario := &config.Scenario{ID: "auth-counts"}
	middleware := MockScenarioContextMiddleware(scenario)

	// Messages would imply 1 completed user turn, but authoritative metadata says 5
	ctx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Metadata: map[string]interface{}{
			"arena_user_completed_turns": 5,
		},
	}

	err := middleware.Process(ctx, func() error { return nil })
	require.NoError(t, err)

	require.NotNil(t, ctx.Metadata)
	assert.Equal(t, "auth-counts", ctx.Metadata["mock_scenario_id"])
	// Should prefer authoritative completed user turns over counting messages
	assert.Equal(t, 5, ctx.Metadata["mock_turn_number"])
}
