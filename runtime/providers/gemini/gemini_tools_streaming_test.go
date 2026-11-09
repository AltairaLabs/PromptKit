package gemini

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The tool provider is what CreateProviderFromSpec returns for type gemini, so
// its streaming-input surface must be the base provider's: the same media
// types, the same audio format, and a session built from the caller's config
// with the provider's model and key.
func TestToolProvider_StreamingMethodsForwardToBaseProvider(t *testing.T) {
	toolProvider := NewToolProvider("test-id", "gemini-live-x", "", providers.ProviderDefaults{}, false)

	assert.Equal(t, []string{types.ContentTypeAudio, types.ContentTypeVideo, types.ContentTypeImage},
		toolProvider.SupportsStreamInput())

	caps := toolProvider.GetStreamingCapabilities()
	require.NotNil(t, caps.Audio)
	assert.Equal(t, 16000, caps.Audio.PreferredSampleRate)
	assert.Equal(t, []int{1}, caps.Audio.SupportedChannels)
	assert.True(t, caps.BidirectionalSupport)

	toolProvider.apiKey = "key-123"
	var gotKey string
	var gotCfg *StreamSessionConfig
	want := &StreamSession{}
	toolProvider.newStreamSessionFn = func(ctx context.Context, _, apiKey string, cfg *StreamSessionConfig) (*StreamSession, error) {
		gotKey, gotCfg = apiKey, cfg
		sctx, cancel := context.WithCancel(ctx)
		want.ctx, want.cancel, want.errCh = sctx, cancel, make(chan error, 1)
		return want, nil
	}
	session, err := toolProvider.CreateStreamSession(context.Background(), &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type: types.ContentTypeAudio, ChunkSize: 3200, SampleRate: 16000, Channels: 1, BitDepth: 16,
			Encoding: "pcm_linear16",
		},
		SystemInstruction: "be brief",
	})
	require.NoError(t, err)
	t.Cleanup(func() { want.cancel() })

	assert.Same(t, want, session)
	assert.Equal(t, "key-123", gotKey)
	require.NotNil(t, gotCfg)
	assert.Equal(t, "gemini-live-x", gotCfg.Model)
	assert.Equal(t, "be brief", gotCfg.SystemInstruction)
	assert.Equal(t, []string{"TEXT"}, gotCfg.ResponseModalities)
}

// Registering the gemini factory makes the type creatable from a spec, and
// what it builds is the tool-capable provider.
func TestGeminiType_IsRegisteredAsToolProvider(t *testing.T) {
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "g", Type: "gemini", Model: "gemini-2.5-flash", BaseURL: "https://example.invalid",
	})
	require.NoError(t, err)
	tp, ok := p.(*ToolProvider)
	require.True(t, ok, "gemini spec must build *ToolProvider, got %T", p)
	assert.Equal(t, "g", tp.ID())
	assert.Equal(t, "gemini-2.5-flash", tp.Model())
}
