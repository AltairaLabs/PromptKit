package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestCountUserTurns_CountsOnlyUserMessages(t *testing.T) {
	msgs := []types.Message{
		{Role: "system"}, {Role: "user"}, {Role: "assistant"},
		{Role: "tool"}, {Role: "user"}, {Role: "assistant"}, {Role: "user"},
	}
	assert.Equal(t, 3, CountUserTurns(msgs))
	assert.Equal(t, 0, CountUserTurns(nil))
	assert.Equal(t, 0, CountUserTurns([]types.Message{{Role: "assistant"}}))
}
