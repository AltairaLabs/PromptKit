package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/internal/streaming"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// vertexToken stands in for the GCP token source: each Apply issues the next token.
type vertexToken struct{ issued *int }

func (v vertexToken) Apply(_ context.Context, req *http.Request) error {
	*v.issued++
	req.Header.Set("Authorization", "Bearer gcp-token-"+string(rune('0'+*v.issued)))
	return nil
}

func (vertexToken) Type() string { return "gcp" }

func liveAudioRequest() *providers.StreamingInputConfig {
	return &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type: types.ContentTypeAudio, SampleRate: 16000, Encoding: "pcm16", Channels: 1, ChunkSize: 3200, BitDepth: 16,
		},
	}
}

func vertexLiveProvider(region string, issued *int) *Provider {
	return NewProviderWithCredential("live", "gemini-3.8-live", "", providers.ProviderDefaults{}, false,
		vertexToken{issued: issued}, "vertex", &providers.PlatformConfig{Type: "vertex", Project: "proj", Region: region})
}

// A Live session on a Vertex-hosted provider targets the location's Vertex
// Live endpoint, names the model by its Vertex resource, and authenticates
// with the platform token instead of an API key.
func TestLive_VertexPlatformTargetsTheVertexLiveEndpoint(t *testing.T) {
	cases := []struct{ region, url string }{
		{"us-central1", "wss://us-central1-aiplatform.googleapis.com/ws/google.cloud.aiplatform.v1.LlmBidiService/BidiGenerateContent"},
		{"us", "wss://aiplatform.us.rep.googleapis.com/ws/google.cloud.aiplatform.v1.LlmBidiService/BidiGenerateContent"},
	}
	for _, tc := range cases {
		t.Run(tc.region, func(t *testing.T) {
			issued := 0
			p := vertexLiveProvider(tc.region, &issued)
			var gotURL, gotKey string
			var gotCfg StreamSessionConfig
			p.newStreamSessionFn = func(ctx context.Context, url, key string, cfg *StreamSessionConfig) (*StreamSession, error) {
				gotURL, gotKey, gotCfg = url, key, *cfg
				sctx, cancel := context.WithCancel(ctx)
				return &StreamSession{ctx: sctx, cancel: cancel, errCh: make(chan error, 1)}, nil
			}
			_, err := p.CreateStreamSession(context.Background(), liveAudioRequest())
			require.NoError(t, err)

			assert.Equal(t, tc.url, gotURL)
			assert.Empty(t, gotKey)
			assert.Equal(t, "projects/proj/locations/"+tc.region+"/publishers/google/models/gemini-3.8-live", gotCfg.Model)
			require.NotNil(t, gotCfg.dialHeaders)
			h, err := gotCfg.dialHeaders(context.Background())
			require.NoError(t, err)
			assert.Equal(t, "Bearer gcp-token-1", h.Get("Authorization"))
		})
	}
}

