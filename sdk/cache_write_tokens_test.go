package sdk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
)

// The responses under testdata/claude_cache_write were recorded from the
// Claude Messages API: a call that wrote the prompt prefix to the cache, as
// JSON (messages.json: cache_creation_input_tokens 9061) and as a stream
// (messages.sse: 9077).

// claudeRecordedServer serves the recorded Claude response matching the
// request: the stream for a streamed request, the JSON otherwise.
func claudeRecordedServer(t *testing.T) *httptest.Server {
	t.Helper()
	jsonBody, err := os.ReadFile("testdata/claude_cache_write/messages.json")
	require.NoError(t, err)
	sseBody, err := os.ReadFile("testdata/claude_cache_write/messages.sse")
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write(sseBody)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jsonBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// completedCalls collects the provider.call.completed events of conv.
func completedCalls(conv *Conversation) func(t *testing.T) []events.ProviderCallCompletedData {
	var mu sync.Mutex
	var got []events.ProviderCallCompletedData
	conv.EventBus().Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
		if d, ok := e.Data.(*events.ProviderCallCompletedData); ok {
			mu.Lock()
			got = append(got, *d)
			mu.Unlock()
		}
	})
	return func(t *testing.T) []events.ProviderCallCompletedData {
		t.Helper()
		require.Eventually(t, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(got) > 0
		}, 2*time.Second, 5*time.Millisecond, "no provider.call.completed event")
		mu.Lock()
		defer mu.Unlock()
		return append([]events.ProviderCallCompletedData(nil), got...)
	}
}

// unaryClaude is the Claude provider on its non-streaming path.
type unaryClaude struct{ *claude.ToolProvider }

func (unaryClaude) SupportsStreaming() bool { return false }

// provider.call.completed carries the cache writes Claude reported for the
// call, separately from the uncached input and the cache reads, on the
// streaming path and on the non-streaming one.
func TestSend_ProviderCallReportsClaudeCacheWrites(t *testing.T) {
	cases := []struct {
		name      string
		stream    bool
		wantWrite int
		wantOut   int
	}{
		{name: "stream", stream: true, wantWrite: 9077, wantOut: 16},
		{name: "json", stream: false, wantWrite: 9061, wantOut: 16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := claudeRecordedServer(t)
			t.Setenv("ANTHROPIC_API_KEY", "k")
			var provider providers.Provider = claude.NewToolProvider("claude", "claude-sonnet-4-6", srv.URL,
				providers.ProviderDefaults{MaxTokens: 64}, false)
			if !tc.stream {
				provider = unaryClaude{provider.(*claude.ToolProvider)}
			}
			conv, err := Open(writeTestPack(t), "chat", WithProvider(provider), WithSkipSchemaValidation())
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			calls := completedCalls(conv)

			resp, err := conv.Send(context.Background(), "Hello!")
			require.NoError(t, err)
			assert.Equal(t, "Hello! Welcome to support — how can I help you today?", resp.Text())

			got := calls(t)
			require.Len(t, got, 1)
			assert.Equal(t, tc.wantWrite, got[0].CacheWriteTokens)
			assert.Equal(t, 3, got[0].InputTokens, "cache writes stay out of the input count")
			assert.Equal(t, 0, got[0].CachedTokens)
			assert.Equal(t, tc.wantOut, got[0].OutputTokens)
		})
	}
}
