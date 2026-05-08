package tts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// ---------------------------------------------------------------------------
// Compile-time interface satisfaction checks are in openai.go.
// These unit tests exercise SynthesizeTTS + stream cost on each impl.
// ---------------------------------------------------------------------------

func TestOpenAIService_BaseTTSProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write([]byte("pcm-audio-bytes"))
	}))
	defer server.Close()

	svc := NewOpenAI("test-key", WithOpenAIBaseURL(server.URL))

	// Verify Provider identity methods.
	assert.Equal(t, "openai", svc.Name())
	assert.Equal(t, base.ProviderTypeTTS, svc.Type())
	assert.NotNil(t, svc.Pricing())
	assert.NoError(t, svc.Validate())
	assert.NoError(t, svc.Init(context.Background()))
	assert.NoError(t, svc.HealthCheck(context.Background()))
	assert.NoError(t, svc.Close())

	// SynthesizeTTS should return a stream with audio and non-nil cost.
	stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{
		Text:  "hello world",
		Voice: "alloy",
	})
	require.NoError(t, err)

	audio, cost, err := base.ReadAllAudio(stream)
	require.NoError(t, err)
	assert.Equal(t, []byte("pcm-audio-bytes"), audio)
	require.NotNil(t, cost)
	assert.Greater(t, cost.TotalCost, 0.0, "cost should be positive for non-empty text")
	assert.Equal(t, "character", func() string {
		for k := range cost.Quantities {
			return k
		}
		return ""
	}())
}

func TestElevenLabsService_BaseTTSProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3-audio-bytes"))
	}))
	defer server.Close()

	svc := NewElevenLabs("test-key", WithElevenLabsBaseURL(server.URL))

	assert.Equal(t, "elevenlabs", svc.Name())
	assert.Equal(t, base.ProviderTypeTTS, svc.Type())
	assert.NotNil(t, svc.Pricing())
	assert.NoError(t, svc.Validate())
	assert.NoError(t, svc.Init(context.Background()))
	assert.NoError(t, svc.HealthCheck(context.Background()))
	assert.NoError(t, svc.Close())

	stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{
		Text:  "hello",
		Voice: elevenLabsDefaultVoice,
	})
	require.NoError(t, err)

	audio, cost, err := base.ReadAllAudio(stream)
	require.NoError(t, err)
	assert.Equal(t, []byte("mp3-audio-bytes"), audio)
	require.NotNil(t, cost)
	assert.Greater(t, cost.TotalCost, 0.0)
}

func TestCartesiaService_BaseTTSProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write([]byte("cartesia-pcm"))
	}))
	defer server.Close()

	svc := NewCartesia("test-key", WithCartesiaBaseURL(server.URL))

	assert.Equal(t, "cartesia", svc.Name())
	assert.Equal(t, base.ProviderTypeTTS, svc.Type())
	assert.NotNil(t, svc.Pricing())
	assert.NoError(t, svc.Validate())
	assert.NoError(t, svc.Init(context.Background()))
	assert.NoError(t, svc.HealthCheck(context.Background()))
	assert.NoError(t, svc.Close())

	stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{
		Text:  "hi there",
		Voice: cartesiaDefaultVoice,
	})
	require.NoError(t, err)

	audio, cost, err := base.ReadAllAudio(stream)
	require.NoError(t, err)
	assert.Equal(t, []byte("cartesia-pcm"), audio)
	require.NotNil(t, cost)
	assert.Greater(t, cost.TotalCost, 0.0)
}

func TestTTSStream_Close_DrainsSafely(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write([]byte("some audio"))
	}))
	defer server.Close()

	svc := NewOpenAI("test-key", WithOpenAIBaseURL(server.URL))
	stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{Text: "test"})
	require.NoError(t, err)

	// Close without draining should not block.
	assert.NoError(t, stream.Close())
	// Second close should be a no-op.
	assert.NoError(t, stream.Close())
}
