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
type BidirectionalSession interface {
	Connect(ctx context.Context, providerSession providers.StreamInputSession) error
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
	ConversationID string
	UserID         string
	StateStore     statestore.Store
	Pipeline       *pipeline.Pipeline
	Metadata       map[string]interface{}
}

// NewTextSession creates a new text session.
func NewTextSession(cfg TextConfig) (TextSession, error) {
	return newTextSession(cfg)
}

// NewBidirectionalSession creates a new bidirectional streaming session.
func NewBidirectionalSession(cfg BidirectionalConfig) (BidirectionalSession, error) {
	return newBidirectionalSession(cfg)
}
