// Package variables provides dynamic variable resolution for prompt templates.
// Variable providers can inject context from external sources (databases, APIs,
// conversation state) before template rendering.
package variables

import (
	"context"
)

// Provider resolves variables dynamically at runtime.
// Variables returned override static variables with the same key.
// Providers are called before template rendering to inject dynamic context.
//
// Providers that need access to conversation state (like StateProvider)
// should receive it via constructor injection rather than through Provide().
type Provider interface {
	// Name returns the provider identifier (for logging/debugging)
	Name() string

	// Provide returns variables to inject into template context.
	// Called before each template render.
	Provide(ctx context.Context) (map[string]string, error)
}
