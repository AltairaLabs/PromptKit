package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A model configured with unsupported_params for the penalties is not sent
// them; one without is.
func TestApplyPenalties_HonorsUnsupportedParams(t *testing.T) {
	freq, pres := float32(0.3), float32(0.4)
	req := &providers.PredictionRequest{FrequencyPenalty: &freq, PresencePenalty: &pres}

	open := &Provider{}
	var cfg geminiGenConfig
	cfg.applyPenalties(open, req)
	assert.Equal(t, &freq, cfg.FrequencyPenalty)
	assert.Equal(t, &pres, cfg.PresencePenalty)

	closed := &Provider{}
	closed.setUnsupportedParams([]string{"presence_penalty", "frequency_penalty"})
	cfg = geminiGenConfig{}
	cfg.applyPenalties(closed, req)
	assert.Nil(t, cfg.FrequencyPenalty)
	assert.Nil(t, cfg.PresencePenalty)
}
