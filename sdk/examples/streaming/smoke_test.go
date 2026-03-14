package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func TestSmoke(t *testing.T) {
	provider := mock.NewProvider("mock", "mock-model", false)
	conv, err := sdk.Open("streaming.pack.json", "storyteller",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer conv.Close()

	ch := conv.Stream(context.Background(), "Tell a story")
	var chunks int
	for chunk := range ch {
		if chunk.Type == sdk.ChunkText {
			chunks++
		}
		if chunk.Type == sdk.ChunkDone {
			break
		}
	}
	assert.Greater(t, chunks, 0, "expected at least one text chunk")
}
