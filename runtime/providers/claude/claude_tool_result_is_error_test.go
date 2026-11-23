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

// A tool result that carries an error is sent with is_error: true, which
// tells Claude the call failed
// (https://platform.claude.com/docs/en/agents-and-tools/tool-use/handle-tool-calls).
// A result without one has no is_error.
func TestClaudeToolProvider_ErroredToolResultIsMarked(t *testing.T) {
	var body struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
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
		Name: "lookup_order", Description: "Look up an order", InputSchema: json.RawMessage(`{"type":"object"}`),
	}})
	require.NoError(t, err)
	refused := types.NewTextToolResult("c1", "refund", "Tool refund was not run: it is not available this turn")
	refused.Error = "Tool refund was not run: it is not available this turn"
	history := []types.Message{
		{Role: "user", Content: "refund ORD-1 and look it up"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "refund", Args: json.RawMessage(`{}`)},
			{ID: "c2", Name: "lookup_order", Args: json.RawMessage(`{}`)},
		}},
		types.NewToolResultMessage(refused),
		types.NewToolResultMessage(types.NewTextToolResult("c2", "lookup_order", "shipped")),
	}

	_, _, err = provider.PredictWithTools(context.Background(), providers.PredictionRequest{Messages: history}, tools, "auto")
	require.NoError(t, err)

	require.Len(t, body.Messages, 3)
	var results []map[string]any
	require.NoError(t, json.Unmarshal(body.Messages[2].Content, &results))
	require.Len(t, results, 2)
	assert.Equal(t, "c1", results[0]["tool_use_id"])
	assert.Equal(t, true, results[0]["is_error"])
	assert.Equal(t, "c2", results[1]["tool_use_id"])
	assert.NotContains(t, results[1], "is_error")
}
