package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// HistoryTurns keeps user and assistant messages with text, in order, reading
// text from Parts as well as Content.
func TestHistoryTurns(t *testing.T) {
	parts := types.Message{Role: "user"}
	parts.AddTextPart("from parts")
	got := HistoryTurns([]types.Message{
		{Role: "system", Content: "s"},
		parts,
		{Role: "assistant", Content: "reply"},
		{Role: "tool", Content: "result"},
		{Role: "assistant", Content: ""},
		{Role: "user", Content: "last"},
	})
	assert.Equal(t, []HistoryTurn{
		{Role: "user", Text: "from parts"},
		{Role: "assistant", Text: "reply"},
		{Role: "user", Text: "last"},
	}, got)
	assert.Empty(t, HistoryTurns(nil))
}
