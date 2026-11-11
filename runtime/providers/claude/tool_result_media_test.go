package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// toolResultBlockOf sends a round whose snapshot call returned parts and
// returns the tool_result block the request carried, or the error.
func toolResultBlockOf(t *testing.T, parts []types.ContentPart) (map[string]any, error) {
	t.Helper()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg","type":"message","role":"assistant","model":"m",` +
			`"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	tp := NewToolProvider("claude", "claude-opus-5-5", srv.URL, providers.ProviderDefaults{}, false)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "snapshot", Description: "snapshot", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	require.NoError(t, err)
	_, _, err = tp.PredictWithTools(context.Background(), providers.PredictionRequest{MaxTokens: 64, Messages: []types.Message{
		{Role: "user", Content: "check the yard"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "toolu_1", Name: "snapshot", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "toolu_1", Name: "snapshot", Parts: parts}},
	}}, tools, "auto")
	if err != nil {
		return nil, err
	}
	for _, m := range body["messages"].([]any) {
		content, _ := m.(map[string]any)["content"].([]any)
		for _, block := range content {
			if b := block.(map[string]any); b["type"] == "tool_result" {
				return b, nil
			}
		}
	}
	t.Fatalf("no tool_result in %v", body["messages"])
	return nil, nil
}

// An image a tool returned by URL is sent as a url source. Anthropic rejects
// media_type on a url source ("Extra inputs are not permitted"), which the
// tool path used to set.
func TestClaudeToolResult_URLImageHasNoMediaType(t *testing.T) {
	url := "https://example.com/snap.png"
	block, err := toolResultBlockOf(t, []types.ContentPart{
		types.NewTextPart("camera 3"),
		{Type: types.ContentTypeImage, Media: &types.MediaContent{URL: &url, MIMEType: types.MIMETypeImagePNG}},
	})
	require.NoError(t, err)
	content := block["content"].([]any)
	require.Len(t, content, 2)
	assert.Equal(t, map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": url}}, content[1])
}

// Claude's tool results take no audio or video, so a tool's audio is
// described in a text block, naming where it is stored, instead of dropped.
func TestClaudeToolResult_AudioIsDescribed(t *testing.T) {
	block, err := toolResultBlockOf(t, []types.ContentPart{
		types.NewTextPart("recorded"),
		types.NewAudioPartFromStorageRef("media/call-9", types.MIMETypeAudioMP3),
	})
	require.NoError(t, err)
	content := block["content"].([]any)
	require.Len(t, content, 2)
	assert.Equal(t, map[string]any{"type": "text", "text": "[This tool returned an audio (audio/mpeg, stored as " +
		"media/call-9) that was not sent: Claude does not accept audio input.]"}, content[1])
}

// A document in a format Claude does not take, or an image over its size
// limit, is described instead of failing the request: the request is rebuilt
// from history on every later turn, so failing it would end the conversation.
func TestClaudeToolResult_UnacceptedMediaIsDescribed(t *testing.T) {
	cases := map[string]struct {
		part types.ContentPart
		want string
	}{
		"CSV document": {
			types.NewDocumentPartFromData("YSxi", "text/csv"),
			"[This tool returned a document (text/csv) that was not sent: Claude does not accept text/csv documents.]",
		},
		"image over 5 MB": {
			types.NewImagePartFromData(strings.Repeat("A", 7*1024*1024), types.MIMETypeImagePNG, nil),
			"[This tool returned an image (image/png) that was not sent: it is 5.2 MB, over Claude's 5 MB limit for images.]",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			block, err := toolResultBlockOf(t, []types.ContentPart{tc.part})
			require.NoError(t, err)
			content := block["content"].([]any)
			require.Len(t, content, 1)
			assert.Equal(t, map[string]any{"type": "text", "text": tc.want}, content[0])
		})
	}
}
