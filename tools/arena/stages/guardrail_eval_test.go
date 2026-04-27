package stages

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGuardrailEvalStage_TurnStateTakesPrecedenceOverMetadata(t *testing.T) {
	// When wired with a TurnState that has Validators, the stage uses those
	// configs in preference to the deprecated metadata bag. The metadata
	// fallback path is exercised by all the other tests in this file.
	turnState := stage.NewTurnState()
	turnState.Validators = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"forbidden"}}},
	}
	s := NewGuardrailEvalStageWithTurnState(turnState)

	elem := newTestMessageElement("assistant", "This is forbidden content.")
	// Bag contains a different (incorrect) config; TurnState must win.
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"never_match"}}},
	}

	results := runStage(t, s, []stage.StreamElement{elem})
	require.Len(t, results, 1)
	msg := results[0].Message
	require.Len(t, msg.Validations, 1)
	assert.False(t, msg.Validations[0].Passed, "TurnState validator should fail on the forbidden word")
}

func TestGuardrailEvalStage_TurnStateEmptyFallsBackToMetadata(t *testing.T) {
	// When TurnState is wired but has no Validators, the stage falls back
	// to scanning the metadata bag for back-compat.
	turnState := stage.NewTurnState()
	s := NewGuardrailEvalStageWithTurnState(turnState)

	elem := newTestMessageElement("assistant", "This is forbidden content.")
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"forbidden"}}},
	}

	results := runStage(t, s, []stage.StreamElement{elem})
	require.Len(t, results, 1)
	msg := results[0].Message
	require.Len(t, msg.Validations, 1)
	assert.False(t, msg.Validations[0].Passed)
}

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
	assert.NotNil(t, msg.Validations[0].Details["config"], "failed validators should also include config")
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

func TestGuardrailEvalStage_EnforcesLengthTruncation(t *testing.T) {
	s := NewGuardrailEvalStage()

	longContent := "This is a very long response that exceeds the maximum length limit."
	elem := newTestMessageElement("assistant", longContent)
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "length", Params: map[string]interface{}{"max_characters": 20}},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	assert.Len(t, msg.Content, 20, "content should be truncated to max_characters")
	assert.Equal(t, longContent[:20], msg.Content)

	require.Len(t, msg.Validations, 1)
	assert.False(t, msg.Validations[0].Passed)
}

func TestGuardrailEvalStage_EnforcesContentBlock(t *testing.T) {
	s := NewGuardrailEvalStage()

	elem := newTestMessageElement("assistant", "This contains forbidden words.")
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"forbidden"}}},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	assert.Equal(t, prompt.DefaultBlockedMessage, msg.Content)
}

func TestGuardrailEvalStage_EnforcesContentBlockCustomMessage(t *testing.T) {
	s := NewGuardrailEvalStage()

	customMsg := "This response has been removed."
	elem := newTestMessageElement("assistant", "This contains forbidden words.")
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]interface{}{"words": []any{"forbidden"}}, Message: customMsg},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	assert.Equal(t, customMsg, msg.Content)
}

func TestGuardrailEvalStage_FailOnViolationFalseSkipsEnforcement(t *testing.T) {
	s := NewGuardrailEvalStage()

	originalContent := "This is a very long response that exceeds the limit."
	failOnViolation := false
	elem := newTestMessageElement("assistant", originalContent)
	elem.Metadata["validator_configs"] = []prompt.ValidatorConfig{
		{Type: "length", Params: map[string]interface{}{"max_characters": 10}, FailOnViolation: &failOnViolation},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	assert.Equal(t, originalContent, msg.Content, "content should not be modified when FailOnViolation is false")

	require.Len(t, msg.Validations, 1)
	assert.False(t, msg.Validations[0].Passed, "validation should still record failure")
}

func TestGuardrailEvalStage_PassedValidationIncludesConfig(t *testing.T) {
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
	assert.NotNil(t, vr.Details)
	assert.NotNil(t, vr.Details["config"], "passing validators should include config")
	assert.False(t, vr.Timestamp.IsZero())
}
