package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
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
