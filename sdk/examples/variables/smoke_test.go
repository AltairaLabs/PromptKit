package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/variables"
	"github.com/AltairaLabs/PromptKit/sdk"
)

type testVarProvider struct{}

func (p *testVarProvider) Name() string { return "test" }
func (p *testVarProvider) Provide(_ context.Context) (map[string]string, error) {
	return map[string]string{"user_id": "test-user"}, nil
}

// Compile-time check that testVarProvider implements variables.Provider.
var _ variables.Provider = (*testVarProvider)(nil)

func TestSmoke(t *testing.T) {
	provider := mock.NewProvider("mock", "mock-model", false)
	conv, err := sdk.Open("assistant.pack.json", "assistant",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithVariableProvider(&testVarProvider{}),
	)
	require.NoError(t, err)
	defer conv.Close()

	_, err = conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
}
