package conformance_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// optionalSessionInterfaces are the optional StreamInputSession interfaces
// the pipeline type-asserts on the session a provider returns. A session that
// hides one of them silently loses the feature (resuming with history,
// guardrail cuts, cues, tool results), so the session a provider's
// CreateStreamSession returns must implement every one its underlying session
// does.
var optionalSessionInterfaces = []reflect.Type{
	reflect.TypeFor[providers.StreamHistorySession](),
	reflect.TypeFor[providers.ToolResponseSupport](),
	reflect.TypeFor[providers.ToolResponsePartsSupport](),
	reflect.TypeFor[providers.StreamToolUpdater](),
	reflect.TypeFor[providers.ResponseInterrupter](),
	reflect.TypeFor[providers.TurnForgetter](),
	reflect.TypeFor[providers.ContextNoter](),
	reflect.TypeFor[providers.OutputAudioReporter](),
	reflect.TypeFor[providers.OutOfBandSpeaker](),
	reflect.TypeFor[interface{ EndInput() }](),
}

// The session CreateStreamSession returns keeps every capability of the
// provider's own session type: each optional interface the underlying session
// implements, and, so that an interface added later cannot be dropped by a
// wrapper either, every exported method with the same signature.
func TestCreateStreamSession_KeepsEveryCapabilityOfTheUnderlyingSession(t *testing.T) {
	cases := []struct {
		name       string
		underlying reflect.Type
		open       func(t *testing.T) providers.StreamInputSession
	}{
		{"openai", reflect.TypeFor[*openai.RealtimeSession](), openOpenAIRealtime},
		{"gemini", reflect.TypeFor[*gemini.StreamSession](), openGeminiLive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reflect.TypeOf(tc.open(t))
			var implemented int
			for _, iface := range optionalSessionInterfaces {
				if !tc.underlying.Implements(iface) {
					continue
				}
				implemented++
				assert.Truef(t, got.Implements(iface),
					"%s implements %s but the session CreateStreamSession returns (%s) does not",
					tc.underlying, iface, got)
			}
			require.NotZero(t, implemented, "the underlying session implements none of the optional interfaces")
			for i := 0; i < tc.underlying.NumMethod(); i++ {
				want := tc.underlying.Method(i)
				m, ok := got.MethodByName(want.Name)
				if assert.Truef(t, ok, "the session CreateStreamSession returns (%s) has no %s", got, want.Name) {
					assert.Equalf(t, signature(want.Type), signature(m.Type),
						"%s.%s has a different signature", got, want.Name)
				}
			}
		})
	}
}

// signature is a method's type without its receiver.
func signature(fn reflect.Type) string {
	in := make([]string, 0, fn.NumIn()-1)
	for i := 1; i < fn.NumIn(); i++ {
		in = append(in, fn.In(i).String())
	}
	out := make([]string, 0, fn.NumOut())
	for i := 0; i < fn.NumOut(); i++ {
		out = append(out, fn.Out(i).String())
	}
	return "(" + strings.Join(in, ", ") + ") (" + strings.Join(out, ", ") + ")"
}

// paritySessionAudio is PCM16 mono audio at rate, which a session takes as
// its input.
func paritySessionAudio(rate int) *providers.StreamingInputConfig {
	return &providers.StreamingInputConfig{Config: types.StreamingMediaConfig{
		Type: types.ContentTypeAudio, SampleRate: rate, Encoding: "pcm16", Channels: 1, BitDepth: 16, ChunkSize: rate / 5,
	}}
}

// parityCredential authenticates the fake realtime server's handshake.
type parityCredential struct{}

func (parityCredential) Apply(_ context.Context, req *http.Request) error {
	req.Header.Set("Authorization", "Bearer test")
	return nil
}

func (parityCredential) Type() string { return "azure" }

// openOpenAIRealtime opens a realtime session through an OpenAI provider
// whose socket goes to a stand-in server.
func openOpenAIRealtime(t *testing.T) providers.StreamInputSession {
	t.Helper()
	srv := fakeSocketServer(t, func(conn *websocket.Conn) bool {
		return conn.WriteJSON(map[string]any{"type": "session.created",
			"session": map[string]any{"id": "sess_1", "model": "gpt-realtime"}}) == nil
	})
	p := openai.NewProviderFromConfig(&openai.ProviderConfig{
		ID: "rt", Model: "gpt-realtime", Credential: parityCredential{}, Platform: "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: srv.URL + "/"},
	})
	return openSession(t, p, paritySessionAudio(24000))
}

// openGeminiLive opens a Live session through a Gemini provider whose Live
// endpoint is a stand-in server.
func openGeminiLive(t *testing.T) providers.StreamInputSession {
	t.Helper()
	srv := fakeSocketServer(t, func(conn *websocket.Conn) bool {
		if _, _, err := conn.ReadMessage(); err != nil { // setup
			return false
		}
		reply, _ := json.Marshal(gemini.ServerMessage{SetupComplete: &gemini.SetupComplete{}})
		return conn.WriteMessage(websocket.TextMessage, reply) == nil
	})
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "live", Type: "gemini", Model: "gemini-3.1-flash-live-preview",
		AdditionalConfig: map[string]any{"live_url": "ws" + strings.TrimPrefix(srv.URL, "http")},
	})
	require.NoError(t, err)
	return openSession(t, p.(providers.StreamInputSupport), paritySessionAudio(16000))
}

func openSession(
	t *testing.T, p providers.StreamInputSupport, req *providers.StreamingInputConfig,
) providers.StreamInputSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	session, err := p.CreateStreamSession(ctx, req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// fakeSocketServer accepts a WebSocket, runs handshake on it, then reads and
// discards client messages until the connection closes.
func fakeSocketServer(t *testing.T, handshake func(*websocket.Conn) bool) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if !handshake(conn) {
			return
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
