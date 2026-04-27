package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

func TestNewTurnState_ReturnsZeroValuedStruct(t *testing.T) {
	state := NewTurnState()
	if assert.NotNil(t, state) {
		assert.Nil(t, state.Template)
		assert.Empty(t, state.AllowedTools)
		assert.Empty(t, state.Validators)
		assert.Empty(t, state.Variables)
		assert.Empty(t, state.SystemPrompt)
		assert.Empty(t, state.ConversationID)
		assert.Empty(t, state.UserID)
	}
}

// TestTurnState_FieldsArePerTurn pins the architectural intent — TurnState
// fields persist across element processing within a Turn. A producer stage
// writes once; consumer stages observe the write on subsequent reads.
//
// (This test exercises the struct shape, not pipeline goroutine sync;
// concurrency invariants are tested through TestContract_TemplateStage
// which exercises the real pipeline at history sizes from 0 to 2000.)
func TestTurnState_FieldsArePerTurn(t *testing.T) {
	state := NewTurnState()

	state.Template = &prompt.Template{RawTemplate: "you are {{role}}"}
	state.AllowedTools = []string{"search", "memory__remember"}
	state.Validators = []prompt.ValidatorConfig{{Type: "content_filter"}}
	state.Variables = map[string]string{"role": "assistant"}
	state.SystemPrompt = "you are assistant"
	state.ConversationID = "conv-1"
	state.UserID = "user-1"

	assert.Equal(t, "you are {{role}}", state.Template.RawTemplate)
	assert.ElementsMatch(t, []string{"search", "memory__remember"}, state.AllowedTools)
	assert.Len(t, state.Validators, 1)
	assert.Equal(t, "assistant", state.Variables["role"])
	assert.Equal(t, "you are assistant", state.SystemPrompt)
	assert.Equal(t, "conv-1", state.ConversationID)
	assert.Equal(t, "user-1", state.UserID)
}

func TestElementMetadata_FromHistoryDefaultsFalse(t *testing.T) {
	var m ElementMetadata
	assert.False(t, m.FromHistory)

	m.FromHistory = true
	assert.True(t, m.FromHistory)
}
