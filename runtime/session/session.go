package session

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TextSession manages text-based conversations.
type TextSession interface {
	// Execution methods
	Execute(ctx context.Context, role, content string) (*pipeline.ExecutionResult, error)
	ExecuteWithMessage(ctx context.Context, message types.Message) (*pipeline.ExecutionResult, error)
	ExecuteStream(ctx context.Context, role, content string) (<-chan providers.StreamChunk, error)
	ExecuteStreamWithMessage(ctx context.Context, message types.Message) (<-chan providers.StreamChunk, error)

	// Variable management
	SetVar(name, value string)
	GetVar(name string) string
	Variables() map[string]string

	// Accessors
	ID() string
	StateStore() statestore.Store
}

// BidirectionalSession manages bidirectional streaming conversations.
// Uses providers.StreamChunk for BOTH input and output for API symmetry.
type BidirectionalSession interface {
	// Identity
	ID() string

	// SendChunk sends a chunk to the session (populate MediaDelta for media, Content for text).
	// This method is thread-safe and can be called from multiple goroutines.
	SendChunk(ctx context.Context, chunk *providers.StreamChunk) error

	// SendText is a convenience method for sending text directly.
	SendText(ctx context.Context, text string) error

	// Response returns a receive-only channel for streaming responses.
	// The channel emits StreamChunks containing LLM responses (text, media, tool calls, etc).
	Response() <-chan providers.StreamChunk

	// Close ends the streaming session and releases resources.
	Close() error

	// Done returns a channel that's closed when the session ends.
	Done() <-chan struct{}

	// Error returns any error that occurred during the session.
	Error() error

	// StateStore returns the session's state store for persistence.
	StateStore() statestore.Store

	// Variables returns the current session variables for template substitution.
	Variables() map[string]string

	// SetVar sets a session variable.
	SetVar(name, value string)

	// GetVar retrieves a session variable.
	GetVar(name string) (string, bool)
}

// TextConfig configures a TextSession.
type TextConfig struct {
	ConversationID string
	UserID         string
	StateStore     statestore.Store
	Pipeline       *pipeline.Pipeline
	Metadata       map[string]interface{}
	Variables      map[string]string // Initial variables for template substitution
}

// BidirectionalConfig configures a BidirectionalSession.
type BidirectionalConfig struct {
	ConversationID  string
	UserID          string
	StateStore      statestore.Store
	Pipeline        *pipeline.Pipeline
	ProviderSession providers.StreamInputSession // Direct provider session (alternative to Pipeline)
	Metadata        map[string]interface{}
	Variables       map[string]string // Initial variables for template substitution
}
