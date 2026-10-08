package providers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A provider warns once per parameter it does not send, however many requests
// carry it, and says nothing about a parameter the request leaves unset.
func TestWarnUnsentParams_OncePerProviderAndParam(t *testing.T) {
	buf := captureExtraBodyLogs(t)
	topK, pen := 40, float32(0.1)
	all := []string{ParamTopK, ParamFrequencyPenalty, ParamPresencePenalty}

	WarnUnsentParams("unsent-silent", &PredictionRequest{}, all...)
	assert.Empty(t, buf.String(), "a request that sets none drops nothing")

	for range 3 {
		WarnUnsentParams("unsent-once", &PredictionRequest{TopK: &topK, PresencePenalty: &pen}, all...)
	}
	WarnUnsentParams("unsent-other", &PredictionRequest{FrequencyPenalty: &pen}, all...)

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "provider=unsent-once param=top_k"), out)
	assert.Equal(t, 1, strings.Count(out, "provider=unsent-once param=presence_penalty"), out)
	assert.NotContains(t, out, "provider=unsent-once param=frequency_penalty", "unset on that request")
	assert.Equal(t, 1, strings.Count(out, "provider=unsent-other param=frequency_penalty"), out)
	assert.Equal(t, 3, strings.Count(out, "param="), out)
}

// A realtime session's sampling reports its unsent parameters the same way a
// request does, through Request; nil reports none.
func TestStreamingSampling_RequestReportsWhatItSets(t *testing.T) {
	topK := 5
	s := &StreamingSampling{MaxTokens: 9, Temperature: 0, TemperatureSet: true, TopP: 0.5, TopK: &topK}
	req := s.Request()
	assert.Equal(t, 9, req.MaxTokens)
	assert.True(t, req.setsParam(ParamTemperature), "an explicit 0 is set")
	assert.True(t, req.setsParam(ParamTopP))
	assert.True(t, req.setsParam(ParamTopK))
	assert.False(t, req.setsParam(ParamPresencePenalty))

	var none *StreamingSampling
	assert.False(t, none.Request().setsParam(ParamTemperature))
}
