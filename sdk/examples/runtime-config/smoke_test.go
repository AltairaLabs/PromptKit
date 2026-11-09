package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
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

// TestSmoke pins that the required company_name variable, set the way main.go
// sets it, is rendered into the support agent's system prompt.
func TestSmoke(t *testing.T) {
	provider := &systemRecorder{Provider: mock.NewProvider("mock", "mock-model", false)}
	conv, err := sdk.Open("agent.pack.json", "support",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer conv.Close()
	// company_name is a required variable; main.go sets it the same way.
	conv.SetVar("company_name", "Acme Corp")

	_, err = conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	require.Contains(t, provider.system, "You are a professional customer support agent for Acme Corp.")
}
