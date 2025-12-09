// Package variables provides dynamic variable resolution for prompt templates.
// Variable providers can inject context from external sources (databases, APIs,
// conversation state) before template rendering.
package variables

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// Provider resolves variables dynamically at runtime.
// Variables returned override static variables with the same key.
// Providers are called before template rendering to inject dynamic context.
type Provider interface {
	// Name returns the provider identifier (for logging/debugging)
	Name() string

	// Provide returns variables to inject into template context.
	// Called before each template render.
	// The state parameter may be nil if no conversation state exists.
	Provide(ctx context.Context, state *statestore.ConversationState) (map[string]string, error)
}
