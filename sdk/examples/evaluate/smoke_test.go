package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

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
	_ = results
}
