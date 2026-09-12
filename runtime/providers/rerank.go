package providers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// RerankRequest asks a provider to order Documents by their relevance to Query.
//
// Unlike embedding, which scores each text on its own and leaves the comparison
// to the caller, a reranker reads the query and a document together. That is
// what makes it more accurate — and why its output cannot be cached per
// document the way a vector can.
type RerankRequest struct {
	// Query is the text documents are ranked against. Required.
	Query string

	// Documents are the candidates to order. The response refers back to
	// them by position, so the caller keeps ownership of whatever richer
	// objects these strings were rendered from.
	Documents []string

	// TopN caps how many results come back. Zero means "all of them",
	// still ordered. A provider may return fewer than TopN but never more.
	TopN int

	// Model overrides the provider's default. Empty uses the default.
	Model string
}

// RankedDocument is one candidate's placing.
type RankedDocument struct {
	// Index is the document's position in the request's Documents slice.
	// This is the field callers key on: the results are reordered, so
	// position in the response says nothing about which document it was.
	Index int

	// Score is the provider's relevance score. Higher is more relevant.
	// The range is provider-specific and NOT comparable across providers
	// or models — use it to order and to threshold within one provider,
	// never to compare two providers' verdicts.
	Score float64

	// Document echoes the input text when the provider returns it. It may
	// be empty even for a valid result; Index is the reliable identifier.
	Document string
}

// RerankResponse holds the reordered candidates.
type RerankResponse struct {
	// Results are ordered best-first and contain at most TopN entries.
	// Documents the provider dropped are simply absent.
	Results []RankedDocument

	// Model is the model that actually ran, which may differ from the
	// requested one when the provider substitutes.
	Model string

	// Usage reports token consumption when the provider supplies it.
	Usage *RerankUsage
}

// RerankUsage tracks what a rerank call cost.
type RerankUsage struct {
	// TotalTokens counts query and documents together. Rerankers bill on
	// the combined input; there is no output to bill for.
	TotalTokens int
}

// RerankProvider orders a bounded candidate list by relevance to a query.
//
// It is deliberately not a tool and not an agent: reranking is a synchronous
// model-backed function with no conversation, no tool loop and no state. Hosts
// call it directly, typically as an optional stage after a vector search has
// produced more candidates than the prompt can afford to carry.
//
// Implementations may be hosted APIs (Voyage AI, Cohere), a local
// cross-encoder, or an LLM-based scorer, and callers should not need to know
// which. See AltairaLabs/PromptKit#1993.
type RerankProvider interface {
	// Provider supplies lifecycle and health: Name, Type, Pricing,
	// Validate, Init, HealthCheck, Close.
	base.Provider

	// Rerank orders req.Documents by relevance to req.Query. It honors
	// context cancellation and deadlines.
	//
	// An empty Documents slice returns an empty result and no error: having
	// nothing to rank is a normal outcome of an upstream search, not a
	// failure worth propagating.
	Rerank(ctx context.Context, req RerankRequest) (RerankResponse, error)

	// MaxDocuments reports the most candidates one call accepts. Callers
	// that may exceed it should batch; implementations are free to batch
	// internally instead, and say so in their own docs.
	MaxDocuments() int

	// ID returns the configured provider identifier, e.g. "voyageai-rerank".
	// This is the instance's ID from config, not the vendor name — several
	// instances of one vendor can coexist.
	ID() string
}
