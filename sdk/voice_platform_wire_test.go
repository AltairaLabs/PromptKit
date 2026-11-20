package sdk

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// convAzureManagedIdentity points the Azure credential chain at a stand-in
// managed identity endpoint that issues MITOKEN, and pins the chain to it so a
// developer's own az login never answers (or reaches the test server). It
// returns the stand-in, which records the token requests.
func convAzureManagedIdentity(t *testing.T) *convCredServer {
	t.Helper()
	t.Setenv("AZURE_TOKEN_CREDENTIALS", "ManagedIdentityCredential")
	for _, k := range []string{"AZURE_CLIENT_ID", "AZURE_TENANT_ID", "AZURE_CLIENT_SECRET", "MSI_ENDPOINT", "MSI_SECRET",
		"IDENTITY_SERVER_THUMBPRINT", "IMDS_ENDPOINT", "AZURE_FEDERATED_TOKEN_FILE"} {
		t.Setenv(k, "") // restores the variable after the test
		require.NoError(t, os.Unsetenv(k))
	}
	expires := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	msi := newConvCredServer(t, "application/json",
		`{"access_token":"MITOKEN","expires_on":"`+expires+`","resource":"https://cognitiveservices.azure.com","token_type":"Bearer"}`)
	t.Setenv("IDENTITY_ENDPOINT", msi.URL+"/msi/token")
	t.Setenv("IDENTITY_HEADER", "header-secret")
	return msi
}

// convAzureSpec is a keyless OpenAI spec hosted on the Azure resource w serves.
func convAzureSpec(w *convWire, id, deployment string) ProviderSpec {
	return ProviderSpec{
		ID: id, Type: "openai", Model: deployment,
		Platform: &pkgconfig.PlatformConfig{Type: "azure", Endpoint: w.URL},
	}
}

// A keyless Azure-hosted OpenAI TTS or STT spec reaches its deployment's audio
// route with an api-version, authenticated by the platform's token, the same
// way chat and embeddings do (#2677). Before, the platform was dropped and the
// request went to api.openai.com with an empty key.
func TestConv_AzurePlatformVoiceReachesTheDeployment(t *testing.T) {
	ctx := context.Background()

	t.Run("WithTTSProvider", func(t *testing.T) {
		convAzureManagedIdentity(t)
		w := newConvWire(t)
		c := &config{}
		require.NoError(t, WithTTSProvider(convAzureSpec(w, "voice", "my-tts"))(c))
		rc, err := c.ttsService.Synthesize(ctx, "say this", tts.SynthesisConfig{Voice: "alloy"})
		require.NoError(t, err)
		audio, err := io.ReadAll(rc)
		require.NoError(t, err)
		_ = rc.Close()
		assert.Equal(t, "MP3DATA", string(audio))

		reqs := w.requests("/audio/speech")
		require.Len(t, reqs, 1)
		assert.Equal(t, "/openai/deployments/my-tts/audio/speech", reqs[0].Path)
		assert.NotEmpty(t, reqs[0].Query.Get("api-version"))
		assert.Equal(t, "Bearer MITOKEN", reqs[0].Header.Get("Authorization"))
	})

	t.Run("WithSTTProvider", func(t *testing.T) {
		convAzureManagedIdentity(t)
		w := newConvWire(t)
		c := &config{}
		require.NoError(t, WithSTTProvider(convAzureSpec(w, "ears", "my-transcribe"))(c))
		text, err := c.sttService.TranscribeBytes(ctx, []byte("RIFFfake"), stt.TranscriptionConfig{Format: "wav"})
		require.NoError(t, err)
		assert.Equal(t, "transcribed words", text)

		reqs := w.requests("/audio/transcriptions")
		require.Len(t, reqs, 1)
		assert.Equal(t, "/openai/deployments/my-transcribe/audio/transcriptions", reqs[0].Path)
		assert.NotEmpty(t, reqs[0].Query.Get("api-version"))
		assert.Equal(t, "Bearer MITOKEN", reqs[0].Header.Get("Authorization"))
	})
}

// A provider file's platform block on a tts role reaches the deployment too:
// the file route delegates to WithTTSProvider with the platform it declares.
func TestConv_AzurePlatformTTSFromProviderFile(t *testing.T) {
	convAzureManagedIdentity(t)
	w := newConvWire(t)
	path := writeProviderYAML(t, t.TempDir(), "voice.provider.yaml", `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: voice
spec:
  id: voice
  type: openai
  role: tts
  model: my-tts
  platform:
    type: azure
    endpoint: `+w.URL+`
`)
	c := &config{}
	require.NoError(t, WithProviderFile(path)(c))
	rc, err := c.ttsService.Synthesize(context.Background(), "hello", tts.SynthesisConfig{Voice: "alloy"})
	require.NoError(t, err)
	_ = rc.Close()

	reqs := w.requests("/audio/speech")
	require.Len(t, reqs, 1)
	assert.Equal(t, "/openai/deployments/my-tts/audio/speech", reqs[0].Path)
	assert.Equal(t, "Bearer MITOKEN", reqs[0].Header.Get("Authorization"))
}

// No inference type runs on a platform yet. A spec that names one keeps
// calling its own endpoint with its own key, and the dropped platform is a
// warning rather than silence.
func TestConv_InferenceProviderPlatformIsIgnoredWithAWarning(t *testing.T) {
	var logs bytes.Buffer
	logger.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	w := newConvWire(t, convWireReply{Text: "positive"})
	spec := convSpecFor(w, "judge", "gpt-4o-mini")
	spec.Platform = &pkgconfig.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"}
	c := &config{}
	require.NoError(t, WithInferenceProvider(spec)(c))
	p, err := c.inferenceRegistry.Get("judge")
	require.NoError(t, err)
	_, _ = p.Infer(context.Background(), inference.Request{
		Inputs: []types.Message{{Role: "user", Content: "great"}}, Labels: []string{"positive", "negative"},
	})

	reqs := w.chatRequests()
	require.Len(t, reqs, 1)
	convAssertSpecOnWire(t, reqs[0])
	assert.Contains(t, logs.String(), "type=openai platform=azure")
}

// A self-hosted type no platform hosts keeps authenticating with its own key
// when its spec names a platform: the platform's credential chain is never
// consulted, so no platform token reaches the self-hosted endpoint.
func TestConv_UnhostedTypeKeepsItsOwnCredentialUnderAPlatform(t *testing.T) {
	convAzureManagedIdentity(t)
	w := newConvWire(t, convWireReply{Text: "local"})
	spec := convSpecFor(w, "local", "llama3")
	spec.Type = "vllm"
	spec.Platform = &pkgconfig.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"}
	conv, err := Open(createTestPackFile(t, convChatPack), "chat", WithSkipSchemaValidation(), WithLLMProvider(spec))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	_, _ = conv.Send(context.Background(), "hi")

	assert.Equal(t, "Bearer spec-key", w.lastChat(t).Header.Get("Authorization"))
}

// An ollama embedding provider under a platform builds without touching the
// platform's credential chain, which here cannot even be constructed.
func TestConv_UnhostedEmbeddingIgnoresThePlatformCredentialChain(t *testing.T) {
	t.Setenv("AZURE_TOKEN_CREDENTIALS", "NotACredentialType")
	c := &config{}
	err := WithEmbeddingProvider(ProviderSpec{
		ID: "emb", Type: "ollama", Model: "nomic-embed-text", BaseURL: "http://127.0.0.1:1",
		Platform: &pkgconfig.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"},
	})(c)
	require.NoError(t, err)
	assert.Equal(t, []string{"emb"}, c.embeddingProviderIDs)
}
