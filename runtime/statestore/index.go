package statestore

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MessageIndex provides semantic search over conversation messages.
// Implementations can use embedding-based vector search or other similarity methods
// to find messages relevant to a given query.
type MessageIndex interface {
	// Index adds a message to the search index for the given conversation.
	// turnIndex is the position of the message in the conversation history.
	Index(ctx context.Context, conversationID string, turnIndex int, message types.Message) error

	// Search finds the top-k messages most relevant to the query string.
	// Results are ordered by descending relevance score.
	Search(ctx context.Context, conversationID string, query string, k int) ([]IndexResult, error)

	// Delete removes all indexed messages for a conversation.
	Delete(ctx context.Context, conversationID string) error
}

// IndexResult represents a single search result from the message index.
type IndexResult struct {
	// TurnIndex is the position of the message in the conversation history.
	TurnIndex int

	// Message is the full message content.
	Message types.Message

	// Score is the relevance score (higher is more relevant, typically 0.0-1.0).
	Score float64
}
