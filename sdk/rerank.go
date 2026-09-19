package sdk

import (
	"fmt"
	"slices"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// RerankProvider returns the default rerank provider — the first one declared
// via [WithRerankProvider] or a role: rerank provider file.
//
// Reranking has no built-in consumer: nothing in the pipeline calls it, by
// design, because which candidates are worth reranking is the host's decision
// and depends on a retrieval step PromptKit does not own. This accessor is how
// a configured provider is reached.
//
// Returns an error rather than nil when none is configured, so a host that
// meant to configure one finds out here instead of at the call site.
func (c *Conversation) RerankProvider() (providers.RerankProvider, error) {
	if c == nil || c.config == nil || len(c.config.rerankProviderIDs) == 0 {
		return nil, fmt.Errorf("no rerank provider configured: pass sdk.WithRerankProvider(...)")
	}
	return c.config.rerankProviders[c.config.rerankProviderIDs[0]], nil
}

// RerankProviderByID returns a specific rerank provider by its configured ID,
// for hosts running more than one — a cheap reranker for a first pass and an
// accurate one for the survivors, say.
func (c *Conversation) RerankProviderByID(id string) (providers.RerankProvider, error) {
	if c == nil || c.config == nil {
		return nil, fmt.Errorf("rerank provider %q: no providers configured", id)
	}
	rp, ok := c.config.rerankProviders[id]
	if !ok {
		return nil, fmt.Errorf("rerank provider %q not found (configured: %v)",
			id, c.RerankProviderIDs())
	}
	return rp, nil
}

// RerankProviderIDs returns the configured rerank provider IDs in declaration
// order. The first is the one RerankProvider() returns.
func (c *Conversation) RerankProviderIDs() []string {
	if c == nil || c.config == nil {
		return nil
	}
	return slices.Clone(c.config.rerankProviderIDs)
}
