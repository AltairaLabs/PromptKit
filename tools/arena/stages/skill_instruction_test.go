package stages

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillInstructionStage_TurnStateAppendsOnce(t *testing.T) {
	turnState := stage.NewTurnState()
	turnState.SystemPrompt = "Base prompt."
	s := NewSkillInstructionStageWithTurnState("\n\n# Skills\nFoo", turnState)

	inputs := []stage.StreamElement{{}, {}}
	results := runStage(t, s, inputs)
	require.Len(t, results, 2)

	expected := "Base prompt.\n\n# Skills\nFoo"
	assert.Equal(t, expected, turnState.SystemPrompt, "appended exactly once into TurnState")
}

func TestSkillInstructionStage_AppendsToTurnStateSystemPrompt(t *testing.T) {
	turnState := stage.NewTurnState()
	turnState.SystemPrompt = "You are a helpful assistant."
	s := NewSkillInstructionStageWithTurnState("\n\n# Active Skills\n\n## memory-protocol\n\nCall memory__recall first.\n", turnState)

	results := runStage(t, s, []stage.StreamElement{{}})
	require.Len(t, results, 1)
	assert.Equal(t,
		"You are a helpful assistant.\n\n# Active Skills\n\n## memory-protocol\n\nCall memory__recall first.\n",
		turnState.SystemPrompt)
}

func TestSkillInstructionStage_EmptyInstructionsNoOp(t *testing.T) {
	turnState := stage.NewTurnState()
	turnState.SystemPrompt = "You are helpful."
	s := NewSkillInstructionStageWithTurnState("", turnState)

	results := runStage(t, s, []stage.StreamElement{{}})
	require.Len(t, results, 1)
	assert.Equal(t, "You are helpful.", turnState.SystemPrompt)
}

func TestSkillInstructionStage_NilTurnStateNoOp(t *testing.T) {
	s := NewSkillInstructionStageWithTurnState("\nskill instructions", nil)

	results := runStage(t, s, []stage.StreamElement{{}})
	require.Len(t, results, 1)
}
