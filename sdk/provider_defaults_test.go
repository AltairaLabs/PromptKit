package sdk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// recordingOpenAIServer is an OpenAI-compatible endpoint that keeps the body of
// the last chat-completions request and answers it, as SSE when asked to stream.
type recordingOpenAIServer struct {
	*httptest.Server
	mu   sync.Mutex
	body map[string]any
}

func newRecordingOpenAIServer(t *testing.T) *recordingOpenAIServer {
	t.Helper()
	s := &recordingOpenAIServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.body = body
		s.mu.Unlock()

		if stream, _ := body["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w,
				`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`+"\n\n"+
					`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
					"data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"1","choices":[{"index":0,"message":{"role":"assistant",`+
			`"content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *recordingOpenAIServer) lastBody() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body
}

// openWithOpenAIDefaults opens a conversation on a caller-built OpenAI provider
// with the given defaults, over a pack whose prompt carries promptParams.
func openWithOpenAIDefaults(
	t *testing.T, srv *recordingOpenAIServer, defaults providers.ProviderDefaults, promptParams string,
) *Conversation {
	t.Helper()
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "caller", Type: "openai", Model: "gpt-4o-mini", BaseURL: srv.URL, Defaults: defaults,
	})
	require.NoError(t, err)

	params := ""
	if promptParams != "" {
		params = `, "parameters": ` + promptParams
	}
	packPath := createTestPackFile(t, `{
		"id": "defaults-pack",
		"version": "1.0.0",
		"prompts": {
			"default": {"id": "default", "name": "Default", "version": "1.0.0",
				"system_template": "You are helpful."`+params+`}
		}
	}`)
	conv, err := Open(packPath, "default", WithProvider(p), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// sendBoth makes one Send and one Stream turn, returning each request body.
func sendBoth(t *testing.T, conv *Conversation, srv *recordingOpenAIServer) (sent, streamed map[string]any) {
	t.Helper()
	ctx := context.Background()
	_, err := conv.Send(ctx, "hi")
	require.NoError(t, err)
	sent = srv.lastBody()

	for chunk := range conv.Stream(ctx, "hi again") {
		require.NoError(t, chunk.Error)
	}
	streamed = srv.lastBody()
	require.Equal(t, true, streamed["stream"], "the second turn must go through the streaming path")
	return sent, streamed
}

// A caller's provider defaults reach the wire when the prompt sets no
// parameters (#2144). They were overwritten by the SDK's own 4096 / 0.7.
func TestConversation_CallerProviderDefaultsApply(t *testing.T) {
	srv := newRecordingOpenAIServer(t)
	conv := openWithOpenAIDefaults(t, srv, providers.ProviderDefaults{MaxTokens: 400, Temperature: 0.25}, "")

	sent, streamed := sendBoth(t, conv, srv)
	for name, body := range map[string]map[string]any{"Send": sent, "Stream": streamed} {
		assert.EqualValues(t, 400, body["max_completion_tokens"], name)
		assert.InDelta(t, 0.25, body["temperature"], 1e-6, name)
	}
}

// Prompt parameters still win over the provider's defaults.
func TestConversation_PromptParametersOverrideProviderDefaults(t *testing.T) {
	srv := newRecordingOpenAIServer(t)
	conv := openWithOpenAIDefaults(t, srv, providers.ProviderDefaults{MaxTokens: 400, Temperature: 0.25},
		`{"max_tokens": 123, "temperature": 0.5}`)

	sent, streamed := sendBoth(t, conv, srv)
	for name, body := range map[string]map[string]any{"Send": sent, "Stream": streamed} {
		assert.EqualValues(t, 123, body["max_completion_tokens"], name)
		assert.InDelta(t, 0.5, body["temperature"], 1e-6, name)
	}
}

// With no limit from the prompt or the provider, none is sent, so the model's
// own maximum applies; MaxTokensUnlimited says the same thing explicitly.
func TestConversation_NoMaxTokensSendsNoLimit(t *testing.T) {
	for name, maxTokens := range map[string]int{"unset": 0, "unlimited": providers.MaxTokensUnlimited} {
		t.Run(name, func(t *testing.T) {
			srv := newRecordingOpenAIServer(t)
			conv := openWithOpenAIDefaults(t, srv, providers.ProviderDefaults{MaxTokens: maxTokens}, "")

			sent, streamed := sendBoth(t, conv, srv)
			for turn, body := range map[string]map[string]any{"Send": sent, "Stream": streamed} {
				for key := range body {
					assert.False(t, strings.Contains(key, "max_"), "%s sent %s", turn, key)
				}
			}
		})
	}
}
