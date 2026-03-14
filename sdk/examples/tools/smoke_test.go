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
	conv, err := sdk.Open("tools.pack.json", "assistant",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer conv.Close()

	conv.OnTool("get_time", func(args map[string]any) (any, error) {
		return map[string]string{"time": "12:00"}, nil
	})
	conv.OnTool("get_weather", func(args map[string]any) (any, error) {
		return map[string]string{"weather": "sunny"}, nil
	})

	_, err = conv.Send(context.Background(), "What time is it?")
	require.NoError(t, err)
}
