package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// zeroTempJudgeProvider records the request the judge sends.
type zeroTempJudgeProvider struct {
	providers.Provider
	got providers.PredictionRequest
}

func (p *zeroTempJudgeProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.got = req
	return providers.PredictionResponse{Content: `{"passed": true, "score": 1, "reasoning": "ok"}`}, nil
}

// The judge grades deterministically: it asks for temperature 0 explicitly,
// so a provider with a non-zero default does not replace it (#2212).
func TestProviderJudge_AsksForAnExplicitZeroTemperature(t *testing.T) {
	p := &zeroTempJudgeProvider{Provider: mock.NewProvider("judge", "m", false)}
	_, err := NewProviderJudge(p).Judge(context.Background(), JudgeOpts{Content: "x", Criteria: "y"})
	require.NoError(t, err)
	assert.True(t, p.got.TemperatureSet)
	assert.Zero(t, p.got.Temperature)
	assert.InDelta(t, 0, providers.ResolveTemperature(&p.got, 0.7), 1e-9, "resolves to 0 over a 0.7 default")
}
