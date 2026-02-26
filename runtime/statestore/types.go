package statestore

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Sort field constants for ListOptions.SortBy.
const (
	SortByCreatedAt = "created_at"
	SortByUpdatedAt = "updated_at"
)

// defaultTTLHours is the default TTL for conversation states (24 hours).
const defaultTTLHours = 24

// ConversationState represents stored conversation state in the state store.
// This is the primary data structure for persisting and loading conversation history.
type ConversationState struct {
	ID             string                 // Unique conversation identifier
	UserID         string                 // User who owns this conversation
	Messages       []types.Message        // Message history (using unified types.Message)
	SystemPrompt   string                 // System prompt for this conversation
	Summaries      []Summary              // Compressed summaries of old turns
	TokenCount     int                    // Total tokens in messages
	LastAccessedAt time.Time              // Last time conversation was accessed
	Metadata       map[string]interface{} // Arbitrary metadata (e.g., extracted context)
}

// Summary represents a compressed version of conversation turns.
// Used to maintain context while reducing token count for older conversations.
type Summary struct {
	StartTurn  int       // First turn included in this summary
	EndTurn    int       // Last turn included in this summary
	Content    string    // Summarized content
	TokenCount int       // Token count of the summary
	CreatedAt  time.Time // When this summary was created
}
