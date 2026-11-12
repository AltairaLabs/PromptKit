package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Ollama's tool messages carry text only, so a tool's image is described in
// the tool message instead of vanishing from it.
func TestToolResultMedia_IsDescribedNotDropped(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()
	tp := NewToolProvider("ollama", "llava", srv.URL, providers.ProviderDefaults{}, false, nil)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name: "snapshot", Description: "snapshot", InputSchema: json.RawMessage(`{"type":"object"}`),
	}})
	require.NoError(t, err)

	_, _, err = tp.PredictWithTools(context.Background(), providers.PredictionRequest{MaxTokens: 64, Messages: []types.Message{
		{Role: "user", Content: "look"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "call-1", Name: "snapshot", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call-1", Name: "snapshot", Parts: []types.ContentPart{
			types.NewTextPart("camera 3"),
			types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil),
		}}},
	}}, tools, "auto")
	require.NoError(t, err)

	msgs := body["messages"].([]any)
	tool := msgs[len(msgs)-1].(map[string]any)
	assert.Equal(t, "tool", tool["role"])
	assert.Equal(t, "camera 3\n[This tool returned an image (image/png) that was not sent: "+
		"Ollama does not send media in a tool result.]", tool["content"])
}
