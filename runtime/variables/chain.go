package variables

import (
	"context"
	"fmt"
)

// ChainProvider composes multiple providers into a single provider.
// Providers are called in order, with later providers overriding
// variables from earlier providers when keys conflict.
type ChainProvider struct {
	providers []Provider
}

// Chain creates a ChainProvider from multiple providers.
// Providers are called in the order given. Later providers
// override variables from earlier providers.
func Chain(providers ...Provider) *ChainProvider {
	return &ChainProvider{providers: providers}
}

// Name returns the provider identifier.
func (c *ChainProvider) Name() string {
	return "chain"
}

// Provide calls all chained providers and merges their results.
// Returns an error if any provider fails.
func (c *ChainProvider) Provide(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string)

	for _, p := range c.providers {
		vars, err := p.Provide(ctx)
		if err != nil {
			return nil, fmt.Errorf("provider %s failed: %w", p.Name(), err)
		}
		// Merge: later providers override earlier ones
		for k, v := range vars {
			result[k] = v
		}
	}

	return result, nil
}

// Add appends a provider to the chain.
func (c *ChainProvider) Add(p Provider) *ChainProvider {
	c.providers = append(c.providers, p)
	return c
}

// Providers returns the list of providers in the chain.
func (c *ChainProvider) Providers() []Provider {
	return c.providers
}
