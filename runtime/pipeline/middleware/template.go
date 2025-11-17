package middleware

import (
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// templateMiddleware substitutes variables in the system prompt.
type templateMiddleware struct{}

// TemplateMiddleware substitutes variables in the system prompt.
// It replaces {{variable}} placeholders with values from ExecutionContext.Variables.
func TemplateMiddleware() pipeline.Middleware {
	return &templateMiddleware{}
}

// Process substitutes variable placeholders in the system prompt with values from ExecutionContext.Variables.
// It replaces {{variable}} placeholders and stores the result in ExecutionContext.Prompt.
func (m *templateMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Start with the system prompt
	prompt := execCtx.SystemPrompt

	// Substitute variables
	for varName, varValue := range execCtx.Variables {
		placeholder := "{{" + varName + "}}"
		prompt = strings.ReplaceAll(prompt, placeholder, varValue)
	}

	// Store assembled prompt (used by ProviderMiddleware as the System field)
	execCtx.Prompt = prompt

	// Note: We do NOT add system prompt to Messages array.
	// The ProviderMiddleware will extract it from execCtx.Prompt and pass it
	// to the provider's System field (for Claude) or handle it appropriately
	// for each provider type.

	// Continue to next middleware
	return next()
}

// StreamChunk is a no-op for template middleware as it doesn't process stream chunks.
func (m *templateMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Template middleware doesn't process chunks
	return nil
}
