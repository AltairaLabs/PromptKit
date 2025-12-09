package middleware

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/variables"
)

// variableProviderMiddleware resolves variables from providers before template rendering.
type variableProviderMiddleware struct {
	varProviders []variables.Provider
}

// VariableProviderMiddleware creates middleware that resolves variables from providers.
// Variables resolved by providers are merged into ExecutionContext.Variables,
// with provider values overriding existing values for duplicate keys.
//
// This middleware should be placed BEFORE TemplateMiddleware so variables are available
// for template substitution.
//
// Providers that need access to state store should have it injected via constructor
// (e.g., variables.NewStateProvider(store, conversationID)).
func VariableProviderMiddleware(
	varProviders ...variables.Provider,
) pipeline.Middleware {
	return &variableProviderMiddleware{
		varProviders: varProviders,
	}
}

// Process resolves variables from all providers and merges them into the context.
func (m *variableProviderMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Skip if no providers configured
	if len(m.varProviders) == 0 {
		return next()
	}

	// Resolve variables from each provider
	for _, p := range m.varProviders {
		vars, err := p.Provide(execCtx.Context)
		if err != nil {
			return fmt.Errorf("variable provider %s failed: %w", p.Name(), err)
		}

		// Merge provider variables into context (providers override existing values)
		if execCtx.Variables == nil {
			execCtx.Variables = make(map[string]string)
		}
		for k, v := range vars {
			execCtx.Variables[k] = v
		}
	}

	return next()
}

// StreamChunk is a no-op for variable provider middleware.
func (m *variableProviderMiddleware) StreamChunk(
	execCtx *pipeline.ExecutionContext,
	chunk *providers.StreamChunk,
) error {
	return nil
}
