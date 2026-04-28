package stages

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// turnStateWithValidators is a shorthand for the test setup that wires a
// fresh TurnState with the supplied validator configs.
func turnStateWithValidators(configs ...prompt.ValidatorConfig) *stage.TurnState {
	ts := stage.NewTurnState()
	ts.Validators = configs
	return ts
}

func TestGuardrailEvalStage_BannedWordsTriggers(t *testing.T) {
	ts := turnStateWithValidators(prompt.ValidatorConfig{
		Type:   "banned_words",
		Params: map[string]interface{}{"words": []any{"forbidden"}},
	})
	s := NewGuardrailEvalStageWithTurnState(ts)

	elem := newTestMessageElement("assistant", "This is forbidden content.")

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
	ts := turnStateWithValidators(prompt.ValidatorConfig{
		Type:   "banned_words",
		Params: map[string]interface{}{"words": []any{"forbidden"}},
	})
	s := NewGuardrailEvalStageWithTurnState(ts)

	elem := newTestMessageElement("assistant", "This is perfectly fine.")

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	require.Len(t, msg.Validations, 1)
	assert.Equal(t, "banned_words", msg.Validations[0].ValidatorType)
	assert.True(t, msg.Validations[0].Passed)
}

func TestGuardrailEvalStage_NoValidatorConfigs(t *testing.T) {
	s := NewGuardrailEvalStageWithTurnState(stage.NewTurnState())

	elem := newTestMessageElement("assistant", "Hello world")

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Message.Validations)
}

func TestGuardrailEvalStage_UnknownTypeSkipped(t *testing.T) {
	ts := turnStateWithValidators(prompt.ValidatorConfig{
		Type:   "nonexistent_guardrail",
		Params: map[string]interface{}{},
	})
	s := NewGuardrailEvalStageWithTurnState(ts)

	elem := newTestMessageElement("assistant", "Hello world")

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Message.Validations)
}

func TestGuardrailEvalStage_MultipleValidators(t *testing.T) {
	ts := turnStateWithValidators(
		prompt.ValidatorConfig{Type: "banned_words", Params: map[string]interface{}{"words": []any{"forbidden"}}},
		prompt.ValidatorConfig{Type: "length", Params: map[string]interface{}{"max_characters": 20}},
	)
	s := NewGuardrailEvalStageWithTurnState(ts)

	elem := newTestMessageElement("assistant", "This is forbidden content that is way too long for the limit.")

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
	ts := turnStateWithValidators(prompt.ValidatorConfig{
		Type:   "banned_words",
		Params: map[string]interface{}{"words": []any{"forbidden"}},
	})
	s := NewGuardrailEvalStageWithTurnState(ts)

	userElem := newTestMessageElement("user", "Hello")
	assistantElem1 := newTestMessageElement("assistant", "First response")
	assistantElem2 := newTestMessageElement("assistant", "This is forbidden")

	results := runStage(t, s, []stage.StreamElement{userElem, assistantElem1, assistantElem2})

	require.Len(t, results, 3)

	// First assistant should have no validations
	assert.Empty(t, results[1].Message.Validations)

	// Last assistant (index 2) should have validations
	require.Len(t, results[2].Message.Validations, 1)
	assert.False(t, results[2].Message.Validations[0].Passed)
}

func TestGuardrailEvalStage_NoAssistantMessage(t *testing.T) {
	ts := turnStateWithValidators(prompt.ValidatorConfig{
		Type:   "banned_words",
		Params: map[string]interface{}{"words": []any{"test"}},
	})
	s := NewGuardrailEvalStageWithTurnState(ts)

	elem := newTestMessageElement("user", "Hello")

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Message.Validations)
}

func TestGuardrailEvalStage_EnforcesLengthTruncation(t *testing.T) {
	ts := turnStateWithValidators(prompt.ValidatorConfig{
		Type:   "length",
		Params: map[string]interface{}{"max_characters": 20},
	})
	s := NewGuardrailEvalStageWithTurnState(ts)

	longContent := "This is a very long response that exceeds the maximum length limit."
	elem := newTestMessageElement("assistant", longContent)

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	assert.Len(t, msg.Content, 20, "content should be truncated to max_characters")
	assert.Equal(t, longContent[:20], msg.Content)

	require.Len(t, msg.Validations, 1)
	assert.False(t, msg.Validations[0].Passed)
}

func TestGuardrailEvalStage_EnforcesContentBlock(t *testing.T) {
	ts := turnStateWithValidators(prompt.ValidatorConfig{
		Type:   "banned_words",
		Params: map[string]interface{}{"words": []any{"forbidden"}},
	})
	s := NewGuardrailEvalStageWithTurnState(ts)

	elem := newTestMessageElement("assistant", "This contains forbidden words.")

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	assert.Equal(t, prompt.DefaultBlockedMessage, msg.Content)
}

func TestGuardrailEvalStage_EnforcesContentBlockCustomMessage(t *testing.T) {
	customMsg := "This response has been removed."
	ts := turnStateWithValidators(prompt.ValidatorConfig{
		Type:    "banned_words",
		Params:  map[string]interface{}{"words": []any{"forbidden"}},
		Message: customMsg,
	})
	s := NewGuardrailEvalStageWithTurnState(ts)

	elem := newTestMessageElement("assistant", "This contains forbidden words.")

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	assert.Equal(t, customMsg, msg.Content)
}

func TestGuardrailEvalStage_FailOnViolationFalseSkipsEnforcement(t *testing.T) {
	failOnViolation := false
	ts := turnStateWithValidators(prompt.ValidatorConfig{
		Type:            "length",
		Params:          map[string]interface{}{"max_characters": 10},
		FailOnViolation: &failOnViolation,
	})
	s := NewGuardrailEvalStageWithTurnState(ts)

	originalContent := "This is a very long response that exceeds the limit."
	elem := newTestMessageElement("assistant", originalContent)

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	msg := results[0].Message
	assert.Equal(t, originalContent, msg.Content, "content should not be modified when FailOnViolation is false")

	require.Len(t, msg.Validations, 1)
	assert.False(t, msg.Validations[0].Passed, "validation should still record failure")
}

func TestGuardrailEvalStage_PassedValidationIncludesConfig(t *testing.T) {
	ts := turnStateWithValidators(prompt.ValidatorConfig{
		Type:   "banned_words",
		Params: map[string]interface{}{"words": []any{"forbidden"}},
	})
	s := NewGuardrailEvalStageWithTurnState(ts)

	elem := newTestMessageElement("assistant", "Clean content")

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	require.Len(t, results[0].Message.Validations, 1)

	vr := results[0].Message.Validations[0]
	assert.True(t, vr.Passed)
	assert.NotNil(t, vr.Details)
	assert.NotNil(t, vr.Details["config"], "passing validators should include config")
	assert.False(t, vr.Timestamp.IsZero())
}
