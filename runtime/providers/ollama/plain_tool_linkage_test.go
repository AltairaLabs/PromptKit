package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// linkageHistory is a conversation whose assistant turn called a tool, whose
// tool message answered it, and whose user then asked a follow-up.
func linkageHistory() []types.Message {
	return []types.Message{
		{Role: "user", Content: "Weather in Paris?"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "call_abc", Name: "get_weather", Args: json.RawMessage(`{"city":"Paris"}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call_abc", Name: "get_weather", Parts: []types.ContentPart{
			types.NewTextPart("18C and sunny"),
		}}},
		{Role: "user", Content: "Should I take a coat?"},
	}
}

const wantLinkedMessages = `[
	{"role":"user","content":"Weather in Paris?"},
	{"role":"assistant","content":"","tool_calls":[
		{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
	{"role":"tool","name":"get_weather","content":"18C and sunny","tool_call_id":"call_abc"},
	{"role":"user","content":"Should I take a coat?"}
]`

func captureMessages(t *testing.T, stream bool, run func(p *ToolProvider)) json.RawMessage {
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
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)
	run(NewToolProvider("linkage", "llama3.2", server.URL, providers.ProviderDefaults{}, false, nil))
	var got struct {
		Messages json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	return got.Messages
}

func TestPredict_PlainPathKeepsToolLinkage(t *testing.T) {
	got := captureMessages(t, false, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: linkageHistory()})
		require.NoError(t, err)
	})
	assert.JSONEq(t, wantLinkedMessages, string(got))
}

func TestPredictStream_PlainPathKeepsToolLinkage(t *testing.T) {
	got := captureMessages(t, true, func(p *ToolProvider) {
		ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{Messages: linkageHistory()})
		require.NoError(t, err)
		for range ch {
		}
	})
	assert.JSONEq(t, wantLinkedMessages, string(got))
}

// A tool message carries text only: its media is described, never loaded, on
// the plain path as on the tool path, and a nameless result sends no name.
func TestPredict_PlainPathToolMessageDescribesMedia(t *testing.T) {
	got := captureMessages(t, false, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: []types.Message{
			{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call_1", Parts: []types.ContentPart{
				types.NewTextPart("ok"),
				{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: types.MIMETypeImagePNG}},
			}}},
		}})
		require.NoError(t, err)
	})
	var msgs []map[string]any
	require.NoError(t, json.Unmarshal(got, &msgs))
	require.Len(t, msgs, 1)
	assert.Equal(t, "call_1", msgs[0]["tool_call_id"])
	assert.NotContains(t, msgs[0], "name")
	assert.Contains(t, msgs[0]["content"], "ok\n[This tool returned an image (image/png) that was not sent")
}

// The plain and tool paths serialize one history to the same messages.
func TestPlainAndToolPathsSerializeHistoryIdentically(t *testing.T) {
	history := append(linkageHistory(),
		types.Message{Role: "assistant", Content: "Checking.", ToolCalls: []types.MessageToolCall{
			{ID: "call_1", Name: "get_weather", Args: json.RawMessage(`{"city":"Lyon"}`)},
			{ID: "call_2", Name: "get_weather", Args: nil},
		}},
		types.Message{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call_1", Name: "get_weather",
			Parts: []types.ContentPart{types.NewTextPart("12C")}}},
		types.Message{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call_2", Name: "get_weather",
			Parts: []types.ContentPart{types.NewTextPart("9C")}}},
		types.Message{Role: "user", Parts: []types.ContentPart{types.NewTextPart("and this?"),
			types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil)}},
	)
	req := providers.PredictionRequest{System: "be brief", MaxTokens: 50, Messages: history}

	plain := captureMessages(t, false, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), req)
		require.NoError(t, err)
	})
	withTools := captureMessages(t, false, func(p *ToolProvider) {
		tools, err := p.BuildTooling([]*providers.ToolDescriptor{{
			Name: "get_weather", Description: "w", InputSchema: json.RawMessage(`{"type":"object"}`),
		}})
		require.NoError(t, err)
		_, _, err = p.PredictWithTools(context.Background(), req, tools, "auto")
		require.NoError(t, err)
	})

	assert.JSONEq(t, string(withTools), string(plain))
	var msgs []map[string]any
	require.NoError(t, json.Unmarshal(plain, &msgs))
	assert.Len(t, msgs, 9)
}

// tool_call_id belongs to role=tool messages only.
func TestPredict_PlainPathToolResultOnNonToolRoleHasNoToolCallID(t *testing.T) {
	got := captureMessages(t, false, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: []types.Message{
			{Role: "user", Content: "hi", ToolResult: &types.MessageToolResult{ID: "call_x", Name: "x"}},
		}})
		require.NoError(t, err)
	})
	assert.JSONEq(t, `[{"role":"user","content":"hi"}]`, string(got))
}
