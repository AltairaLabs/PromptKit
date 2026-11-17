package memory

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Retriever finds relevant memories given conversation context.
// Used by the retrieval pipeline stage for automatic RAG injection.
// Different from Store.Retrieve() which is tool-facing (specific query).
// Retriever sees the full conversation context and decides what's relevant.
type Retriever interface {
	RetrieveContext(ctx context.Context, scope map[string]string, messages []types.Message) ([]*Memory, error)
}

// ContextRequestRetriever is a Retriever that is given the whole request,
// caller included. The retrieval stage calls RetrieveContextRequest in place
// of RetrieveContext when the retriever implements it, so a per-tenant
// retriever sees who the turn acts for.
type ContextRequestRetriever interface {
	Retriever
	RetrieveContextRequest(ctx context.Context, req ContextRequest) ([]*Memory, error)
}

// RetrieveContextFor asks r for the memories relevant to req: through
// RetrieveContextRequest when r implements ContextRequestRetriever, else
// through RetrieveContext, which does not see the caller. The retrieval stage
// retrieves through it.
func RetrieveContextFor(ctx context.Context, r Retriever, req ContextRequest) ([]*Memory, error) {
	if cr, ok := r.(ContextRequestRetriever); ok {
		return cr.RetrieveContextRequest(ctx, req)
	}
	return r.RetrieveContext(ctx, req.Scope, req.Messages)
}
