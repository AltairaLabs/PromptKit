package provider

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndOfTurnFor_CarriesLastAssistantFinishReason checks the VAD path's turn
// boundary reports why the turn's reply ended.
func TestEndOfTurnFor_CarriesLastAssistantFinishReason(t *testing.T) {
	elem := endOfTurnFor([]types.Message{
		{Role: "assistant", FinishReason: types.FinishReasonToolUse},
		{Role: "tool", Content: "42"},
		{Role: "assistant", Content: "it is 42", FinishReason: types.FinishReasonSafety},
	}, 3)
	require.True(t, elem.EndOfTurn)
	require.NotNil(t, elem.Meta.CallerTurn)
	assert.Equal(t, 3, *elem.Meta.CallerTurn, "the boundary carries the caller turn it ends")
	require.NotNil(t, elem.Meta.FinishReason)
	assert.Equal(t, types.FinishReasonSafety, *elem.Meta.FinishReason)

	assert.Nil(t, endOfTurnFor(nil, 0).Meta.FinishReason, "a turn with no reply reports no reason")
	assert.Nil(t, endOfTurnFor([]types.Message{{Role: "assistant"}}, 0).Meta.FinishReason,
		"an unreported reason stays unset")
}
