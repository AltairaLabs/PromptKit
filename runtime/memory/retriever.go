package memory

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Retriever finds relevant memories given conversation context.
// Used by the retrieval pipeline stage for automatic RAG injection.
// Different from Store.Retrieve() which is tool-facing (specific query).
// Retriever sees the full conversation context and decides what's relevant.
type Retriever interface {
	RetrieveContext(ctx context.Context, scope map[string]string, messages []types.Message) ([]*Memory, error)
}
