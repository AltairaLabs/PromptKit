package claude

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
			{ID: "toolu_abc", Name: "get_weather", Args: json.RawMessage(`{"city":"Paris"}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "toolu_abc", Name: "get_weather", Parts: []types.ContentPart{
			types.NewTextPart("18C and sunny"),
		}}},
		{Role: "user", Content: "Should I take a coat?"},
	}
}

// The tool result goes back in a user message as a tool_result block that
// names the tool_use it answers, ahead of the follow-up question.
const wantLinkedMessages = `[
	{"role":"user","content":[{"type":"text","text":"Weather in Paris?"}]},
	{"role":"assistant","content":[
		{"type":"tool_use","id":"toolu_abc","name":"get_weather","input":{"city":"Paris"}}]},
	{"role":"user","content":[
		{"type":"tool_result","tool_use_id":"toolu_abc","content":"18C and sunny"},
		{"type":"text","text":"Should I take a coat?"}]}
]`

const (
	plainJSONResponse = `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],` +
		`"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	plainSSEResponse = "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
)

func captureMessages(t *testing.T, stream bool, defaults providers.ProviderDefaults,
	run func(p *ToolProvider)) json.RawMessage {
	t.Helper()
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(plainSSEResponse))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(plainJSONResponse))
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_API_KEY", "k")
	run(NewToolProvider("linkage", "claude-sonnet-4-6", server.URL, defaults, false))
	var got struct {
		Messages json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	return got.Messages
}

func TestPredict_PlainPathKeepsToolLinkage(t *testing.T) {
	got := captureMessages(t, false, providers.ProviderDefaults{}, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: linkageHistory()})
		require.NoError(t, err)
	})
	assert.JSONEq(t, wantLinkedMessages, string(got))
}

func TestPredictStream_PlainPathKeepsToolLinkage(t *testing.T) {
	got := captureMessages(t, true, providers.ProviderDefaults{}, func(p *ToolProvider) {
		ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{Messages: linkageHistory()})
		require.NoError(t, err)
		for range ch {
		}
	})
	assert.JSONEq(t, wantLinkedMessages, string(got))
}

// A tool result image goes out inside the tool_result block on the plain path,
// as it does on the tool path.
func TestPredict_PlainPathKeepsToolResultImage(t *testing.T) {
	got := captureMessages(t, false, providers.ProviderDefaults{}, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: []types.Message{
			{Role: "user", Content: "Take a picture"},
			{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "toolu_img", Name: "camera", Args: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolResult: &types.MessageToolResult{ID: "toolu_img", Name: "camera", Parts: []types.ContentPart{
				types.NewTextPart("done"),
				types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil),
			}}},
		}})
		require.NoError(t, err)
	})
	assert.JSONEq(t, `[
		{"role":"user","content":[{"type":"text","text":"Take a picture"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"toolu_img","name":"camera","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_img","content":[
			{"type":"text","text":"done"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aW1n"}}]}]}
	]`, string(got))
}

// The plain and tool paths serialize one history to the same messages. Prompt
// caching is off so the comparison is of serialization alone: the tool path
// places its own cache breakpoints.
func TestPlainAndToolPathsSerializeHistoryIdentically(t *testing.T) {
	history := append(linkageHistory(),
		types.Message{Role: "assistant", Content: "Let me look.", ToolCalls: []types.MessageToolCall{
			{ID: "toolu_1", Name: "get_weather", Args: json.RawMessage(`{"city":"Lyon"}`)},
			{ID: "toolu_2", Name: "camera", Args: nil},
		}},
		types.Message{Role: "tool", ToolResult: &types.MessageToolResult{ID: "toolu_1", Name: "get_weather",
			Parts: []types.ContentPart{types.NewTextPart("12C")}}},
		types.Message{Role: "tool", ToolResult: &types.MessageToolResult{ID: "toolu_2", Name: "camera",
			Parts: []types.ContentPart{types.NewTextPart("shot"),
				types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil)}}},
		types.Message{Role: "assistant", Content: "Lyon is 12C."},
		types.Message{Role: "user", Content: "thanks"},
	)
	req := providers.PredictionRequest{System: "be brief", MaxTokens: 50, Messages: history}
	noCache := providers.ProviderDefaults{DisablePromptCaching: true}

	plain := captureMessages(t, false, noCache, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), req)
		require.NoError(t, err)
	})
	withTools := captureMessages(t, false, noCache, func(p *ToolProvider) {
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
	assert.Len(t, msgs, 7)
}

// Claude on Vertex (and Bedrock, Azure) is the same Provider with another
// platform, so its plain request carries the same linked messages.
func TestVertexPredictStream_PlainPathKeepsToolLinkage(t *testing.T) {
	p, capture := newVertexStreamingTestProvider(t, plainSSEResponse)
	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{Messages: linkageHistory()})
	require.NoError(t, err)
	for range ch {
	}
	var got struct {
		Messages json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal([]byte(capture.body), &got))
	assert.JSONEq(t, wantLinkedMessages, string(got.Messages))
}

// With tools and caching on, the tool path caches the prefix through the first
// message only when that message is the user's: an assistant opener gets no
// breakpoint, and the last block carries the rolling one.
func TestToolPath_FirstMessageBreakpointIsUserOnly(t *testing.T) {
	got := captureMessages(t, false, providers.ProviderDefaults{}, func(p *ToolProvider) {
		tools, err := p.BuildTooling([]*providers.ToolDescriptor{{
			Name: "get_weather", Description: "w", InputSchema: json.RawMessage(`{"type":"object"}`),
		}})
		require.NoError(t, err)
		_, _, err = p.PredictWithTools(context.Background(), providers.PredictionRequest{Messages: []types.Message{
			{Role: "assistant", Content: "How can I help?"},
			{Role: "user", Content: "Weather?"},
		}}, tools, "auto")
		require.NoError(t, err)
	})
	assert.JSONEq(t, `[
		{"role":"assistant","content":[{"type":"text","text":"How can I help?"}]},
		{"role":"user","content":[{"type":"text","text":"Weather?","cache_control":{"type":"ephemeral"}}]}
	]`, string(got))
}
