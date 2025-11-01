package statestore

import (
	"context"
	"errors"
)

// Store defines the interface for persistent conversation state storage.
type Store interface {
	// Load retrieves conversation state by ID
	Load(ctx context.Context, id string) (*ConversationState, error)

	// Save persists conversation state
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

// ErrNotFound is returned when a conversation doesn't exist in the store.
var ErrNotFound = errors.New("conversation not found")

// ErrInvalidID is returned when an invalid conversation ID is provided.
var ErrInvalidID = errors.New("invalid conversation ID")

// ErrInvalidState is returned when a conversation state is invalid.
var ErrInvalidState = errors.New("invalid conversation state")
