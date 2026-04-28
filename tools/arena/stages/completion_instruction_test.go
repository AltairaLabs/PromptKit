package stages

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionInstructionStage_AppendsToTurnStateSystemPrompt(t *testing.T) {
	turnState := stage.NewTurnState()
	turnState.SystemPrompt = "You are a helpful assistant."
	s := NewCompletionInstructionStageWithTurnState("\nPlease finish now.", turnState)

	results := runStage(t, s, []stage.StreamElement{{}})
	require.Len(t, results, 1)
	assert.Equal(t, "You are a helpful assistant.\nPlease finish now.", turnState.SystemPrompt)
}

func TestCompletionInstructionStage_NoTurnState_NoOp(t *testing.T) {
	s := NewCompletionInstructionStageWithTurnState("\nDone.", nil)

	results := runStage(t, s, []stage.StreamElement{{}})
	require.Len(t, results, 1)
}

func TestCompletionInstructionStage_EmptyInstruction_NoOp(t *testing.T) {
	turnState := stage.NewTurnState()
	turnState.SystemPrompt = "Original prompt."
	s := NewCompletionInstructionStageWithTurnState("", turnState)

	results := runStage(t, s, []stage.StreamElement{{}})
	require.Len(t, results, 1)
	assert.Equal(t, "Original prompt.", turnState.SystemPrompt, "empty instruction must not modify TurnState")
}

func TestCompletionInstructionStage_ContextCancellation(t *testing.T) {
	turnState := stage.NewTurnState()
	turnState.SystemPrompt = "Hello"
	s := NewCompletionInstructionStageWithTurnState("\nDone.", turnState)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	input := make(chan stage.StreamElement, 1)
	input <- stage.StreamElement{}
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
	s := NewCompletionInstructionStageWithTurnState("test instruction", stage.NewTurnState())
	assert.Equal(t, "completion_instruction", s.Name())
	assert.Equal(t, stage.StageTypeTransform, s.Type())
}

func TestCompletionInstructionStage_TurnStateAppendsOnce(t *testing.T) {
	// With TurnState, the stage appends exactly once per Turn — even across
	// multiple elements that all flow through. The shared SystemPrompt
	// becomes the source of truth.
	turnState := stage.NewTurnState()
	turnState.SystemPrompt = "You are a helpful assistant."
	s := NewCompletionInstructionStageWithTurnState("\nPlease finish now.", turnState)

	inputs := []stage.StreamElement{{}, {}, {}}
	results := runStage(t, s, inputs)
	require.Len(t, results, 3)

	expected := "You are a helpful assistant.\nPlease finish now."
	assert.Equal(t, expected, turnState.SystemPrompt, "TurnState should hold the appended prompt exactly once")
}

func TestCompletionInstructionStage_TurnStateEmptyPromptStillAppends(t *testing.T) {
	// When TurnState.SystemPrompt is empty (e.g. TemplateStage hasn't run
	// yet), the instruction is still appended.
	turnState := stage.NewTurnState()
	s := NewCompletionInstructionStageWithTurnState(" END", turnState)

	results := runStage(t, s, []stage.StreamElement{{}})
	require.Len(t, results, 1)
	assert.Equal(t, " END", turnState.SystemPrompt)
}
