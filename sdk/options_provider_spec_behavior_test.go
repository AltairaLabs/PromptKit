package sdk

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// convSpecFor is an OpenAI ProviderSpec served by w, with a key and a header
// the request must carry.
func convSpecFor(w *convWire, id, model string) ProviderSpec {
	return ProviderSpec{
		ID: id, Type: "openai", Model: model, BaseURL: w.URL,
		Credential: &pkgconfig.CredentialConfig{APIKey: "spec-key"},
		Headers:    map[string]string{"X-Conv-Tenant": "acme"},
	}
}

// convAssertSpecOnWire checks a request carries the spec's key and header.
func convAssertSpecOnWire(t *testing.T, r convWireRequest) {
	t.Helper()
	assert.Equal(t, "Bearer spec-key", r.Header.Get("Authorization"), "the spec's credential authenticates")
	assert.Equal(t, "acme", r.Header.Get("X-Conv-Tenant"), "the spec's headers are sent")
}

// Each With*Provider builds its capability from the ProviderSpec, and what the
// capability sends is the spec: its base URL, model, credential and headers.
func TestConv_ProviderSpecReachesTheWire(t *testing.T) {
	ctx := context.Background()

	t.Run("WithLLMProvider", func(t *testing.T) {
		w := newConvWire(t, convWireReply{Text: "from spec"})
		conv, err := Open(createTestPackFile(t, convChatPack), "chat", WithSkipSchemaValidation(),
			WithLLMProvider(convSpecFor(w, "agent", "gpt-4o-mini")))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })
		resp, err := conv.Send(ctx, "hi")
		require.NoError(t, err)
		assert.Equal(t, "from spec", resp.Text())
		convAssertSpecOnWire(t, w.lastChat(t))
		assert.Equal(t, "gpt-4o-mini", w.lastChat(t).Body["model"])
	})

	t.Run("WithEmbeddingProvider", func(t *testing.T) {
		w := newConvWire(t)
		c := &config{}
		spec := convSpecFor(w, "emb", "text-embedding-3-small")
		spec.AdditionalConfig = map[string]any{"dimensions": 2}
		require.NoError(t, WithEmbeddingProvider(spec)(c))
		resp, err := c.retrievalProvider.Embed(ctx, providers.EmbeddingRequest{Texts: []string{"a"}})
		require.NoError(t, err)
		require.Len(t, resp.Embeddings, 1)
		reqs := w.requests("/embeddings")
		require.Len(t, reqs, 1)
		convAssertSpecOnWire(t, reqs[0])
		assert.Equal(t, "text-embedding-3-small", reqs[0].Body["model"])
	})

	t.Run("WithTTSProvider", func(t *testing.T) {
		w := newConvWire(t)
		c := &config{}
		require.NoError(t, WithTTSProvider(convSpecFor(w, "voice", "tts-1"))(c))
		rc, err := c.ttsService.Synthesize(ctx, "say this", tts.SynthesisConfig{Voice: "alloy"})
		require.NoError(t, err)
		audio, err := io.ReadAll(rc)
		require.NoError(t, err)
		_ = rc.Close()
		assert.Equal(t, "MP3DATA", string(audio))
		reqs := w.requests("/audio/speech")
		require.Len(t, reqs, 1)
		convAssertSpecOnWire(t, reqs[0])
		assert.Equal(t, "tts-1", reqs[0].Body["model"])
		assert.Equal(t, "say this", reqs[0].Body["input"])
	})

	t.Run("WithSTTProvider", func(t *testing.T) {
		w := newConvWire(t)
		c := &config{}
		require.NoError(t, WithSTTProvider(convSpecFor(w, "ears", "whisper-1"))(c))
		text, err := c.sttService.TranscribeBytes(ctx, []byte("RIFFfake"), stt.TranscriptionConfig{Format: "wav"})
		require.NoError(t, err)
		assert.Equal(t, "transcribed words", text)
		reqs := w.requests("/audio/transcriptions")
		require.Len(t, reqs, 1)
		convAssertSpecOnWire(t, reqs[0])
		assert.Contains(t, string(reqs[0].Raw), "whisper-1", "the spec's model is in the upload")
	})

	t.Run("WithInferenceProvider", func(t *testing.T) {
		w := newConvWire(t, convWireReply{Text: "positive"})
		c := &config{}
		require.NoError(t, WithInferenceProvider(convSpecFor(w, "judge", "gpt-4o-mini"))(c))
		p, err := c.inferenceRegistry.Get("judge")
		require.NoError(t, err)
		_, err = p.Infer(ctx, inference.Request{
			Inputs: []types.Message{{Role: "user", Content: "great"}},
			Labels: []string{"positive", "negative"},
		})
		// The scripted endpoint returns no logprobs; the request is under test.
		require.ErrorContains(t, err, "logprobs")
		r := w.last(t)
		convAssertSpecOnWire(t, r)
		assert.Equal(t, "gpt-4o-mini", r.Body["model"])
	})

	t.Run("WithImageProvider", func(t *testing.T) {
		w := newConvWire(t)
		spec := convSpecFor(w, "img", "imagen-4.0-generate-001")
		spec.Type = "imagen"
		conv, err := Open(createTestPackFile(t, convChatPack), "chat", WithSkipSchemaValidation(),
			WithImageProvider(spec))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })
		_, _ = conv.Send(ctx, "a red circle") // the scripted endpoint has no predict route
		r := w.last(t)
		assert.Equal(t, "/models/imagen-4.0-generate-001:predict", r.Path)
		assert.Equal(t, "spec-key", r.Header.Get("X-Goog-Api-Key"))
		assert.Equal(t, "acme", r.Header.Get("X-Conv-Tenant"))
		assert.Contains(t, string(r.Raw), `"prompt":"a red circle"`)
	})
}
