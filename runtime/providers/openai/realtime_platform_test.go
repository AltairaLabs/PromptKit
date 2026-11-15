package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// tokenCredential stands in for a platform token source (Entra ID, GCP): each
// Apply hands out the next token, so a test can tell a token fetched when the
// socket was dialed from one captured earlier.
type tokenCredential struct {
	mu     sync.Mutex
	issued int
}

func (c *tokenCredential) Apply(_ context.Context, req *http.Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.issued++
	req.Header.Set("Authorization", fmt.Sprintf("Bearer entra-token-%d", c.issued))
	return nil
}

func (c *tokenCredential) Type() string { return "azure" }

// realtimeHandshake is what the fake Realtime server saw on the upgrade request.
type realtimeHandshake struct {
	Path  string
	Query url.Values
	Auth  string
}

// newHandshakeServer is a fake Realtime WebSocket server that records each
// handshake, then sends session.created and drains the client.
func newHandshakeServer(t *testing.T) (*httptest.Server, func() []realtimeHandshake) {
	t.Helper()
	var mu sync.Mutex
	var seen []realtimeHandshake
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, realtimeHandshake{Path: r.URL.Path, Query: r.URL.Query(), Auth: r.Header.Get("Authorization")})
		mu.Unlock()
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess_test", "model": "gpt-realtime"},
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []realtimeHandshake {
		mu.Lock()
		defer mu.Unlock()
		return append([]realtimeHandshake(nil), seen...)
	}
}

func audioStreamRequest() *providers.StreamingInputConfig {
	return &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type: types.ContentTypeAudio, SampleRate: 24000, Encoding: "pcm16", Channels: 1, ChunkSize: 4800,
		},
	}
}

// A realtime session on an Azure-hosted provider dials the platform's v1
// realtime endpoint for its deployment, authenticated by the platform
// credential, never api.openai.com with the (empty) static key (#2677).
func TestRealtime_AzurePlatformDialsTheDeploymentWithThePlatformToken(t *testing.T) {
	srv, handshakes := newHandshakeServer(t)
	cred := &tokenCredential{}
	p := NewProviderFromConfig(&ProviderConfig{
		ID: "azure-realtime", Model: "my-realtime-deployment",
		Credential: cred, Platform: "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: srv.URL + "/"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := p.CreateStreamSession(ctx, audioStreamRequest())
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	got := handshakes()
	require.Len(t, got, 1)
	assert.Equal(t, "/openai/v1/realtime", got[0].Path)
	assert.Equal(t, "my-realtime-deployment", got[0].Query.Get("model"))
	assert.Empty(t, got[0].Query.Get("api-version"), "the GA realtime endpoint rejects an api-version")
	assert.Equal(t, "Bearer entra-token-1", got[0].Auth)
}

// Realtime is not offered on Bedrock or Vertex, so a session there fails
// before dialing anything, naming the platform, instead of silently dialing
// OpenAI with an empty key.
func TestRealtime_UnwiredPlatformIsRejected(t *testing.T) {
	for _, platform := range []string{"bedrock", "vertex"} {
		t.Run(platform, func(t *testing.T) {
			p := NewProviderFromConfig(&ProviderConfig{
				ID: "realtime", Model: "gpt-realtime", Credential: &tokenCredential{}, Platform: platform,
				PlatformConfig: &providers.PlatformConfig{Type: platform, Region: "us-east-1", Project: "p"},
			})
			_, err := p.CreateStreamSession(context.Background(), audioStreamRequest())
			require.Error(t, err)
			assert.Contains(t, err.Error(),
				fmt.Sprintf("OpenAI realtime is not available on platform %q (supported: azure)", platform))
		})
	}
}

// An Azure realtime session needs the resource endpoint and a credential;
// without either it fails before dialing, saying which is missing.
func TestRealtime_AzurePlatformNeedsEndpointAndCredential(t *testing.T) {
	cases := map[string]struct {
		cfg  ProviderConfig
		want string
	}{
		"no endpoint": {
			ProviderConfig{Model: "d", Credential: &tokenCredential{}, Platform: "azure",
				PlatformConfig: &providers.PlatformConfig{Type: "azure"}},
			"OpenAI realtime on azure needs the platform endpoint",
		},
		"no credential": {
			ProviderConfig{Model: "d", Platform: "azure",
				PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"}},
			"OpenAI realtime on azure needs a platform credential",
		},
		"bad endpoint": {
			ProviderConfig{Model: "d", Credential: &tokenCredential{}, Platform: "azure",
				PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: "res.openai.azure.com"}},
			`OpenAI realtime on azure: invalid endpoint "res.openai.azure.com/openai/deployments/d"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.ID = "rt"
			_, err := NewProviderFromConfig(&cfg).CreateStreamSession(context.Background(), audioStreamRequest())
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
		})
	}
}

// The realtime route is derived from the resource host whatever path the
// endpoint carries, over TLS for an https resource.
func TestAzureRealtimeEndpoint_UsesTheResourceHost(t *testing.T) {
	got, err := azureRealtimeEndpoint("https://res.openai.azure.com/openai/deployments/old")
	require.NoError(t, err)
	assert.Equal(t, "wss://res.openai.azure.com/openai/v1/realtime", got)
}

// An Azure provider fronted by a gateway (base_url) opens its realtime
// session through that gateway, as its chat requests do, not straight to the
// resource host.
func TestRealtime_AzurePlatformHonorsTheBaseURL(t *testing.T) {
	gateway, handshakes := newHandshakeServer(t)
	p := NewProviderFromConfig(&ProviderConfig{
		ID: "azure-realtime", Model: "dep", BaseURL: gateway.URL + "/openai/deployments/dep",
		Credential: &tokenCredential{}, Platform: "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: "https://private.openai.azure.com"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := p.CreateStreamSession(ctx, audioStreamRequest())
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	got := handshakes()
	require.Len(t, got, 1)
	assert.Equal(t, "/openai/v1/realtime", got[0].Path)
}

// Stream input is advertised exactly where a realtime session can open.
func TestRealtime_StreamInputFollowsThePlatform(t *testing.T) {
	azure := NewProviderFromConfig(&ProviderConfig{ID: "a", Model: "gpt-realtime", Credential: &tokenCredential{},
		Platform: "azure", PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"}})
	assert.Equal(t, []string{types.ContentTypeAudio}, azure.SupportsStreamInput())

	vertex := NewProviderFromConfig(&ProviderConfig{ID: "v", Model: "gpt-realtime", Credential: &tokenCredential{},
		Platform: "vertex", PlatformConfig: &providers.PlatformConfig{Type: "vertex", Project: "p", Region: "r"}})
	assert.Empty(t, vertex.SupportsStreamInput())
}
