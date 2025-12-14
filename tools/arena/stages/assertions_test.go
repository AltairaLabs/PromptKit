package stages

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArenaAssertionStage_NoAssertions(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewArenaAssertionStage(registry, nil)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Hello"),
		newTestMessageElement("assistant", "Hi there!"),
	}

	results := runStage(t, s, inputs)

	// Should forward all elements unchanged
	require.Len(t, results, 2)
	assert.Equal(t, "Hello", results[0].Message.Content)
	assert.Equal(t, "Hi there!", results[1].Message.Content)
}

func TestArenaAssertionStage_EmptyAssertionConfigs(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewArenaAssertionStage(registry, []assertions.AssertionConfig{})

	inputs := []stage.StreamElement{
		newTestMessageElement("assistant", "Response"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "Response", results[0].Message.Content)
}

func TestArenaAssertionStage_WithPassingAssertion(t *testing.T) {
	registry := validators.NewRegistry()

	// Register a simple validator that always passes
	registry.Register("always_pass", func(params map[string]interface{}) validators.Validator {
		return &alwaysPassValidator{}
	})

	assertionConfigs := []assertions.AssertionConfig{
		{
			Type:    "always_pass",
			Message: "Should always pass",
		},
	}

	s := NewArenaAssertionStage(registry, assertionConfigs)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Hello"),
		newTestMessageElement("assistant", "Hi there!"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 2)

	// Check that assertions were attached to assistant message
	assistantElem := results[1]
	require.NotNil(t, assistantElem.Message.Meta)
	assertionsResult, ok := assistantElem.Message.Meta["assertions"].(map[string]interface{})
	require.True(t, ok)
	assert.True(t, assertionsResult["passed"].(bool))
}

func TestArenaAssertionStage_WithFailingAssertion(t *testing.T) {
	registry := validators.NewRegistry()

	// Register a validator that always fails
	registry.Register("always_fail", func(params map[string]interface{}) validators.Validator {
		return &alwaysFailValidator{}
	})

	assertionConfigs := []assertions.AssertionConfig{
		{
			Type:    "always_fail",
			Message: "Should always fail",
		},
	}

	s := NewArenaAssertionStage(registry, assertionConfigs)

	inputs := []stage.StreamElement{
		newTestMessageElement("assistant", "Response"),
	}

	input := make(chan stage.StreamElement, len(inputs))
	for _, elem := range inputs {
		input <- elem
	}
	close(input)

	output := make(chan stage.StreamElement, 100)
	ctx := context.Background()

	// Process should return error for failed assertion
	err := s.Process(ctx, input, output)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestArenaAssertionStage_NoAssistantMessage(t *testing.T) {
	registry := validators.NewRegistry()
	registry.Register("always_pass", func(params map[string]interface{}) validators.Validator {
		return &alwaysPassValidator{}
	})

	assertionConfigs := []assertions.AssertionConfig{
		{Type: "always_pass"},
	}

	s := NewArenaAssertionStage(registry, assertionConfigs)

	// Only user messages, no assistant
	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Hello"),
		newTestMessageElement("user", "World"),
	}

	results := runStage(t, s, inputs)

	// Should forward all elements (no assertions run without assistant message)
	require.Len(t, results, 2)
}

func TestArenaAssertionStage_UnknownValidatorType(t *testing.T) {
	registry := validators.NewRegistry()
	// Don't register any validators

	assertionConfigs := []assertions.AssertionConfig{
		{
			Type:    "unknown_validator",
			Message: "Unknown",
		},
	}

	s := NewArenaAssertionStage(registry, assertionConfigs)

	inputs := []stage.StreamElement{
		newTestMessageElement("assistant", "Response"),
	}

	// Should not error, just skip unknown validators
	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
}

func TestArenaAssertionStage_ExtractsMessagesFromElements(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewArenaAssertionStage(registry, nil)

	elements := []stage.StreamElement{
		newTestMessageElement("user", "User message"),
		newTestMessageElement("assistant", "Assistant message"),
		{Metadata: map[string]interface{}{"no_message": true}}, // Element without message
	}

	messages := s.extractMessagesFromElements(elements)

	require.Len(t, messages, 2)
	assert.Equal(t, "User message", messages[0].Content)
	assert.Equal(t, "Assistant message", messages[1].Content)
}

func TestArenaAssertionStage_FindLastAssistantElementIndex(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewArenaAssertionStage(registry, nil)

	elements := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("assistant", "Assistant 1"),
		newTestMessageElement("user", "User 2"),
		newTestMessageElement("assistant", "Assistant 2"),
	}

	idx := s.findLastAssistantElementIndex(elements)

	assert.Equal(t, 3, idx) // Last assistant is at index 3
}

func TestArenaAssertionStage_FindLastAssistantElementIndex_NotFound(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewArenaAssertionStage(registry, nil)

	elements := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("user", "User 2"),
	}

	idx := s.findLastAssistantElementIndex(elements)

	assert.Equal(t, -1, idx)
}

func TestArenaAssertionStage_ExtractTurnMessages(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewArenaAssertionStage(registry, nil)

	messages := []types.Message{
		{Role: "user", Content: "User 1", Source: "statestore"},
		{Role: "assistant", Content: "Assistant 1", Source: "statestore"},
		{Role: "user", Content: "User 2", Source: ""},
		{Role: "assistant", Content: "Assistant 2", Source: ""},
	}

	turnMessages := s.extractTurnMessages(messages)

	// Should only include messages not from statestore
	require.Len(t, turnMessages, 2)
	assert.Equal(t, "User 2", turnMessages[0].Content)
	assert.Equal(t, "Assistant 2", turnMessages[1].Content)
}

