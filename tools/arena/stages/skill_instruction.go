package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// SkillInstructionStage appends preloaded skill instructions to the
// system_prompt metadata so the model sees skills marked preload: true
// from turn 1 without having to call skill__activate.
type SkillInstructionStage struct {
	stage.BaseStage
	instructions string
}

// NewSkillInstructionStage creates a stage that appends the given preloaded
// skill instructions block to the "system_prompt" metadata key.
func NewSkillInstructionStage(instructions string) *SkillInstructionStage {
	return &SkillInstructionStage{
		BaseStage:    stage.NewBaseStage("skill_instruction", stage.StageTypeTransform),
		instructions: instructions,
	}
}

// Process appends the preloaded skill instructions to the system_prompt metadata.
//
//nolint:lll // Channel signature cannot be shortened
func (s *SkillInstructionStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	for elem := range input {
		if elem.Metadata != nil && s.instructions != "" {
			if sp, ok := elem.Metadata["system_prompt"].(string); ok {
				elem.Metadata["system_prompt"] = sp + s.instructions
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
