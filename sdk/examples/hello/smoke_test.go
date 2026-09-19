package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func TestSmoke(t *testing.T) {
	provider := mock.NewProvider("mock", "mock-model", false)
	conv, err := sdk.Open("hello.pack.json", "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer conv.Close()

	conv.SetVar("user_name", "Test")
	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())
}
