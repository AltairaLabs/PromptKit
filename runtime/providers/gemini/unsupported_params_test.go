package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A model configured with unsupported_params: [top_k] is not sent it; one
// without is.
func TestApplyOptionalSampling_HonorsUnsupportedParams(t *testing.T) {
	topK := 40
	req := &providers.PredictionRequest{TopK: &topK}

	open := &Provider{}
	var cfg geminiGenConfig
	cfg.applyOptionalSampling(open, req)
	assert.Equal(t, &topK, cfg.TopK)

	closed := &Provider{}
	closed.SetUnsupportedParams([]string{"top_k"})
	cfg = geminiGenConfig{}
	cfg.applyOptionalSampling(closed, req)
	assert.Nil(t, cfg.TopK)
}
