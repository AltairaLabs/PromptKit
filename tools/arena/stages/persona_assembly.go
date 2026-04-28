package stages

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// PersonaAssemblyStage assembles persona prompts using the same
// fragment/template system as PromptAssemblyStage.
//
// This stage mirrors the behavior of PromptAssemblyMiddleware but for personas:
// - Uses persona's BuildSystemPrompt() which handles fragment assembly
// - Supports template variable substitution with {{variable}} syntax
// - Injects persona-specific variables (goals, constraints, style)
// - Writes the rendered system prompt and base variables into TurnState
type PersonaAssemblyStage struct {
	stage.BaseStage
	persona       *config.UserPersonaPack
	region        string
	baseVariables map[string]string
	turnState     *stage.TurnState
}

// NewPersonaAssemblyStageWithTurnState creates a persona assembly stage that
// writes the rendered persona system prompt into the shared *TurnState.
func NewPersonaAssemblyStageWithTurnState(
	persona *config.UserPersonaPack,
	region string,
	baseVariables map[string]string,
	turnState *stage.TurnState,
) *PersonaAssemblyStage {
	return &PersonaAssemblyStage{
		BaseStage:     stage.NewBaseStage("arena_persona_assembly", stage.StageTypeTransform),
		persona:       persona,
		region:        region,
		baseVariables: baseVariables,
		turnState:     turnState,
	}
}

// Process assembles the persona prompt and writes it into TurnState. All input
// elements are forwarded unchanged.
//
//nolint:lll // Channel signature cannot be shortened
func (s *PersonaAssemblyStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	// Use persona's BuildSystemPrompt which handles:
	// - Fragment assembly (if persona uses fragments)
	// - Template variable substitution
	// - Persona-specific variable injection (goals, constraints, style)
	systemPrompt, err := s.persona.BuildSystemPrompt(s.region, s.baseVariables)
	if err != nil {
		return fmt.Errorf("failed to assemble persona prompt: %w", err)
	}

	logger.Debug("Assembled persona prompt",
		"persona", s.persona.ID,
		"region", s.region,
		"length", len(systemPrompt),
		"base_vars", len(s.baseVariables))

	if s.turnState != nil {
		s.turnState.SystemPrompt = systemPrompt
		if s.turnState.Variables == nil {
			s.turnState.Variables = make(map[string]string, len(s.baseVariables))
		}
		for k, v := range s.baseVariables {
			if _, exists := s.turnState.Variables[k]; !exists {
				s.turnState.Variables[k] = v
			}
		}
	}

	// Forward all input elements unchanged.
	for elem := range input {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
