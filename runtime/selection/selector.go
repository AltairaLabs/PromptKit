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

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Candidate describes one item a Selector may choose to surface.
// IDs are stable identifiers (skill name, tool name); the rest is
// descriptive context for the ranker. The json tags are the exec
// selector's wire keys (see ExecClient).
type Candidate struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	// AllowedTools is a skill candidate's allowed-tools: the tools the skill
	// grants on activation, before the pack's tools cap them. It is empty for
	// a tool candidate, and an exec selector receives it only when set. It
	// shares the registry's slice: treat it, like Metadata, as read-only.
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

// Query carries the context PromptKit has assembled for this
// selection round. Kind distinguishes skills from tools so one
// Selector implementation can serve both hook points. The json tags
// are the exec selector's wire keys (see ExecClient).
type Query struct {
	Text    string `json:"text"`
	Kind    string `json:"kind"` // "skill" | "tool"
	PackID  string `json:"pack_id"`
	AgentID string `json:"agent_id,omitempty"`
	K       int    `json:"k"` // desired max results; selector may return fewer
}

// SelectorContext is handed to Init. It carries shared infrastructure
// selectors may opt into — most notably the configured embedding
// provider, so in-process selectors can reuse the same instance RAG
// uses instead of constructing their own. Any field may be nil.
//
// A selector that needs a field it did not get must fail Init rather
// than degrade silently: Select's fallback is "include all eligible",
// which is indistinguishable from a selector that ran and chose
// everything.
type SelectorContext struct {
	Embeddings providers.EmbeddingProvider

	// Rerank is the configured rerank provider, for selectors that rank
	// by relevance to the turn's query rather than by vector distance.
	// Nil when the host configured none.
	Rerank providers.RerankProvider
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