func TestArenaAssertionStage_BuildValidatorParams(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewArenaAssertionStage(registry, nil)

	configParams := map[string]interface{}{
		"custom_param": "custom_value",
	}
	turnMessages := []types.Message{
		{Role: "assistant", Content: "Response"},
	}
	allMessages := []types.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Response"},
	}
	metadata := map[string]interface{}{
		"test_key": "test_value",
	}

	params := s.buildValidatorParams(configParams, turnMessages, allMessages, metadata)

	assert.Equal(t, "custom_value", params["custom_param"])
	assert.NotNil(t, params["_turn_messages"])
	assert.NotNil(t, params["_execution_context_messages"])
	assert.NotNil(t, params["_metadata"])
	assert.NotNil(t, params["_assistant_message"])
}

func TestArenaAssertionStage_BuildValidatorParams_NoAssistant(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewArenaAssertionStage(registry, nil)

	turnMessages := []types.Message{
		{Role: "user", Content: "Hello"},
	}

	params := s.buildValidatorParams(nil, turnMessages, turnMessages, nil)

	// Should not have assistant message
	_, hasAssistant := params["_assistant_message"]
	assert.False(t, hasAssistant)
}

func TestArenaAssertionStage_AttachResultsToMessage(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewArenaAssertionStage(registry, nil)

	msg := &types.Message{
		Role:    "assistant",
		Content: "Response",
	}

	results := map[string]interface{}{
		"passed": true,
		"total":  1,
	}

	s.attachResultsToMessage(msg, results)

	require.NotNil(t, msg.Meta)
	assert.Equal(t, results, msg.Meta["assertions"])
}

func TestArenaAssertionStage_AttachResultsToMessage_NilMeta(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewArenaAssertionStage(registry, nil)

	msg := &types.Message{
		Role:    "assistant",
		Content: "Response",
		Meta:    nil,
	}

	results := map[string]interface{}{
		"passed": true,
	}

	s.attachResultsToMessage(msg, results)

	require.NotNil(t, msg.Meta)
	assert.Equal(t, results, msg.Meta["assertions"])
}

func TestArenaAssertionStage_WithMetadata(t *testing.T) {
	registry := validators.NewRegistry()

	s := NewArenaAssertionStage(registry, nil)

	elem := newTestMessageElement("assistant", "Response")
	elem.Metadata = map[string]interface{}{
		"custom_key": "custom_value",
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	// Metadata should be preserved
	assert.Equal(t, "custom_value", results[0].Metadata["custom_key"])
}

func TestArenaAssertionStage_MultipleAssertions(t *testing.T) {
	registry := validators.NewRegistry()

	// Register validators
	registry.Register("pass1", func(params map[string]interface{}) validators.Validator {
		return &alwaysPassValidator{}
	})
	registry.Register("pass2", func(params map[string]interface{}) validators.Validator {
		return &alwaysPassValidator{}
	})

	assertionConfigs := []assertions.AssertionConfig{
		{Type: "pass1", Message: "First assertion"},
		{Type: "pass2", Message: "Second assertion"},
	}

	s := NewArenaAssertionStage(registry, assertionConfigs)

	inputs := []stage.StreamElement{
		newTestMessageElement("assistant", "Response"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)

	// Check assertions result
	assertionsResult := results[0].Message.Meta["assertions"].(map[string]interface{})
	assert.True(t, assertionsResult["passed"].(bool))
	assert.Equal(t, 2, assertionsResult["total"])
	assert.Equal(t, 0, assertionsResult["failed"])
}

func TestArenaAssertionStage_FailFastOnFailure(t *testing.T) {
	registry := validators.NewRegistry()

	callCount := 0

	// Register validators - first fails, second should not be called
	registry.Register("fail_first", func(params map[string]interface{}) validators.Validator {
		callCount++
		return &alwaysFailValidator{}
	})
	registry.Register("should_not_run", func(params map[string]interface{}) validators.Validator {
		callCount++
		return &alwaysPassValidator{}
	})

	assertionConfigs := []assertions.AssertionConfig{
		{Type: "fail_first", Message: "Will fail"},
		{Type: "should_not_run", Message: "Should not run"},
	}

	s := NewArenaAssertionStage(registry, assertionConfigs)

	inputs := []stage.StreamElement{
		newTestMessageElement("assistant", "Response"),
	}

	input := make(chan stage.StreamElement, len(inputs))
	for _, elem := range inputs {
		input <- elem
	}
	close(input)

	output := make(chan stage.StreamElement, 100)
	ctx := context.Background()

	_ = s.Process(ctx, input, output)

	// Only the first validator should have been called (fail fast)
	assert.Equal(t, 1, callCount)
}

// Test validators for assertions

type alwaysPassValidator struct{}

func (v *alwaysPassValidator) Validate(content string, params map[string]interface{}) validators.ValidationResult {
	return validators.ValidationResult{
		Passed:  true,
		Details: map[string]interface{}{"status": "passed"},
	}
}

type alwaysFailValidator struct{}

func (v *alwaysFailValidator) Validate(content string, params map[string]interface{}) validators.ValidationResult {
	return validators.ValidationResult{
		Passed:  false,
		Details: map[string]interface{}{"status": "failed", "reason": "always fails"},
	}
}
