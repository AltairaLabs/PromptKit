// Package statestore provides conversation state persistence and management.
package statestore

import (
	"context"
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Store defines the interface for persistent conversation state storage.
//
// Store is read-shaped: it deliberately does not include bulk-write methods.
// Callers needing to persist state should use the typed write interfaces
// (MessageAppender, MetadataAccessor.MergeMetadata, SummaryAccessor.SaveSummary).
// Admin/seed paths that need to replace whole state should type-assert for
// BulkWriter; hot-path pipeline stages must not.
type Store interface {
	// Load retrieves conversation state by ID
	Load(ctx context.Context, id string) (*ConversationState, error)

	// Fork creates a copy of an existing conversation state with a new ID
	// The original conversation is left unchanged. Returns ErrNotFound if sourceID doesn't exist.
	Fork(ctx context.Context, sourceID, newID string) error
}

// BulkWriter allows replacing the entire conversation state in one
// operation. Optional and explicitly OUT-OF-BAND for hot-path stages —
// it exists for admin tools, test seeders, and Session.Clear-style ops.
//
// Pipeline stages must NEVER take a BulkWriter as a config field. Use
// MessageAppender, MetadataAccessor.MergeMetadata, and SummaryAccessor.SaveSummary
// for typed, race-safe writes instead. A reflective test in
// runtime/pipeline/stage enforces this invariant.
type BulkWriter interface {
	// Save persists the entire conversation state, overwriting any existing
	// record. Auto-creates the conversation if it doesn't exist.
	Save(ctx context.Context, state *ConversationState) error
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
// Pipeline stages type-assert for this interface and fall back to BulkWriter when unavailable.
type MessageAppender interface {
	// AppendMessages appends messages to the conversation's message history.
	// Auto-creates the conversation if it doesn't exist.
	AppendMessages(ctx context.Context, id string, messages []types.Message) error
}

// MetadataAccessor allows reading and writing metadata without loading the
// full state. This is an optional interface — stores that implement it
// enable efficient metadata operations without the cost of deep-copying the
// message history. Pipeline stages type-assert for this interface; reads
// fall back to Store.Load when LoadMetadata is unavailable, and writes fall
// back to BulkWriter when MergeMetadata is unavailable.
type MetadataAccessor interface {
	// LoadMetadata returns just the metadata map for the given conversation.
	// Returns ErrNotFound if the conversation doesn't exist.
	// The returned map is a deep copy safe for mutation by the caller.
	LoadMetadata(ctx context.Context, id string) (map[string]interface{}, error)

	// MergeMetadata atomically merges the supplied keys into the
	// conversation's Metadata map. Existing keys are overwritten, other
	// fields (Messages, Summaries, etc.) are untouched. Auto-creates the
	// conversation if it doesn't exist.
	MergeMetadata(ctx context.Context, id string, updates map[string]interface{}) error
}

// SummaryAccessor allows reading and writing summaries independently of the full state.
// This is an optional interface for stores that support efficient summary operations.
type SummaryAccessor interface {
	// LoadSummaries returns all summaries for the given conversation.
	// Returns nil (not an error) if no summaries exist.
	LoadSummaries(ctx context.Context, id string) ([]Summary, error)

	// SaveSummary appends a summary to the conversation's summary list.
	// Auto-creates the conversation if it doesn't exist.
	SaveSummary(ctx context.Context, id string, summary Summary) error
}

// ListAccessor allows storing append-only collections of opaque items
// (JSON-encoded by the caller) per conversation. Each list is keyed by
// a stable name (e.g. "workflow.history").
//
// Stores that implement this interface persist appends incrementally —
// MemoryStore in a Go slice, RedisStore as a Redis list (RPUSH). This
// keeps per-write cost O(new entries) regardless of how long the
// collection has grown — the load-bearing property for long-running
// workflows whose History/ArtifactHistory grow without bound.
//
// Optional. Callers (typically the SDK workflow code) type-assert; the
// caller surfaces a clear error when the store doesn't satisfy.
type ListAccessor interface {
	// AppendList appends items to the named list for the conversation.
	// Items are opaque bytes (typically JSON-encoded by the caller).
	// Auto-creates the conversation and the list if either is missing.
	AppendList(ctx context.Context, id, listName string, items [][]byte) error

	// LoadList returns all items of the named list, in append order.
	// Returns (nil, nil) — not an error — when the list is empty or
	// has never been written. Returns ErrNotFound only when the
	// conversation itself doesn't exist.
	LoadList(ctx context.Context, id, listName string) ([][]byte, error)

	// ListLen returns the current length of the named list. Returns 0
	// for empty/missing lists; ErrNotFound only when the conversation
	// doesn't exist.
	ListLen(ctx context.Context, id, listName string) (int, error)
}

// ErrNotFound is returned when a conversation doesn't exist in the store.
var ErrNotFound = errors.New("conversation not found")

// ErrInvalidID is returned when an invalid conversation ID is provided.
var ErrInvalidID = errors.New("invalid conversation ID")

// ErrInvalidState is returned when a conversation state is invalid.
var ErrInvalidState = errors.New("invalid conversation state")
