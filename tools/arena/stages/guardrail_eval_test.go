package stages

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGuardrailEvalStage_BannedWordsTriggers(t *testing.T) {
	s := NewGuardrailEvalStage()

	elem := newTestMessageElement("assistant", "This is forbidden content.")
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"forbidden"}}},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	require.Len(t, msg.Validations, 1)
	assert.Equal(t, "banned_words", msg.Validations[0].ValidatorType)
	assert.False(t, msg.Validations[0].Passed)
	assert.Contains(t, msg.Validations[0].Details["reason"], "forbidden")
}

func TestGuardrailEvalStage_CleanResponse(t *testing.T) {
	s := NewGuardrailEvalStage()

	elem := newTestMessageElement("assistant", "This is perfectly fine.")
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"forbidden"}}},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	require.Len(t, msg.Validations, 1)
	assert.Equal(t, "banned_words", msg.Validations[0].ValidatorType)
	assert.True(t, msg.Validations[0].Passed)
}

func TestGuardrailEvalStage_NoValidatorConfigs(t *testing.T) {
	s := NewGuardrailEvalStage()

	elem := newTestMessageElement("assistant", "Hello world")

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Message.Validations)
}

func TestGuardrailEvalStage_UnknownTypeSkipped(t *testing.T) {
	s := NewGuardrailEvalStage()

	elem := newTestMessageElement("assistant", "Hello world")
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "nonexistent_guardrail", Params: map[string]interface{}{}},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Message.Validations)
}

func TestGuardrailEvalStage_MultipleValidators(t *testing.T) {
	s := NewGuardrailEvalStage()

	elem := newTestMessageElement("assistant", "This is forbidden content that is way too long for the limit.")
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"forbidden"}}},
		{Type: "length", Params: map[string]interface{}{"max_characters": 20}},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	require.Len(t, msg.Validations, 2)

	assert.Equal(t, "banned_words", msg.Validations[0].ValidatorType)
	assert.False(t, msg.Validations[0].Passed)

	assert.Equal(t, "length", msg.Validations[1].ValidatorType)
	assert.False(t, msg.Validations[1].Passed)
}

func TestGuardrailEvalStage_LastAssistantMessage(t *testing.T) {
	s := NewGuardrailEvalStage()

	// Multiple messages — only the last assistant should get validations
	userElem := newTestMessageElement("user", "Hello")
	assistantElem1 := newTestMessageElement("assistant", "First response")
	assistantElem2 := newTestMessageElement("assistant", "This is forbidden")
	assistantElem2.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"forbidden"}}},
	}

	results := runStage(t, s, []stage.StreamElement{userElem, assistantElem1, assistantElem2})

	require.Len(t, results, 3)

	// First assistant should have no validations
	assert.Empty(t, results[1].Message.Validations)

	// Last assistant (index 2) should have validations
	require.Len(t, results[2].Message.Validations, 1)
	assert.False(t, results[2].Message.Validations[0].Passed)
}

func TestGuardrailEvalStage_NoAssistantMessage(t *testing.T) {
	s := NewGuardrailEvalStage()

	elem := newTestMessageElement("user", "Hello")
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"test"}}},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Message.Validations)
}

func TestGuardrailEvalStage_PassedValidationNoDetails(t *testing.T) {
	s := NewGuardrailEvalStage()

	elem := newTestMessageElement("assistant", "Clean content")
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"forbidden"}}},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	require.Len(t, results[0].Message.Validations, 1)

	vr := results[0].Message.Validations[0]
	assert.True(t, vr.Passed)
	assert.Nil(t, vr.Details)
	assert.False(t, vr.Timestamp.IsZero())
}
