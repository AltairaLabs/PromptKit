package ollama

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Arguments that are not valid JSON replay as {} so one malformed call cannot
// fail every later request (#2103). Valid arguments replay unchanged.
func TestConvertToolCallsToOllama_InvalidArgsReplayAsEmptyObject(t *testing.T) {
	out := (&ToolProvider{}).convertToolCallsToOllama([]types.MessageToolCall{
		{ID: "bad", Name: "catalog_overview", Args: json.RawMessage(`{`)},
		{ID: "good", Name: "lookup", Args: json.RawMessage(`{"id":"ORD-1"}`)},
	})
	require.Len(t, out, 2)
	assert.Equal(t, "{}", out[0]["function"].(map[string]any)["arguments"])
	assert.JSONEq(t, `{"id":"ORD-1"}`, out[1]["function"].(map[string]any)["arguments"].(string))
}
