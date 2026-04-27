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

	inputs := []stage.StreamElement{
		{Metadata: map[string]interface{}{"system_prompt": "stale-1"}},
		{Metadata: map[string]interface{}{"system_prompt": "stale-2"}},
	}
	results := runStage(t, s, inputs)
	require.Len(t, results, 2)

	expected := "Base prompt.\n\n# Skills\nFoo"
	assert.Equal(t, expected, turnState.SystemPrompt, "appended exactly once into TurnState")
	assert.Equal(t, expected, results[0].Metadata["system_prompt"])
	assert.Equal(t, expected, results[1].Metadata["system_prompt"], "no double-append on second element")
}

func TestSkillInstructionStage_AppendsToSystemPrompt(t *testing.T) {
	s := NewSkillInstructionStage("\n\n# Active Skills\n\n## memory-protocol\n\nCall memory__recall first.\n")

	elem := stage.StreamElement{
		Metadata: map[string]interface{}{
			"system_prompt": "You are a helpful assistant.",
		},
	}

	results := runStage(t, s, []stage.StreamElement{elem})
	require.Len(t, results, 1)
	assert.Equal(t,
		"You are a helpful assistant.\n\n# Active Skills\n\n## memory-protocol\n\nCall memory__recall first.\n",
		results[0].Metadata["system_prompt"])
}

func TestSkillInstructionStage_EmptyInstructionsNoOp(t *testing.T) {
	s := NewSkillInstructionStage("")

	elem := stage.StreamElement{
		Metadata: map[string]interface{}{
			"system_prompt": "You are helpful.",
		},
	}

	results := runStage(t, s, []stage.StreamElement{elem})
	require.Len(t, results, 1)
	assert.Equal(t, "You are helpful.", results[0].Metadata["system_prompt"])
}

func TestSkillInstructionStage_NoSystemPromptKey(t *testing.T) {
	s := NewSkillInstructionStage("\nskill instructions")

	elem := stage.StreamElement{
		Metadata: map[string]interface{}{
			"other_key": "value",
		},
	}

	results := runStage(t, s, []stage.StreamElement{elem})
	require.Len(t, results, 1)
	assert.Nil(t, results[0].Metadata["system_prompt"])
}

func TestSkillInstructionStage_NilMetadata(t *testing.T) {
	s := NewSkillInstructionStage("\nskill instructions")

	elem := stage.StreamElement{Metadata: nil}

	results := runStage(t, s, []stage.StreamElement{elem})
	require.Len(t, results, 1)
	assert.Nil(t, results[0].Metadata)
}
