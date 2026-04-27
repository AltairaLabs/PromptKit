package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// SkillInstructionStage appends preloaded skill instructions to the system
// prompt so the model sees skills marked preload: true from turn 1 without
// having to call skill__activate. The instructions are appended exactly once
// per Turn when wired with a *TurnState.
type SkillInstructionStage struct {
	stage.BaseStage
	instructions string
	turnState    *stage.TurnState
}

// NewSkillInstructionStage creates a stage that appends the given preloaded
// skill instructions block to the system prompt. Pipelines that have migrated
// to TurnState should use NewSkillInstructionStageWithTurnState.
func NewSkillInstructionStage(instructions string) *SkillInstructionStage {
	return &SkillInstructionStage{
		BaseStage:    stage.NewBaseStage("skill_instruction", stage.StageTypeTransform),
		instructions: instructions,
	}
}

// NewSkillInstructionStageWithTurnState creates a stage that reads and writes
// the system prompt through the shared *TurnState.
func NewSkillInstructionStageWithTurnState(
	instructions string, turnState *stage.TurnState,
) *SkillInstructionStage {
	return &SkillInstructionStage{
		BaseStage:    stage.NewBaseStage("skill_instruction", stage.StageTypeTransform),
		instructions: instructions,
		turnState:    turnState,
	}
}

// Process appends the preloaded skill instructions to the system prompt.
//
//nolint:lll // Channel signature cannot be shortened
func (s *SkillInstructionStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	return processInstructionStage(ctx, input, output, s.instructions, s.turnState)
}
