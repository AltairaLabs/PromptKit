package gemini

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

type vertexToken struct{}

func (vertexToken) Apply(_ context.Context, req *http.Request) error {
	req.Header.Set("Authorization", "Bearer gcp-token")
	return nil
}

func (vertexToken) Type() string { return "gcp" }

// A Live session on a Vertex-hosted provider is refused before anything is
// dialed. Without the check it opened the AI Studio socket with the provider's
// empty static key and failed there, far from the cause.
func TestLive_VertexPlatformIsRejectedBeforeDialing(t *testing.T) {
	p := NewProviderWithCredential("live", "gemini-3.8-live", "", providers.ProviderDefaults{}, false,
		vertexToken{}, "vertex", &providers.PlatformConfig{Type: "vertex", Project: "proj", Region: "us-central1"})
	dialed := false
	p.newStreamSessionFn = func(context.Context, string, string, *StreamSessionConfig) (*StreamSession, error) {
		dialed = true
		return nil, nil
	}

	_, err := p.CreateStreamSession(context.Background(), &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type: types.ContentTypeAudio, SampleRate: 16000, Encoding: "pcm16", Channels: 1, ChunkSize: 3200, BitDepth: 16,
		},
	})
	require.Error(t, err)
	assert.Equal(t, `live sessions are not available on platform "vertex": Gemini Live runs on the Gemini API only`, err.Error())
	assert.False(t, dialed)
}
