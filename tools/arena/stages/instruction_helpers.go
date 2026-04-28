package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// processInstructionStage is the shared pipeline body for stages that append a
// fixed instruction string to the system prompt
// (CompletionInstructionStage, SkillInstructionStage). The instruction is
// appended exactly once per Turn into TurnState.SystemPrompt, regardless of
// how many elements flow through.
//
// An empty instruction string short-circuits to a pure passthrough — no
// element is mutated and TurnState is not modified.
//
//nolint:lll // Channel signature cannot be shortened
func processInstructionStage(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement, instruction string, turnState *stage.TurnState) error {
	defer close(output)

	if instruction == "" || turnState == nil {
		return forwardAllInstructionElements(ctx, input, output)
	}

	return appendInstructionViaTurnState(ctx, input, output, instruction, turnState)
}

func forwardAllInstructionElements(
	ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement,
) error {
	for elem := range input {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// appendInstructionViaTurnState appends the instruction exactly once into
// TurnState.SystemPrompt. Subsequent elements pass through unmodified.
func appendInstructionViaTurnState(
	ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement,
	instruction string, turnState *stage.TurnState,
) error {
	appended := false
	for elem := range input {
		if !appended {
			turnState.SystemPrompt += instruction
			appended = true
		}
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
