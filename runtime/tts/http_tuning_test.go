package tts_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// headerRecorder is an httptest handler that records one request header and
// answers with a few bytes of "audio".
type headerRecorder struct {
	mu   sync.Mutex
	name string
	got  string
}

func (h *headerRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.got = r.Header.Get(h.name)
	h.mu.Unlock()
	w.Header().Set("Content-Type", "audio/mpeg")
	_, _ = w.Write([]byte("audio"))
}

func (h *headerRecorder) value() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.got
}

func TestTTSCreateFromSpec_OpenAITuningHeaderReachesServer(t *testing.T) {
	rec := &headerRecorder{name: "X-Gateway"}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		Type: "openai", BaseURL: srv.URL,
		Credential: credentials.NewAPIKeyCredential("sk-test"),
		Tuning:     base.HTTPTuning{Headers: map[string]string{"X-Gateway": "gw-1"}},
	})
	require.NoError(t, err)

	rc, err := svc.Synthesize(context.Background(), "hello", tts.SynthesisConfig{})
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
	assert.Equal(t, "gw-1", rec.value())
}

func TestTTSCreateFromSpec_ElevenLabsCredentialHeaderCollision(t *testing.T) {
	rec := &headerRecorder{name: "xi-api-key"}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		Type: "elevenlabs", BaseURL: srv.URL,
		Credential: credentials.NewAPIKeyCredential("el-key"),
		Tuning:     base.HTTPTuning{Headers: map[string]string{"xi-api-key": "other"}},
	})
	require.NoError(t, err)

	rc, err := svc.Synthesize(context.Background(), "hello", tts.SynthesisConfig{})
	if rc != nil {
		_ = rc.Close()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), `custom header "xi-api-key" collides with built-in header`)
	assert.Empty(t, rec.value(), "the request must not reach the server")
}

func TestTTSCreateFromSpec_CartesiaWebsocketDialCarriesHeader(t *testing.T) {
	var (
		mu  sync.Mutex
		got string
	)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Get("X-Gateway")
		mu.Unlock()
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		Type:             "cartesia",
		Credential:       credentials.NewAPIKeyCredential("car-key"),
		AdditionalConfig: map[string]any{"ws_url": "ws" + strings.TrimPrefix(srv.URL, "http")},
		Tuning:           base.HTTPTuning{Headers: map[string]string{"X-Gateway": "gw-ws"}},
	})
	require.NoError(t, err)
	streamer, ok := svc.(tts.StreamingService)
	require.True(t, ok)

	ch, err := streamer.SynthesizeStream(context.Background(), "hello", tts.SynthesisConfig{})
	if err == nil {
		for range ch { //nolint:revive // drain until the server closes
		}
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "gw-ws", got)
}

func TestTTSCreateFromSpec_CartesiaWebsocketHeaderCollision(t *testing.T) {
	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		Type:             "cartesia",
		Credential:       credentials.NewAPIKeyCredential("car-key"),
		AdditionalConfig: map[string]any{"ws_url": "ws://127.0.0.1:1"},
		Tuning:           base.HTTPTuning{Headers: map[string]string{"x-api-key": "other"}},
	})
	require.NoError(t, err)
	_, err = svc.(tts.StreamingService).SynthesizeStream(context.Background(), "hello", tts.SynthesisConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides with built-in header")
}