// The session sends the platform token on the handshake (fetched per dial)
// with no API key, and the Vertex model resource in setup unchanged.
func TestLiveSession_DialHeadersAuthenticateTheHandshake(t *testing.T) {
	type handshake struct{ auth, key, model string }
	got := make(chan handshake, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, key := r.Header.Get("Authorization"), r.Header.Get(apiKeyHeader)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var setup struct {
			Setup struct {
				Model string `json:"model"`
			} `json:"setup"`
		}
		_ = json.Unmarshal(data, &setup)
		got <- handshake{auth: auth, key: key, model: setup.Setup.Model}
		reply, _ := json.Marshal(ServerMessage{SetupComplete: &SetupComplete{}})
		_ = conn.WriteMessage(websocket.TextMessage, reply)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	issued := 0
	cfg := &StreamSessionConfig{
		Model:              "projects/proj/locations/us-central1/publishers/google/models/gemini-3.8-live",
		ResponseModalities: []string{"TEXT"},
		dialHeaders:        streaming.CredentialDialHeaders(vertexToken{issued: &issued}, srv.URL),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := NewStreamSession(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), "", cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	h := <-got
	assert.Equal(t, "Bearer gcp-token-1", h.auth)
	assert.Empty(t, h.key, "no API key is sent on a platform session")
	assert.Equal(t, cfg.Model, h.model)
}

// Bedrock and Azure host no Gemini Live, so a session there is refused
// before anything is dialed.
func TestLive_UnhostedPlatformIsRejectedBeforeDialing(t *testing.T) {
	issued := 0
	p := NewProviderWithCredential("live", "gemini-3.8-live", "", providers.ProviderDefaults{}, false,
		vertexToken{issued: &issued}, "bedrock", &providers.PlatformConfig{Type: "bedrock", Region: "us-east-1"})
	dialed := false
	p.newStreamSessionFn = func(context.Context, string, string, *StreamSessionConfig) (*StreamSession, error) {
		dialed = true
		return nil, nil
	}
	_, err := p.CreateStreamSession(context.Background(), liveAudioRequest())
	require.Error(t, err)
	assert.Equal(t, `live sessions are not available on platform "bedrock" (supported: vertex)`, err.Error())
	assert.False(t, dialed)
	assert.Empty(t, p.SupportsStreamInput())
}

// A Vertex provider advertises the stream input its sessions accept.
func TestLive_VertexPlatformAdvertisesStreamInput(t *testing.T) {
	issued := 0
	assert.Equal(t, []string{types.ContentTypeAudio, types.ContentTypeVideo, types.ContentTypeImage},
		vertexLiveProvider("us-central1", &issued).SupportsStreamInput())
}

// A reconnect after a dropped socket authenticates with a token fetched for
// that dial, not the one the session opened with.
func TestLiveSession_ReconnectFetchesAFreshToken(t *testing.T) {
	auths := make(chan string, 4)
	var dials int
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dials++
		first := dials == 1
		auths <- r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		reply, _ := json.Marshal(ServerMessage{SetupComplete: &SetupComplete{}})
		_ = conn.WriteMessage(websocket.TextMessage, reply)
		if first {
			return // drop the socket to force a reconnect
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	issued := 0
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := NewStreamSession(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), "", &StreamSessionConfig{
		Model:              "projects/p/locations/us-central1/publishers/google/models/gemini-3.8-live",
		ResponseModalities: []string{"TEXT"},
		AutoReconnect:      true,
		MaxReconnectTries:  2,
		dialHeaders:        streaming.CredentialDialHeaders(vertexToken{issued: &issued}, srv.URL),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	var got []string
	for len(got) < 2 {
		select {
		case a := <-auths:
			got = append(got, a)
		case <-ctx.Done():
			t.Fatalf("no reconnect; handshakes so far: %v", got)
		}
	}
	assert.Equal(t, []string{"Bearer gcp-token-1", "Bearer gcp-token-2"}, got)
}

// A Vertex Live session needs the project, region and credential the
// endpoint and token come from; without them it fails before dialing.
func TestLive_VertexPlatformNeedsProjectRegionAndCredential(t *testing.T) {
	issued := 0
	cases := map[string]struct {
		cred providers.Credential
		pc   *providers.PlatformConfig
		want string
	}{
		"no region": {vertexToken{issued: &issued}, &providers.PlatformConfig{Type: "vertex", Project: "p"},
			"live sessions on vertex need platform.project and platform.region"},
		"no credential": {nil, &providers.PlatformConfig{Type: "vertex", Project: "p", Region: "us-central1"},
			"live sessions on vertex need a platform credential"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := NewProviderWithCredential("live", "gemini-3.8-live", "https://x", providers.ProviderDefaults{}, false,
				tc.cred, "vertex", tc.pc)
			p.credential = tc.cred
			_, err := p.CreateStreamSession(context.Background(), liveAudioRequest())
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
		})
	}
}
