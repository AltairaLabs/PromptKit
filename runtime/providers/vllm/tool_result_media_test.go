package vllm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// vLLM's tool messages carry text only, so a tool's image is described in the
// tool message instead of vanishing from it, on every request path.
func TestToolResultMedia_IsDescribedNotDropped(t *testing.T) {
	result := &types.MessageToolResult{ID: "call-1", Name: "describe", Parts: []types.ContentPart{
		types.NewTextPart("camera 3"),
		types.NewImagePartFromURL(imageTestURL, nil),
	}}
	for _, path := range imageRequestPaths {
		t.Run(path.name, func(t *testing.T) {
			p, captured := newSpecProviderWithCapture(t)
			err := path.send(t.Context(), p, providers.PredictionRequest{Messages: []types.Message{
				{Role: "user", Content: "look"},
				{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "call-1", Name: "describe", Args: json.RawMessage(`{}`)}}},
				{Role: "tool", ToolResult: result},
			}})
			require.NoError(t, err)
			msgs := captured()["messages"].([]any)
			tool := msgs[len(msgs)-1].(map[string]any)
			assert.Equal(t, "tool", tool["role"])
			assert.Equal(t, "camera 3\n[This tool returned an image (image/png, at https://example.com/cat.png) "+
				"that was not sent: vLLM does not send media in a tool result.]", tool["content"])
		})
	}
}
