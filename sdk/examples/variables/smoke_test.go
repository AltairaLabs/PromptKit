package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/variables"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// systemRecorder is the mock LLM, recording the system prompt it was sent.
type systemRecorder struct {
	providers.Provider
	system string
}

func (r *systemRecorder) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	r.system = req.System
	return r.Provider.Predict(ctx, req)
}

func (r *systemRecorder) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	r.system = req.System
	return r.Provider.PredictStream(ctx, req)
}

// TestSmoke wires the providers the way main_interactive.go does and pins
// what they put in the system prompt. A pack variable default would outrank a
// provider (session variables rank above providers), so the pack declares
// none for the names these providers supply.
func TestSmoke(t *testing.T) {
	provider := &systemRecorder{Provider: mock.NewProvider("mock", "mock-model", false)}
	conv, err := sdk.Open("assistant.pack.json", "assistant",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithVariableProvider(variables.NewTimeProvider()),
		sdk.WithVariableProvider(NewUserContextProvider("user-12345")),
	)
	require.NoError(t, err)
	defer conv.Close()

	_, err = conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	require.Contains(t, provider.system, "- User ID: user-12345\n")
	require.Contains(t, provider.system, "- Preferred response style: friendly and conversational\n")
	require.Contains(t, provider.system, "- Expertise level: beginner\n")
	require.NotContains(t, provider.system, "unknown", "the TimeProvider supplies every time variable")
	require.NotContains(t, provider.system, "{{", "every placeholder is rendered")
}
