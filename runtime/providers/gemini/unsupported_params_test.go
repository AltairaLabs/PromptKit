package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A model configured with unsupported_params for the penalties and top_k is
// not sent them; one without is.
func TestApplyOptionalSampling_HonorsUnsupportedParams(t *testing.T) {
	freq, pres, topK := float32(0.3), float32(0.4), 40
	req := &providers.PredictionRequest{FrequencyPenalty: &freq, PresencePenalty: &pres, TopK: &topK}

	open := &Provider{}
	var cfg geminiGenConfig
	cfg.applyOptionalSampling(open, req)
	assert.Equal(t, &freq, cfg.FrequencyPenalty)
	assert.Equal(t, &pres, cfg.PresencePenalty)
	assert.Equal(t, &topK, cfg.TopK)

	closed := &Provider{}
	closed.setUnsupportedParams([]string{"presence_penalty", "frequency_penalty", "top_k"})
	cfg = geminiGenConfig{}
	cfg.applyOptionalSampling(closed, req)
	assert.Nil(t, cfg.FrequencyPenalty)
	assert.Nil(t, cfg.PresencePenalty)
	assert.Nil(t, cfg.TopK)
}
