package middleware

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// PersonaAssemblyMiddleware assembles persona prompts using the same
// fragment/template system as PromptAssemblyMiddleware.
// Populates execCtx.SystemPrompt with the persona's assembled prompt.
//
// This middleware mirrors the behavior of PromptAssemblyMiddleware but for personas:
// - Uses persona's BuildSystemPrompt() which handles fragment assembly
// - Supports template variable substitution with {{variable}} syntax
// - Injects persona-specific variables (goals, constraints, style)
// - Sets base variables for downstream template middleware
func PersonaAssemblyMiddleware(
	persona *config.UserPersonaPack,
	region string,
	baseVariables map[string]string,
) pipeline.Middleware {
	return &personaAssemblyMiddleware{
		persona:       persona,
		region:        region,
		baseVariables: baseVariables,
	}
}

type personaAssemblyMiddleware struct {
	persona       *config.UserPersonaPack
	region        string
	baseVariables map[string]string
}

func (m *personaAssemblyMiddleware) Process(ctx *pipeline.ExecutionContext, next func() error) error {
	// Use persona's BuildSystemPrompt which handles:
	// - Fragment assembly (if persona uses fragments)
	// - Template variable substitution
	// - Persona-specific variable injection (goals, constraints, style)
	systemPrompt, err := m.persona.BuildSystemPrompt(m.region, m.baseVariables)
	if err != nil {
		return fmt.Errorf("failed to assemble persona prompt: %w", err)
	}

	// Set the system prompt (TemplateMiddleware will process any remaining {{vars}})
	ctx.SystemPrompt = systemPrompt

	// Initialize Variables map with base variables
	if ctx.Variables == nil {
		ctx.Variables = make(map[string]string)
	}
	for k, v := range m.baseVariables {
		if _, exists := ctx.Variables[k]; !exists {
			ctx.Variables[k] = v
		}
	}

	logger.Debug("Assembled persona prompt",
		"persona", m.persona.ID,
		"region", m.region,
		"length", len(systemPrompt),
		"base_vars", len(m.baseVariables))

	return next()
}

func (m *personaAssemblyMiddleware) StreamChunk(ctx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Persona assembly middleware doesn't process chunks
	return nil
}
