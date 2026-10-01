package vllm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Arguments that are not valid JSON replay as {} so one malformed call cannot
// fail every later request (#2103).
func TestPrepareMessages_InvalidToolArgsReplayAsEmptyObject(t *testing.T) {
	history := toolRoundTripHistory()
	history[1].ToolCalls[0].Args = json.RawMessage(`{`)

	msgs, err := (&Provider{}).prepareMessages(&providers.PredictionRequest{Messages: history})
	require.NoError(t, err)
	assistant := findByRole(t, msgs, "assistant")
	require.Len(t, assistant.ToolCalls, 1)
	assert.Equal(t, "{}", assistant.ToolCalls[0].Function.Arguments)
}
