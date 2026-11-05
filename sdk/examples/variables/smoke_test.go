package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/variables"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
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
		// The pack's template uses {{current_month}} and {{current_year}},
		// which only the TimeProvider supplies — as main_interactive.go wires it.
		sdk.WithVariableProvider(variables.NewTimeProvider()),
		sdk.WithVariableProvider(&testVarProvider{}),
	)
	require.NoError(t, err)
	defer conv.Close()

	_, err = conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
}
