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
	conv, err := sdk.Open("client-tools.pack.json", "assistant",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer conv.Close()

	conv.OnClientTool("get_location", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		return map[string]string{"lat": "37.7749", "lng": "-122.4194"}, nil
	})

	_, err = conv.Send(context.Background(), "Where am I?")
	require.NoError(t, err)
}
