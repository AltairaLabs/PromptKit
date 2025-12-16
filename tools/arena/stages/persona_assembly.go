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
// It enriches elements with the persona's assembled prompt and variables.
//
// This stage mirrors the behavior of PromptAssemblyMiddleware but for personas:
// - Uses persona's BuildSystemPrompt() which handles fragment assembly
// - Supports template variable substitution with {{variable}} syntax
// - Injects persona-specific variables (goals, constraints, style)
// - Sets base variables for downstream template stage
type PersonaAssemblyStage struct {
	stage.BaseStage
	persona       *config.UserPersonaPack
	region        string
	baseVariables map[string]string
}

// NewPersonaAssemblyStage creates a new persona assembly stage.
func NewPersonaAssemblyStage(
	persona *config.UserPersonaPack,
	region string,
	baseVariables map[string]string,
) *PersonaAssemblyStage {
	return &PersonaAssemblyStage{
		BaseStage:     stage.NewBaseStage("arena_persona_assembly", stage.StageTypeTransform),
		persona:       persona,
		region:        region,
		baseVariables: baseVariables,
	}
}

// Process assembles the persona prompt and enriches all elements with it.
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

	// Prepare metadata to add to all elements
	personaMetadata := map[string]interface{}{
		"system_prompt":  systemPrompt,
		"base_variables": s.baseVariables,
		"persona_id":     s.persona.ID,
		"region":         s.region,
	}

	// Forward all elements with persona metadata
	for elem := range input {
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}

		// Enrich element with persona metadata
		for key, value := range personaMetadata {
			// Don't overwrite existing metadata
			if _, exists := elem.Metadata[key]; !exists {
				elem.Metadata[key] = value
			}
		}

		// Forward element
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
