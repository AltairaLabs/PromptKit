package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// linkageHistory is a conversation whose assistant turn called a tool and
// whose tool message answered it.
func linkageHistory() []types.Message {
	return []types.Message{
		{Role: "user", Content: "Weather in Paris?"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "call_abc", Name: "get_weather", Args: json.RawMessage(`{"city":"Paris"}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call_abc", Name: "get_weather", Parts: []types.ContentPart{
			types.NewTextPart("18C and sunny"),
		}}},
	}
}

const wantLinkedMessages = `[
	{"role":"user","content":"Weather in Paris?"},
	{"role":"assistant","content":"","tool_calls":[
		{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
	{"role":"tool","content":"18C and sunny","tool_call_id":"call_abc"}
]`

func captureChatBody(t *testing.T, stream bool, run func(p *Provider)) []byte {
	t.Helper()
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)
	p := &Provider{
		BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
		model:        "gpt-4o",
		baseURL:      server.URL,
		apiKey:       "k",
		defaults:     providers.ProviderDefaults{MaxTokens: 100},
	}
	run(p)
	return body
}

func TestPredict_PlainPathKeepsToolLinkage(t *testing.T) {
	body := captureChatBody(t, false, func(p *Provider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: linkageHistory()})
		require.NoError(t, err)
	})
	var got struct {
		Messages json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.JSONEq(t, wantLinkedMessages, string(got.Messages))
}

func TestPredictStream_PlainPathKeepsToolLinkage(t *testing.T) {
	body := captureChatBody(t, true, func(p *Provider) {
		ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{Messages: linkageHistory()})
		require.NoError(t, err)
		for range ch {
		}
	})
	var got struct {
		Messages json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.JSONEq(t, wantLinkedMessages, string(got.Messages))
}

func TestPredict_PlainResponsesModeKeepsToolLinkage(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r1","status":"completed","output":[{"type":"message","role":"assistant",` +
			`"content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()
	p := &Provider{
		BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
		model:        "gpt-4o",
		baseURL:      server.URL,
		apiKey:       "k",
		apiMode:      APIModeResponses,
		defaults:     providers.ProviderDefaults{MaxTokens: 100},
	}
	_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: linkageHistory()})
	require.NoError(t, err)
	var got struct {
		Input []map[string]any `json:"input"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Len(t, got.Input, 3)
	assert.Equal(t, "function_call", got.Input[1]["type"])
	assert.Equal(t, "call_abc", got.Input[1]["call_id"])
	assert.Equal(t, "function_call_output", got.Input[2]["type"])
	assert.Equal(t, "call_abc", got.Input[2]["call_id"])
	assert.Equal(t, "18C and sunny", got.Input[2]["output"])
}

// TestPredict_PlainPathToolLinkage_Live proves OpenAI accepts the plain
// request with linked tool messages and answers from the tool result.
func TestPredict_PlainPathToolLinkage_Live(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	p := NewProvider("live", "gpt-4o-mini", "https://api.openai.com/v1",
		providers.ProviderDefaults{MaxTokens: 100}, false)
	p.apiKey = key
	hist := linkageHistory()
	hist[2].ToolResult.Parts = []types.ContentPart{types.NewTextPart("21.7C and overcast.")}
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: hist})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "21.7")
}
