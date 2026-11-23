package claude

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

// A tool choice of "none" with tools declared is sent as Claude's own
// {"type":"none"}, which keeps the model from calling any of them
// (https://platform.claude.com/docs/en/agents-and-tools/tool-use/define-tools,
// "none prevents Claude from using any tools"). Omitting tool_choice instead
// leaves the model free to call every tool it was sent.
func TestClaudeToolProvider_ToolChoiceNoneIsSent(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_test", "type": "message", "role": "assistant", "model": "claude-test",
			"content": []map[string]string{{"type": "text", "text": "done"}},
			"usage":   map[string]int{"input_tokens": 10, "output_tokens": 2},
		})
	}))
	defer server.Close()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	provider := NewToolProvider("claude", "claude-test", server.URL, providers.ProviderDefaults{MaxTokens: 100}, false)
	tools, err := provider.BuildTooling([]*providers.ToolDescriptor{{
		Name: "lookup_order", Description: "Look up an order",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}})
	require.NoError(t, err)

	_, _, err = provider.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	}, tools, "none")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"type": "none"}, body["tool_choice"])
	require.Len(t, body["tools"], 1, "the tools are still sent")
}
