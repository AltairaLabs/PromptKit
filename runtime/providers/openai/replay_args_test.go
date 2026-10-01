package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A model that answers a zero-argument tool with arguments "{" (seen from
// qwen3-coder on Bedrock's OpenAI-compatible endpoint) must not poison the
// conversation: replayed history has to carry valid JSON, or a strict server
// rejects every later request (#2103). Valid arguments replay unchanged.
func TestReplayArgs_InvalidJSONReplaysAsEmptyObject(t *testing.T) {
	calls := []types.MessageToolCall{
		{ID: "bad", Name: "catalog_overview", Args: json.RawMessage(`{`)},
		{ID: "empty", Name: "catalog_overview"},
		{ID: "good", Name: "lookup", Args: json.RawMessage(`{"id":"ORD-1"}`)},
	}

	t.Run("chat completions", func(t *testing.T) {
		out := (&ToolProvider{}).convertToolCallsToOpenAI(calls)
		require.Len(t, out, 3)
		args := func(i int) string {
			return out[i]["function"].(map[string]interface{})["arguments"].(string)
		}
		assert.Equal(t, "{}", args(0))
		assert.Equal(t, "{}", args(1))
		assert.JSONEq(t, `{"id":"ORD-1"}`, args(2))
	})

	t.Run("responses API", func(t *testing.T) {
		items := (&Provider{}).assistantToolCallItems(&types.Message{Role: "assistant", ToolCalls: calls})
		require.Len(t, items, 3)
		assert.Equal(t, "{}", items[0][keyArguments])
		assert.Equal(t, "{}", items[1][keyArguments])
		assert.JSONEq(t, `{"id":"ORD-1"}`, items[2][keyArguments].(string))
	})
}
