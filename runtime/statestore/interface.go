// Package statestore provides conversation state persistence and management.
package statestore

import (
	"context"
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Store defines the interface for persistent conversation state storage.
type Store interface {
	// Load retrieves conversation state by ID
	Load(ctx context.Context, id string) (*ConversationState, error)

	// Save persists conversation state
	Save(ctx context.Context, state *ConversationState) error

	// Fork creates a copy of an existing conversation state with a new ID
	// The original conversation is left unchanged. Returns ErrNotFound if sourceID doesn't exist.
	Fork(ctx context.Context, sourceID, newID string) error
}

// ListOptions provides filtering and pagination options for listing conversations.
type ListOptions struct {
	// UserID filters conversations by the user who owns them.
	// If empty, all conversations are returned (subject to pagination).
	UserID string

	// Limit is the maximum number of conversation IDs to return.
	// If 0, a default limit (e.g., 100) should be applied.
	Limit int

	// Offset is the number of conversations to skip (for pagination).
	Offset int

	// SortBy specifies the field to sort by (e.g., "created_at", "updated_at").
	// If empty, implementation-specific default sorting is used.
	SortBy string

	// SortOrder specifies sort direction: "asc" or "desc".
	// If empty, defaults to "desc" (newest first).
	SortOrder string
}

// MessageReader allows loading a subset of messages without full state deserialization.
// This is an optional interface — stores that implement it enable efficient partial reads.
// Pipeline stages type-assert for this interface and fall back to Store.Load when unavailable.
type MessageReader interface {
	// LoadRecentMessages returns the last n messages for the given conversation.
	// Returns ErrNotFound if the conversation doesn't exist.
	LoadRecentMessages(ctx context.Context, id string, n int) ([]types.Message, error)

	// MessageCount returns the total number of messages in the conversation.
	// Returns ErrNotFound if the conversation doesn't exist.
	MessageCount(ctx context.Context, id string) (int, error)
}

// MessageAppender allows appending messages without a full load+replace+save cycle.
// This is an optional interface — stores that implement it enable incremental saves.
// Pipeline stages type-assert for this interface and fall back to Store.Save when unavailable.
type MessageAppender interface {
	// AppendMessages appends messages to the conversation's message history.
	// Creates the conversation if it doesn't exist.
	AppendMessages(ctx context.Context, id string, messages []types.Message) error
}

// SummaryAccessor allows reading and writing summaries independently of the full state.
// This is an optional interface for stores that support efficient summary operations.
type SummaryAccessor interface {
	// LoadSummaries returns all summaries for the given conversation.
	// Returns nil (not an error) if no summaries exist.
	LoadSummaries(ctx context.Context, id string) ([]Summary, error)

	// SaveSummary appends a summary to the conversation's summary list.
	SaveSummary(ctx context.Context, id string, summary Summary) error
}

// ErrNotFound is returned when a conversation doesn't exist in the store.
var ErrNotFound = errors.New("conversation not found")

// ErrInvalidID is returned when an invalid conversation ID is provided.
var ErrInvalidID = errors.New("invalid conversation ID")

// ErrInvalidState is returned when a conversation state is invalid.
var ErrInvalidState = errors.New("invalid conversation state")
