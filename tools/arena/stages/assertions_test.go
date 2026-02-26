package stages

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTurnEvalRunner implements TurnEvalRunner for testing.
type mockTurnEvalRunner struct {
	results []evals.EvalResult
}

func (m *mockTurnEvalRunner) RunAssertionsAsEvals(
	_ context.Context,
	assertionConfigs []assertions.AssertionConfig,
	_ []types.Message,
	_ int,
	_ string,
	_ evals.EvalTrigger,
) []evals.EvalResult {
	return m.results
}


func TestArenaAssertionStage_NoAssertions(t *testing.T) {
	s := NewArenaAssertionStage(nil)

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
	s := NewArenaAssertionStage([]assertions.AssertionConfig{})

	inputs := []stage.StreamElement{
		newTestMessageElement("assistant", "Response"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "Response", results[0].Message.Content)
}

func TestArenaAssertionStage_WithPassingAssertion(t *testing.T) {
	assertionConfigs := []assertions.AssertionConfig{
		{
			Type:    "always_pass",
			Message: "Should always pass",
		},
	}

	runner := &mockTurnEvalRunner{
		results: []evals.EvalResult{
			{
				Type:    "always_pass",
				Passed:  true,
				Score:   testutil.Ptr(1.0),
				Message: "Should always pass",
				Details: map[string]any{"status": "passed"},
			},
		},
	}

	s := NewArenaAssertionStage(assertionConfigs).WithTurnEvalRunner(runner, "test-session")

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
	assertionConfigs := []assertions.AssertionConfig{
		{
			Type:    "always_fail",
			Message: "Should always fail",
		},
	}

	runner := &mockTurnEvalRunner{
		results: []evals.EvalResult{
			{
				Type:    "always_fail",
				Passed:  false,
				Score:   testutil.Ptr(0.0),
				Message: "Should always fail",
				Details: map[string]any{"status": "failed", "reason": "always fails"},
			},
		},
	}

	s := NewArenaAssertionStage(assertionConfigs).WithTurnEvalRunner(runner, "test-session")

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
	assertionConfigs := []assertions.AssertionConfig{
		{Type: "always_pass"},
	}

	runner := &mockTurnEvalRunner{
		results: []evals.EvalResult{
			{Type: "always_pass", Passed: true, Score: testutil.Ptr(1.0)},
		},
	}

	s := NewArenaAssertionStage(assertionConfigs).WithTurnEvalRunner(runner, "test-session")

	// Only user messages, no assistant
	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Hello"),
		newTestMessageElement("user", "World"),
	}

	results := runStage(t, s, inputs)

	// Should forward all elements (no assertions run without assistant message)
	require.Len(t, results, 2)
}

func TestArenaAssertionStage_NoTurnEvalRunner(t *testing.T) {
	// When no TurnEvalRunner is set, assertions should be skipped
	assertionConfigs := []assertions.AssertionConfig{
		{
			Type:    "some_validator",
			Message: "Should be skipped",
		},
	}

	s := NewArenaAssertionStage(assertionConfigs)

	inputs := []stage.StreamElement{
		newTestMessageElement("assistant", "Response"),
	}

	// Should not error, assertions are skipped when no runner is set
	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
}

func TestArenaAssertionStage_ExtractsMessagesFromElements(t *testing.T) {
	s := NewArenaAssertionStage(nil)

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
	s := NewArenaAssertionStage(nil)

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
	s := NewArenaAssertionStage(nil)

	elements := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("user", "User 2"),
	}

	idx := s.findLastAssistantElementIndex(elements)

	assert.Equal(t, -1, idx)
}

func TestArenaAssertionStage_ExtractTurnMessages(t *testing.T) {
	s := NewArenaAssertionStage(nil)

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

func TestArenaAssertionStage_BuildWhenParams(t *testing.T) {
	s := NewArenaAssertionStage(nil)

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

	params := s.buildWhenParams(configParams, turnMessages, allMessages, metadata)

	assert.Equal(t, "custom_value", params["custom_param"])
	assert.NotNil(t, params["_turn_messages"])
	assert.NotNil(t, params["_execution_context_messages"])
	assert.NotNil(t, params["_metadata"])
	assert.NotNil(t, params["_assistant_message"])
}

func TestArenaAssertionStage_BuildWhenParams_NoAssistant(t *testing.T) {
	s := NewArenaAssertionStage(nil)

	turnMessages := []types.Message{
		{Role: "user", Content: "Hello"},
	}

	params := s.buildWhenParams(nil, turnMessages, turnMessages, nil)

	// Should not have assistant message
	_, hasAssistant := params["_assistant_message"]
	assert.False(t, hasAssistant)
}

func TestArenaAssertionStage_AttachResultsToMessage(t *testing.T) {
	s := NewArenaAssertionStage(nil)

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
	s := NewArenaAssertionStage(nil)

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
	s := NewArenaAssertionStage(nil)

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
	assertionConfigs := []assertions.AssertionConfig{
		{Type: "pass1", Message: "First assertion"},
		{Type: "pass2", Message: "Second assertion"},
	}

	runner := &mockTurnEvalRunner{
		results: []evals.EvalResult{
			{
				Type:    "pass1",
				Passed:  true,
				Score:   testutil.Ptr(1.0),
				Message: "First assertion",
				Details: map[string]any{"status": "passed"},
			},
			{
				Type:    "pass2",
				Passed:  true,
				Score:   testutil.Ptr(1.0),
				Message: "Second assertion",
				Details: map[string]any{"status": "passed"},
			},
		},
	}

	s := NewArenaAssertionStage(assertionConfigs).WithTurnEvalRunner(runner, "test-session")

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

func TestArenaAssertionStage_AllAssertionsRunOnFailure(t *testing.T) {
	// The eval runner runs all assertions at once (no fail-fast behavior).
	assertionConfigs := []assertions.AssertionConfig{
		{Type: "fail_first", Message: "Will fail"},
		{Type: "also_runs", Message: "Also runs"},
	}

	runner := &mockTurnEvalRunner{
		results: []evals.EvalResult{
			{
				Type:    "fail_first",
				Passed:  false,
				Score:   testutil.Ptr(0.0),
				Message: "Will fail",
				Details: map[string]any{"status": "failed"},
			},
			{
				Type:    "also_runs",
				Passed:  true,
				Score:   testutil.Ptr(1.0),
				Message: "Also runs",
				Details: map[string]any{"status": "passed"},
			},
		},
	}

	s := NewArenaAssertionStage(assertionConfigs).WithTurnEvalRunner(runner, "test-session")

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

	err := s.Process(ctx, input, output)

	// Should still fail because one assertion failed
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	// Collect output elements
	var results []stage.StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	// Should have the original element + error element
	// The first element should have assertions attached with both results
	require.GreaterOrEqual(t, len(results), 1)
	assertionsResult := results[0].Message.Meta["assertions"].(map[string]interface{})
	assert.Equal(t, 2, assertionsResult["total"])
	assert.Equal(t, 1, assertionsResult["failed"])
}
