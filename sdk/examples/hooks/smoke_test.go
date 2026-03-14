package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func TestSmoke(t *testing.T) {
	provider := mock.NewProvider("mock", "mock-model", false)
	conv, err := sdk.Open("hooks.pack.json", "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer conv.Close()

	_, err = conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
}
