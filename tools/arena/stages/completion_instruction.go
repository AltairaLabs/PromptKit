package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// CompletionInstructionStage appends natural termination instructions to the
// system prompt assembled by PersonaAssemblyStage. The instruction is appended
// exactly once per Turn — TurnState.SystemPrompt is the source of truth and
// the stage is idempotent across the elements that flow through it.
type CompletionInstructionStage struct {
	stage.BaseStage
	instruction string
	turnState   *stage.TurnState
}

// NewCompletionInstructionStage creates a stage that appends the given
// instruction to the system prompt. Pipelines that have migrated to TurnState
// should use NewCompletionInstructionStageWithTurnState.
func NewCompletionInstructionStage(instruction string) *CompletionInstructionStage {
	return &CompletionInstructionStage{
		BaseStage:   stage.NewBaseStage("completion_instruction", stage.StageTypeTransform),
		instruction: instruction,
	}
}

// NewCompletionInstructionStageWithTurnState creates a stage that reads and
// writes the system prompt through the shared *TurnState.
func NewCompletionInstructionStageWithTurnState(
	instruction string, turnState *stage.TurnState,
) *CompletionInstructionStage {
	return &CompletionInstructionStage{
		BaseStage:   stage.NewBaseStage("completion_instruction", stage.StageTypeTransform),
		instruction: instruction,
		turnState:   turnState,
	}
}

// Process appends the completion instruction to the system prompt.
//
//nolint:lll // Channel signature cannot be shortened
func (s *CompletionInstructionStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	return processInstructionStage(ctx, input, output, s.instruction, s.turnState)
}
