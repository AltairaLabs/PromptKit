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
