package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// processInstructionStage is the shared pipeline body for stages that append a
// fixed instruction string to the system prompt
// (CompletionInstructionStage, SkillInstructionStage). It supports two paths:
//
//  1. With a *TurnState wired: the instruction is appended exactly once per
//     Turn into TurnState.SystemPrompt, regardless of how many elements flow
//     through. Subsequent elements just propagate the appended value back to
//     their own metadata bag for any back-compat readers that haven't
//     migrated.
//  2. Without TurnState (legacy callers): per-element bag append on every
//     element that has a system_prompt key. This preserves pre-#1052
//     behavior for callers that haven't yet wired TurnState into their
//     pipeline builder.
//
// An empty instruction string short-circuits to a pure passthrough — no
// element is mutated.
//
//nolint:lll // Channel signature cannot be shortened
func processInstructionStage(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement, instruction string, turnState *stage.TurnState) error {
	defer close(output)

	if instruction == "" {
		return forwardAllInstructionElements(ctx, input, output)
	}

	if turnState != nil {
		return appendInstructionViaTurnState(ctx, input, output, instruction, turnState)
	}

	return appendInstructionViaBag(ctx, input, output, instruction)
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

// appendInstructionViaBag is the legacy per-element bag-append path. Each
// element with a system_prompt key gets the instruction appended to its bag
// value. Used only when TurnState is not wired.
func appendInstructionViaBag(
	ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement, instruction string,
) error {
	for elem := range input {
		// Legacy bag path: preserved for back-compat with callers that
		// haven't wired TurnState into their pipeline builder yet.
		//nolint:staticcheck // see comment above
		if elem.Metadata != nil {
			//nolint:staticcheck // see comment above
			if sp, ok := elem.Metadata["system_prompt"].(string); ok {
				//nolint:staticcheck // see comment above
				elem.Metadata["system_prompt"] = sp + instruction
			}
		}
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// appendInstructionViaTurnState appends the instruction exactly once into
// TurnState.SystemPrompt and propagates the appended value onto every
// element's metadata bag (for back-compat readers downstream that haven't yet
// migrated off the bag).
func appendInstructionViaTurnState(
	ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement,
	instruction string, turnState *stage.TurnState,
) error {
	appended := false
	for elem := range input {
		if !appended {
			seedTurnStateSystemPrompt(&elem, instruction, turnState)
			appended = true
		}
		propagateTurnStateSystemPromptToBag(&elem, turnState)

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func seedTurnStateSystemPrompt(elem *stage.StreamElement, instruction string, turnState *stage.TurnState) {
	if turnState.SystemPrompt != "" {
		turnState.SystemPrompt += instruction
		return
	}
	//nolint:staticcheck // Reading the bag for first-time seeding is the explicit back-compat path.
	if elem.Metadata == nil {
		return
	}
	//nolint:staticcheck // see above
	if sp, ok := elem.Metadata["system_prompt"].(string); ok {
		turnState.SystemPrompt = sp + instruction
	}
}

func propagateTurnStateSystemPromptToBag(elem *stage.StreamElement, turnState *stage.TurnState) {
	if turnState.SystemPrompt == "" {
		return
	}
	// Writing system_prompt back to the bag keeps any unmigrated
	// downstream reader in sync; removed when the bag is removed.
	//nolint:staticcheck // see comment above
	if elem.Metadata == nil {
		//nolint:staticcheck // see comment above
		elem.Metadata = make(map[string]interface{})
	}
	//nolint:staticcheck // see comment above
	elem.Metadata["system_prompt"] = turnState.SystemPrompt
}
