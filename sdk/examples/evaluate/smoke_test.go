package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// TestSmoke runs the example's every_turn evals over a greeting and pins what
// each one measured: the greeting is found, no politeness word is, and the
// on_session_complete farewell eval does not run for an every_turn trigger.
func TestSmoke(t *testing.T) {
	results, err := sdk.Evaluate(context.Background(), sdk.EvaluateOpts{
		PackPath: "evaluate.pack.json",
		Trigger:  evals.TriggerEveryTurn,
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "Hello! How can I help you today?"},
		},
		SkipSchemaValidation: true,
	})
	require.NoError(t, err)

	scores := make(map[string]float64, len(results))
	for _, r := range results {
		require.Empty(t, r.Error, "eval %s", r.EvalID)
		require.NotNil(t, r.Score, "eval %s", r.EvalID)
		scores[r.EvalID] = *r.Score
	}
	require.Equal(t, map[string]float64{"greeting_check": 1, "polite_check": 0}, scores)
}
