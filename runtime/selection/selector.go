// Package selection defines the externalized selector interface that
// narrows a candidate set of skills or tools down to the items most
// relevant for the current turn.
//
// PromptKit owns pack semantics (eligibility, query derivation, when to
// select, fallback on error). A Selector owns the ranking algorithm:
// it may use embeddings, a rerank service, BM25, an LLM judge, or plain
// rules. PromptKit hands it a query and candidates and receives IDs.
// No vectors or scores cross the interface boundary.
package selection

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Candidate describes one item a Selector may choose to surface.
// IDs are stable identifiers (skill name, tool name); the rest is
// descriptive context for the ranker.
type Candidate struct {
	ID          string
	Name        string
	Description string
	Metadata    map[string]string
}

// Query carries the context PromptKit has assembled for this
// selection round. Kind distinguishes skills from tools so one
// Selector implementation can serve both hook points.
type Query struct {
	Text    string
	Kind    string // "skill" | "tool"
	PackID  string
	AgentID string
	K       int // desired max results; selector may return fewer
}

// SelectorContext is handed to Init. It carries shared infrastructure
// selectors may opt into — most notably the configured embedding
// provider, so in-process selectors can reuse the same instance RAG
// uses instead of constructing their own. Any field may be nil.
type SelectorContext struct {
	Embeddings providers.EmbeddingProvider
}

// Selector narrows a candidate set. Returning an error or an empty
// result tells PromptKit to fall back to "include all eligible";
// Selectors must never panic. Implementations should be safe for
// concurrent use.
type Selector interface {
	Name() string
	Init(ctx SelectorContext) error
	Select(ctx context.Context, q Query, candidates []Candidate) ([]string, error)
}
