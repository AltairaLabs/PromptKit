package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// sessionConfigSpy records the config each realtime session is created with.
type sessionConfigSpy struct {
	*mock.StreamingProvider
	mu   sync.Mutex
	seen []*providers.StreamingInputConfig
}

func (s *sessionConfigSpy) CreateStreamSession(
	ctx context.Context, req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	s.mu.Lock()
	s.seen = append(s.seen, req)
	s.mu.Unlock()
	return s.StreamingProvider.CreateStreamSession(ctx, req)
}

func (s *sessionConfigSpy) first() *providers.StreamingInputConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) == 0 {
		return nil
	}
	return s.seen[0]
}

const duplexSamplingPack = `{
	"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
	"id": "duplex-sampling", "name": "duplex-sampling", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {"voice": {"id": "voice", "name": "voice", "version": "1.0.0", "system_template": "voice",
		"parameters": {"max_tokens": 321, "temperature": 0.6, "top_p": 0.8, "top_k": 30}}}
}`

// A realtime (ASM) session is created with the opened prompt's sampling
// parameters, as a ProviderStage call is (#2219). Before, the session config
// carried none, so the same prompt ran at the realtime API's defaults.
func TestOpenDuplex_SessionCarriesPromptSamplingParams(t *testing.T) {
	spy := &sessionConfigSpy{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi")}
	callerConfig := &providers.StreamingInputConfig{Config: types.StreamingMediaConfig{
		Type: types.ContentTypeAudio, ChunkSize: 3200, SampleRate: 16000, Channels: 1, BitDepth: 16,
		Encoding: "pcm_linear16"}}
	conv, err := OpenDuplex(createTestPackFile(t, duplexSamplingPack), "voice",
		WithProvider(spy), WithStreamingConfig(callerConfig))
	require.NoError(t, err)
	defer conv.Close()

	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{Content: "hello"}))
	require.Eventually(t, func() bool { return spy.first() != nil }, 5*time.Second, 10*time.Millisecond)

	s := spy.first().Sampling
	require.NotNil(t, s, "the session config carries no sampling parameters")
	assert.Equal(t, 321, s.MaxTokens)
	assert.InDelta(t, 0.6, s.Temperature, 1e-6)
	assert.True(t, s.TemperatureSet)
	assert.InDelta(t, 0.8, s.TopP, 1e-6)
	require.NotNil(t, s.TopK)
	assert.Equal(t, 30, *s.TopK)

	assert.Nil(t, callerConfig.Sampling, "the caller's config is copied, not written to")
	assert.Empty(t, callerConfig.SystemInstruction, "the caller's config is copied, not written to")
}
