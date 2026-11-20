package sdk

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// Token audiences, as the managed identity endpoint receives them (the
// resource parameter is the scope without its /.default suffix).
const (
	foundryProjectAudience    = "https://ai.azure.com"
	cognitiveServicesAudience = "https://cognitiveservices.azure.com"
)

// foundryProject is the path of a Foundry project under its account host.
const foundryProject = "/api/projects/my-proj"

// convFoundryIdentity stands in for the managed identity and gives this run a
// client ID of its own, returned and set as AZURE_CLIENT_ID, so the identity
// library's process-wide token cache cannot answer for the stand-in
// (-count=N).
func convFoundryIdentity(t *testing.T) (*convCredServer, string) {
	t.Helper()
	msi := convAzureManagedIdentity(t)
	clientID := fmt.Sprintf("foundry-id-%d", azureMIRuns.Add(1))
	t.Setenv("AZURE_CLIENT_ID", clientID)
	return msi, clientID
}

// convTokenAudience is the audience of the one token the provider asked for.
func convTokenAudience(t *testing.T, msi *convCredServer) string {
	t.Helper()
	reqs := msi.requests()
	require.Len(t, reqs, 1, "one token request")
	return reqs[0].Query.Get("resource")
}

// convFoundrySpec is a keyless OpenAI spec on the Azure endpoint, whose path
// decides whether it is a Foundry project or an account.
func convFoundrySpec(endpoint, id, deployment string) ProviderSpec {
	return ProviderSpec{
		ID: id, Type: "openai", Model: deployment,
		Platform: &pkgconfig.PlatformConfig{Type: "azure", Endpoint: endpoint},
	}
}

// A Foundry project endpoint accepts only tokens for https://ai.azure.com and
// serves the OpenAI v1 route under the project, with the deployment in the
// body and no api-version (#2818). Before, the token was for Cognitive
// Services and the project answered 401 "audience is incorrect".
func TestConv_AzureFoundryProjectChat(t *testing.T) {
	cases := map[string]func(endpoint, clientID string) Option{
		"WithLLMProvider": func(endpoint, _ string) Option {
			return WithLLMProvider(convFoundrySpec(endpoint, "agent", "my-deployment"))
		},
		"WithAzure": func(endpoint, clientID string) Option {
			return WithAzure(endpoint, "openai", "my-deployment", WithAzureManagedIdentity(clientID))
		},
	}
	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			msi, clientID := convFoundryIdentity(t)
			w := newConvWire(t)
			r := convSendVia(t, w, opt(w.URL+foundryProject, clientID))

			assert.Equal(t, foundryProject+"/openai/v1/chat/completions", r.Path)
			assert.Equal(t, "", r.Query.Encode(), "the v1 route takes no api-version")
			assert.Equal(t, "my-deployment", r.Body["model"])
			assert.Equal(t, "Bearer MITOKEN", r.Header.Get("Authorization"))
			assert.Equal(t, foundryProjectAudience, convTokenAudience(t, msi))
		})
	}
}

// An account endpoint keeps the Cognitive Services audience and the
// deployment route, whichever account host it is on.
func TestConv_AzureAccountEndpointKeepsCognitiveServicesScope(t *testing.T) {
	msi, _ := convFoundryIdentity(t)
	w := newConvWire(t)
	r := convSendVia(t, w, WithLLMProvider(convFoundrySpec(w.URL, "agent", "my-deployment")))

	assert.Equal(t, "/openai/deployments/my-deployment/chat/completions", r.Path)
	assert.NotEmpty(t, r.Query.Get("api-version"))
	assert.Equal(t, cognitiveServicesAudience, convTokenAudience(t, msi))
}

// Embeddings, TTS and STT on a Foundry project take the same audience and
// route as chat.
func TestConv_AzureFoundryProjectServices(t *testing.T) {
	ctx := context.Background()

	t.Run("embeddings", func(t *testing.T) {
		msi, _ := convFoundryIdentity(t)
		w := newConvWire(t)
		c := &config{}
		require.NoError(t, WithEmbeddingProvider(convFoundrySpec(w.URL+foundryProject, "emb", "my-embed"))(c))
		resp, err := c.embeddingProviders["emb"].Embed(ctx, providers.EmbeddingRequest{Texts: []string{"a"}})
		require.NoError(t, err)
		assert.Equal(t, [][]float32{{0.25, 0.5}}, resp.Embeddings)

		reqs := w.requests("/embeddings")
		require.Len(t, reqs, 1)
		assert.Equal(t, foundryProject+"/openai/v1/embeddings", reqs[0].Path)
		assert.Equal(t, "", reqs[0].Query.Encode())
		assert.Equal(t, "my-embed", reqs[0].Body["model"])
		assert.Equal(t, "Bearer MITOKEN", reqs[0].Header.Get("Authorization"))
		assert.Equal(t, foundryProjectAudience, convTokenAudience(t, msi))
	})

	t.Run("tts", func(t *testing.T) {
		msi, _ := convFoundryIdentity(t)
		w := newConvWire(t)
		c := &config{}
		require.NoError(t, WithTTSProvider(convFoundrySpec(w.URL+foundryProject, "voice", "my-tts"))(c))
		rc, err := c.ttsService.Synthesize(ctx, "say this", tts.SynthesisConfig{Voice: "alloy"})
		require.NoError(t, err)
		audio, err := io.ReadAll(rc)
		require.NoError(t, err)
		_ = rc.Close()
		assert.Equal(t, "MP3DATA", string(audio))

		reqs := w.requests("/audio/speech")
		require.Len(t, reqs, 1)
		assert.Equal(t, foundryProject+"/openai/v1/audio/speech", reqs[0].Path)
		assert.Equal(t, "", reqs[0].Query.Encode())
		assert.Equal(t, "my-tts", reqs[0].Body["model"])
		assert.Equal(t, "Bearer MITOKEN", reqs[0].Header.Get("Authorization"))
		assert.Equal(t, foundryProjectAudience, convTokenAudience(t, msi))
	})

	t.Run("stt", func(t *testing.T) {
		msi, _ := convFoundryIdentity(t)
		w := newConvWire(t)
		c := &config{}
		require.NoError(t, WithSTTProvider(convFoundrySpec(w.URL+foundryProject, "ears", "my-transcribe"))(c))
		text, err := c.sttService.TranscribeBytes(ctx, []byte("RIFFfake"), stt.TranscriptionConfig{Format: "wav"})
		require.NoError(t, err)
		assert.Equal(t, "transcribed words", text)

		reqs := w.requests("/audio/transcriptions")
		require.Len(t, reqs, 1)
		assert.Equal(t, foundryProject+"/openai/v1/audio/transcriptions", reqs[0].Path)
		assert.Equal(t, "", reqs[0].Query.Encode())
		assert.Equal(t, "Bearer MITOKEN", reqs[0].Header.Get("Authorization"))
		assert.Equal(t, foundryProjectAudience, convTokenAudience(t, msi))
	})
}
