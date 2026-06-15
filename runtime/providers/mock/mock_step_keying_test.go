package mock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestFileMockRepository_StepIDKeying verifies the per-step response lookup in
// FileMockRepository. The priority order is:
//   - selfplay (unchanged — not tested here)
//   - steps[stepID]  ← new, after selfplay, before turn-number lookup
//   - turns[turnNumber]
//   - scenario default
//   - global default
//   - fallback
func TestFileMockRepository_StepIDKeying(t *testing.T) {
	configData := `
defaultResponse: "global default"
scenarios:
  my-scenario:
    defaultResponse: "scenario default"
    steps:
      classify: "classify response"
      synthesize:
        type: text
        content: "synthesize structured response"
    turns:
      1: "turn 1 response"
`

	tempFile := createTempYAMLFile(t, configData)
	defer cleanupTempFile(t, tempFile)

	repo, err := NewFileMockRepository(tempFile)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("step id classify returns step response", func(t *testing.T) {
		turn, err := repo.GetTurn(ctx, ResponseParams{
			ScenarioID: "my-scenario",
			StepID:     "classify",
		})
		require.NoError(t, err)
		assert.Equal(t, "classify response", turn.Content)
		assert.Equal(t, turnTypeText, turn.Type)
	})

	t.Run("step id synthesize returns structured step response", func(t *testing.T) {
		turn, err := repo.GetTurn(ctx, ResponseParams{
			ScenarioID: "my-scenario",
			StepID:     "synthesize",
		})
		require.NoError(t, err)
		assert.Equal(t, "synthesize structured response", turn.Content)
		assert.Equal(t, turnTypeText, turn.Type)
	})

	t.Run("unknown step id falls back to turn lookup", func(t *testing.T) {
		turn, err := repo.GetTurn(ctx, ResponseParams{
			ScenarioID: "my-scenario",
			StepID:     "unknown-step",
			TurnNumber: 1,
		})
		require.NoError(t, err)
		assert.Equal(t, "turn 1 response", turn.Content)
	})

	t.Run("empty step id falls back to scenario default", func(t *testing.T) {
		turn, err := repo.GetTurn(ctx, ResponseParams{
			ScenarioID: "my-scenario",
			StepID:     "",
			TurnNumber: 99, // non-existent turn
		})
		require.NoError(t, err)
		assert.Equal(t, "scenario default", turn.Content)
	})

	t.Run("step id wins over turn number when both set", func(t *testing.T) {
		turn, err := repo.GetTurn(ctx, ResponseParams{
			ScenarioID: "my-scenario",
			StepID:     "classify",
			TurnNumber: 1, // turn 1 also exists but step should win
		})
		require.NoError(t, err)
		assert.Equal(t, "classify response", turn.Content)
	})
}

// TestMockProvider_StepIDFromMetadata verifies that the Provider reads
// "composition_step_id" from req.Metadata and populates ResponseParams.StepID.
func TestMockProvider_StepIDFromMetadata(t *testing.T) {
	configData := `
defaultResponse: "global default"
scenarios:
  my-scenario:
    steps:
      classify: "classify mock response"
`

	tempFile := createTempYAMLFile(t, configData)
	defer cleanupTempFile(t, tempFile)

	repo, err := NewFileMockRepository(tempFile)
	require.NoError(t, err)

	provider := NewProviderWithRepository("test-provider", "test-model", false, repo)

	ctx := context.Background()
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "classify this"},
		},
		Metadata: map[string]interface{}{
			"mock_scenario_id":    "my-scenario",
			"composition_step_id": "classify",
		},
	}

	resp, err := provider.Predict(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "classify mock response", resp.Content)
}
