package stages

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionInstructionStage_AppendsToSystemPrompt(t *testing.T) {
	s := NewCompletionInstructionStage("\nPlease finish now.")

	elem := stage.StreamElement{
		Metadata: map[string]interface{}{
			"system_prompt": "You are a helpful assistant.",
		},
	}

	results := runStage(t, s, []stage.StreamElement{elem})
	require.Len(t, results, 1)
	assert.Equal(t, "You are a helpful assistant.\nPlease finish now.", results[0].Metadata["system_prompt"])
}

func TestCompletionInstructionStage_NoSystemPromptKey(t *testing.T) {
	s := NewCompletionInstructionStage("\nDone.")

	elem := stage.StreamElement{
		Metadata: map[string]interface{}{
			"other_key": "value",
		},
	}

	results := runStage(t, s, []stage.StreamElement{elem})
	require.Len(t, results, 1)
	assert.Equal(t, "value", results[0].Metadata["other_key"])
	assert.Nil(t, results[0].Metadata["system_prompt"])
}

func TestCompletionInstructionStage_NilMetadata(t *testing.T) {
	s := NewCompletionInstructionStage("\nDone.")

	elem := stage.StreamElement{
		Metadata: nil,
	}

	results := runStage(t, s, []stage.StreamElement{elem})
	require.Len(t, results, 1)
	assert.Nil(t, results[0].Metadata)
}

func TestCompletionInstructionStage_ContextCancellation(t *testing.T) {
	s := NewCompletionInstructionStage("\nDone.")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	input := make(chan stage.StreamElement, 1)
	input <- stage.StreamElement{
		Metadata: map[string]interface{}{
			"system_prompt": "Hello",
		},
	}
	close(input)

	output := make(chan stage.StreamElement) // unbuffered so send blocks

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Process(ctx, input, output)
	}()

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Process to return")
	}
}

func TestCompletionInstructionStage_Constructor(t *testing.T) {
	s := NewCompletionInstructionStage("test instruction")
	assert.Equal(t, "completion_instruction", s.Name())
	assert.Equal(t, stage.StageTypeTransform, s.Type())
}

func TestCompletionInstructionStage_TurnStateAppendsOnce(t *testing.T) {
	// With TurnState, the stage appends exactly once per Turn — even across
	// multiple elements that all carry a system_prompt. The shared
	// SystemPrompt becomes the source of truth and propagates onto every
	// element's bag for back-compat readers.
	turnState := stage.NewTurnState()
	turnState.SystemPrompt = "You are a helpful assistant."
	s := NewCompletionInstructionStageWithTurnState("\nPlease finish now.", turnState)

	inputs := []stage.StreamElement{
		{Metadata: map[string]interface{}{"system_prompt": "stale-bag-value"}},
		{Metadata: map[string]interface{}{"system_prompt": "another-stale-value"}},
		{Metadata: map[string]interface{}{"other": "no system_prompt"}},
	}
	results := runStage(t, s, inputs)
	require.Len(t, results, 3)

	expected := "You are a helpful assistant.\nPlease finish now."
	assert.Equal(t, expected, turnState.SystemPrompt, "TurnState should hold the appended prompt exactly once")
	assert.Equal(t, expected, results[0].Metadata["system_prompt"])
	assert.Equal(t, expected, results[1].Metadata["system_prompt"],
		"second element's bag should mirror the appended TurnState prompt — no double-append")
	assert.Equal(t, expected, results[2].Metadata["system_prompt"],
		"element without prior system_prompt still gets the propagated value")
}

func TestCompletionInstructionStage_TurnStateEmptyFallsBackToBag(t *testing.T) {
	// When TurnState.SystemPrompt is empty (legacy callers wiring TurnState
	// but not having TemplateStage populate it), the stage seeds from the
	// first element's bag and writes the appended value back.
	turnState := stage.NewTurnState()
	s := NewCompletionInstructionStageWithTurnState(" END", turnState)

	inputs := []stage.StreamElement{
		{Metadata: map[string]interface{}{"system_prompt": "First"}},
	}
	results := runStage(t, s, inputs)
	require.Len(t, results, 1)
	assert.Equal(t, "First END", turnState.SystemPrompt)
	assert.Equal(t, "First END", results[0].Metadata["system_prompt"])
}

func TestCompletionInstructionStage_MultipleElements(t *testing.T) {
	s := NewCompletionInstructionStage(" END")

	inputs := []stage.StreamElement{
		{Metadata: map[string]interface{}{"system_prompt": "First"}},
		{Metadata: map[string]interface{}{"system_prompt": "Second"}},
		{Metadata: map[string]interface{}{"other": "no change"}},
	}

	results := runStage(t, s, inputs)
	require.Len(t, results, 3)
	assert.Equal(t, "First END", results[0].Metadata["system_prompt"])
	assert.Equal(t, "Second END", results[1].Metadata["system_prompt"])
	assert.Equal(t, "no change", results[2].Metadata["other"])
}
