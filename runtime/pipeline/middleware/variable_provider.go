package middleware

import (
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/variables"
)

// variableProviderMiddleware resolves variables from providers before template rendering.
type variableProviderMiddleware struct {
	varProviders []variables.Provider
	config       *pipeline.StateStoreConfig
}

// VariableProviderMiddleware creates middleware that resolves variables from providers.
// Variables resolved by providers are merged into ExecutionContext.Variables,
// with provider values overriding existing values for duplicate keys.
//
// This middleware should be placed AFTER StateStoreLoadMiddleware (to have access
// to conversation state) and BEFORE TemplateMiddleware (so variables are available
// for template substitution).
//
// The config parameter provides access to the state store for loading conversation state.
// If nil, providers will receive nil state.
func VariableProviderMiddleware(
	config *pipeline.StateStoreConfig,
	varProviders ...variables.Provider,
) pipeline.Middleware {
	return &variableProviderMiddleware{
		varProviders: varProviders,
		config:       config,
	}
}

// Process resolves variables from all providers and merges them into the context.
func (m *variableProviderMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Skip if no providers configured
	if len(m.varProviders) == 0 {
		return next()
	}

	// Load state for providers (if state store is configured)
	state := m.loadState(execCtx)

	// Resolve variables from each provider
	for _, p := range m.varProviders {
		vars, err := p.Provide(execCtx.Context, state)
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

// loadState loads conversation state from the state store if configured.
func (m *variableProviderMiddleware) loadState(
	execCtx *pipeline.ExecutionContext,
) *statestore.ConversationState {
	if m.config == nil || m.config.Store == nil {
		return nil
	}

	store, ok := m.config.Store.(statestore.Store)
	if !ok {
		return nil
	}

	state, err := store.Load(execCtx.Context, m.config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		// Log error but don't fail - providers can work without state
		return nil
	}

	return state
}

// StreamChunk is a no-op for variable provider middleware.
func (m *variableProviderMiddleware) StreamChunk(
	execCtx *pipeline.ExecutionContext,
	chunk *providers.StreamChunk,
) error {
	return nil
}
