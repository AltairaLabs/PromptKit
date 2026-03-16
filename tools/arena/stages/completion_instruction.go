package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// CompletionInstructionStage appends natural termination instructions to the
// system prompt assembled by PersonaAssemblyStage.
type CompletionInstructionStage struct {
	stage.BaseStage
	instruction string
}

// NewCompletionInstructionStage creates a stage that appends the given instruction
// to the "system_prompt" metadata key.
func NewCompletionInstructionStage(instruction string) *CompletionInstructionStage {
	return &CompletionInstructionStage{
		BaseStage:   stage.NewBaseStage("completion_instruction", stage.StageTypeTransform),
		instruction: instruction,
	}
}

// Process appends the completion instruction to the system_prompt metadata.
//
//nolint:lll // Channel signature cannot be shortened
func (s *CompletionInstructionStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	for elem := range input {
		if elem.Metadata != nil {
			if sp, ok := elem.Metadata["system_prompt"].(string); ok {
				elem.Metadata["system_prompt"] = sp + s.instruction
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
