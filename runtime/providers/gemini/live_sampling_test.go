package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A Live setup carries the prompt's sampling parameters in generationConfig,
// except the penalties, which Gemini rejects (#2219).
func TestBuildSetupMessage_CarriesSampling(t *testing.T) {
	topK, pen := 20, float32(0.1)
	msg := buildSetupMessage(&StreamSessionConfig{Model: "gemini-3.8-live", Sampling: &providers.StreamingSampling{
		MaxTokens: 400, Temperature: 0.7, TopP: 0.9, TopK: &topK, FrequencyPenalty: &pen, PresencePenalty: &pen,
	}}, []string{"AUDIO"})
	gen := msg["setup"].(map[string]interface{})["generationConfig"].(map[string]interface{})

	assert.InDelta(t, 0.7, gen["temperature"], 1e-6)
	assert.InDelta(t, 0.9, gen["topP"], 1e-6)
	assert.Equal(t, 20, gen["topK"])
	assert.Equal(t, 400, gen["maxOutputTokens"])
	assert.NotContains(t, gen, "presencePenalty")
	assert.NotContains(t, gen, "frequencyPenalty")

	bare := buildSetupMessage(&StreamSessionConfig{Model: "m"}, []string{"AUDIO"})
	bareGen := bare["setup"].(map[string]interface{})["generationConfig"].(map[string]interface{})
	assert.NotContains(t, bareGen, "temperature", "no sampling sends none")
}

// A temperature below liveMinTemperature is held back: Gemini Live opens the
// session and never answers at it. The caller's sampling is not modified.
func TestLiveSampling_HoldsBackATemperatureLiveHangsAt(t *testing.T) {
	low := &providers.StreamingSampling{Temperature: 0, TemperatureSet: true, TopP: 0.9}
	got := liveSampling("live", low)
	assert.False(t, got.TemperatureSet)
	assert.InDelta(t, 0.9, got.TopP, 1e-6, "the rest is kept")
	assert.True(t, low.TemperatureSet, "the caller's sampling is untouched")

	ok := &providers.StreamingSampling{Temperature: 0.7}
	assert.Same(t, ok, liveSampling("live", ok))
	assert.Nil(t, liveSampling("live", nil))
}
