package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
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

	svc := NewOpenAI("test-key", base.WithBaseURL(server.URL))

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

	svc := NewElevenLabs("test-key", base.WithBaseURL(server.URL))

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

	svc := NewCartesia("test-key", base.WithBaseURL(server.URL))

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

// Close on an undrained stream consumes the rest of the body so the reader
// goroutine finishes: afterwards the chunk channel is closed and the call's
// cost is stamped. A second Close is a no-op.
func TestTTSStream_Close_DrainsSafely(t *testing.T) {
	body := bytes.Repeat([]byte{1}, 1<<20) // far more than the channel buffers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	svc := NewOpenAI("test-key", base.WithBaseURL(server.URL))
	stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{Text: "test"})
	require.NoError(t, err)

	require.NoError(t, stream.Close())
	_, open := <-stream.Chunks()
	assert.False(t, open, "Close must leave the chunk channel drained and closed")
	cost := stream.Cost()
	require.NotNil(t, cost, "the reader goroutine stamps cost before it exits")
	assert.Equal(t, float64(4), cost.Quantities["character"])
	assert.NoError(t, stream.Close())
}

// TTSRequest.SampleRate is the output rate the caller will play the audio at,
// and ReadAllAudio returns bare samples with no rate attached: a service that
// ignores it hands back audio the caller plays at the wrong pitch. Cartesia
// and ElevenLabs both take the rate on the wire, so it must reach it.
func TestSynthesizeTTS_SampleRateReachesTheWire(t *testing.T) {
	t.Run("cartesia output_format.sample_rate", func(t *testing.T) {
		var got map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.Header().Set("Content-Type", "audio/pcm")
			_, _ = w.Write([]byte("pcm"))
		}))
		defer server.Close()

		svc := NewCartesia("k", base.WithBaseURL(server.URL))
		stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{
			Text: "hi", Voice: cartesiaDefaultVoice, Format: "pcm", SampleRate: 16000,
		})
		require.NoError(t, err)
		_, _, err = base.ReadAllAudio(stream)
		require.NoError(t, err)

		of, _ := got["output_format"].(map[string]any)
		assert.Equal(t, map[string]any{"container": "raw", "encoding": "pcm_s16le", "sample_rate": float64(16000)}, of)
	})

	t.Run("elevenlabs output_format query", func(t *testing.T) {
		var gotFormat string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotFormat = r.URL.Query().Get("output_format")
			w.Header().Set("Content-Type", "audio/pcm")
			_, _ = w.Write([]byte("pcm"))
		}))
		defer server.Close()

		svc := NewElevenLabs("k", base.WithBaseURL(server.URL))
		stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{
			Text: "hi", Voice: elevenLabsDefaultVoice, Format: "pcm", SampleRate: 16000,
		})
		require.NoError(t, err)
		_, _, err = base.ReadAllAudio(stream)
		require.NoError(t, err)
		assert.Equal(t, "pcm_16000", gotFormat)
	})

	t.Run("unset keeps each service's default", func(t *testing.T) {
		var gotFormat string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotFormat = r.URL.Query().Get("output_format")
			_, _ = w.Write([]byte("pcm"))
		}))
		defer server.Close()

		svc := NewElevenLabs("k", base.WithBaseURL(server.URL))
		stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{
			Text: "hi", Voice: elevenLabsDefaultVoice, Format: "pcm",
		})
		require.NoError(t, err)
		_, _, err = base.ReadAllAudio(stream)
		require.NoError(t, err)
		assert.Equal(t, "pcm_24000", gotFormat)
	})
}
