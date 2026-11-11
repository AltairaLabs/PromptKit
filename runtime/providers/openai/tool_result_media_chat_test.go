package openai

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

// chatRequestOf sends msgs through the Chat Completions tool path, once
// non-streaming and once streaming, and returns the "messages" array each
// request carried.
func chatRequestsOf(t *testing.T, msgs []types.Message) map[string][]map[string]any {
	t.Helper()
	out := map[string][]map[string]any{}
	for _, stream := range []bool{false, true} {
		var body map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			require.NoError(t, json.Unmarshal(raw, &body))
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
				return
			}
			_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[{"index":0,` +
				`"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],` +
				`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		}))
		t.Setenv("OPENAI_API_KEY", "k")
		tp := NewToolProvider("media", "gpt-4o", srv.URL, providers.ProviderDefaults{}, false, nil, nil)
		tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
			{Name: "snapshot", Description: "snapshot", InputSchema: json.RawMessage(`{"type":"object"}`)},
			{Name: "status", Description: "status", InputSchema: json.RawMessage(`{"type":"object"}`)},
		})
		require.NoError(t, err)
		req := providers.PredictionRequest{MaxTokens: 64, Messages: msgs}
		if stream {
			ch, streamErr := tp.PredictStreamWithTools(context.Background(), req, tools, "auto")
			require.NoError(t, streamErr)
			for range ch { //nolint:revive // drain
			}
		} else {
			_, _, err = tp.PredictWithTools(context.Background(), req, tools, "auto")
			require.NoError(t, err)
		}
		srv.Close()
		key := "unary"
		if stream {
			key = "stream"
		}
		raw, _ := json.Marshal(body["messages"])
		var parsed []map[string]any
		require.NoError(t, json.Unmarshal(raw, &parsed))
		out[key] = parsed
	}
	return out
}

// imageRound is a round in which the model called snapshot (which returned
// text and an image) and status (which returned text).
func imageRound() []types.Message {
	snap := &types.MessageToolResult{ID: "call-1", Name: "snapshot", Parts: []types.ContentPart{
		types.NewTextPart("camera 3"),
		types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil),
	}}
	status := types.NewTextToolResult("call-2", "status", "all clear")
	return []types.Message{
		{Role: "user", Content: "check the yard"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "call-1", Name: "snapshot", Args: json.RawMessage(`{}`)},
			{ID: "call-2", Name: "status", Args: json.RawMessage(`{}`)},
		}},
		{Role: "tool", ToolResult: snap},
		{Role: "tool", ToolResult: &status},
	}
}

// OpenAI accepts images only in user messages ("Image URLs are only allowed
// for messages with role 'user'"), so an image a tool returned reaches the
// model in a user message after the round's tool results. The tool message
// keeps the text and says where the image went.
func TestChatToolResultImage_ReachesTheModelInAUserMessage(t *testing.T) {
	for mode, messages := range chatRequestsOf(t, imageRound()) {
		t.Run(mode, func(t *testing.T) {
			require.Len(t, messages, 5, "user, assistant, two tool results, then the image")
			assert.Equal(t, []any{"user", "assistant", "tool", "tool", "user"}, []any{
				messages[0]["role"], messages[1]["role"], messages[2]["role"], messages[3]["role"], messages[4]["role"],
			})

			assert.Equal(t, "call-1", messages[2]["tool_call_id"])
			assert.Equal(t,
				"camera 3\n[The image this tool returned is attached in the next user message.]",
				messages[2]["content"], "the tool message carries text only")
			assert.Equal(t, "all clear", messages[3]["content"])

			parts, ok := messages[4]["content"].([]any)
			require.True(t, ok, "the lifted message carries content parts")
			require.Len(t, parts, 2)
			label := parts[0].(map[string]any)
			assert.Equal(t, "text", label["type"])
			assert.Equal(t, "Media returned by tool snapshot (call call-1):", label["text"])
			image := parts[1].(map[string]any)
			assert.Equal(t, "image_url", image["type"])
			assert.Equal(t, "data:image/png;base64,aW1n", image["image_url"].(map[string]any)["url"])
		})
	}
}

// A round whose results are text only is sent as it was: no extra message.
func TestChatToolResultText_AddsNoMessage(t *testing.T) {
	round := imageRound()
	text := types.NewTextToolResult("call-1", "snapshot", "camera offline")
	round[2] = types.Message{Role: "tool", ToolResult: &text}
	for mode, messages := range chatRequestsOf(t, round) {
		t.Run(mode, func(t *testing.T) {
			require.Len(t, messages, 4)
			assert.Equal(t, "camera offline", messages[2]["content"])
			assert.Equal(t, "tool", messages[3]["role"])
		})
	}
}

// Media a user message cannot carry either (here a video, which OpenAI does
// not accept as input) is described in the tool message, naming where it is
// stored, instead of being dropped without a word.
func TestChatToolResultVideo_IsDescribedNotDropped(t *testing.T) {
	round := imageRound()
	clip := &types.MessageToolResult{ID: "call-1", Name: "snapshot", Parts: []types.ContentPart{
		types.NewTextPart("camera 3"),
		types.NewVideoPartFromStorageRef("media/clip-7", "video/mp4"),
	}}
	round[2] = types.Message{Role: "tool", ToolResult: clip}
	for mode, messages := range chatRequestsOf(t, round) {
		t.Run(mode, func(t *testing.T) {
			require.Len(t, messages, 4, "nothing to attach, so no extra message")
			assert.Equal(t,
				"camera 3\n[This tool returned a video (video/mp4, stored as media/clip-7) that was not sent: "+
					"OpenAI Chat Completions does not accept video input.]",
				messages[2]["content"])
		})
	}
}

// In history, the image follows its own round, before the model's reply to
// it: a later turn does not move it.
func TestChatToolResultImage_StaysWithItsRoundInHistory(t *testing.T) {
	round := append(imageRound(),
		types.Message{Role: "assistant", Content: "The yard is clear."},
		types.Message{Role: "user", Content: "thanks"},
	)
	for mode, messages := range chatRequestsOf(t, round) {
		t.Run(mode, func(t *testing.T) {
			roles := make([]any, len(messages))
			for i, m := range messages {
				roles[i] = m["role"]
			}
			assert.Equal(t, []any{"user", "assistant", "tool", "tool", "user", "assistant", "user"}, roles)
			assert.Equal(t, "The yard is clear.", messages[5]["content"])
		})
	}
}
