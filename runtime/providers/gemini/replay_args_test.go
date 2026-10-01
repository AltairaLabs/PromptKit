package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Gemini takes args as an object. Arguments that are not valid JSON used to
// replay as a raw string, which Gemini rejects; they replay as {} instead
// (#2103). Valid arguments replay unchanged.
func TestToolCallParts_InvalidArgsReplayAsEmptyObject(t *testing.T) {
	parts := toolCallParts([]types.MessageToolCall{
		{ID: "bad", Name: "catalog_overview", Args: json.RawMessage(`{`)},
		{ID: "good", Name: "lookup", Args: json.RawMessage(`{"id":"ORD-1"}`)},
	})
	require.Len(t, parts, 2)
	args := func(i int) any {
		return parts[i].(map[string]any)["functionCall"].(map[string]any)["args"]
	}
	assert.Equal(t, map[string]any{}, args(0))
	assert.Equal(t, map[string]any{"id": "ORD-1"}, args(1))
}
