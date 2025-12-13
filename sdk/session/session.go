package session

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// BaseSession represents core session capabilities shared by all session types.
type BaseSession interface {
	// Identity
	ID() string

	// Variable management for template substitution
	Variables() map[string]string
	SetVar(name, value string)
	GetVar(name string) (string, bool)

	// State management - encapsulates StateStore access
	Messages(ctx context.Context) ([]types.Message, error)
	Clear(ctx context.Context) error
}

// UnarySession manages unary (request/response) conversations with multimodal support.
type UnarySession interface {
	BaseSession

	// Execution methods
	Execute(ctx context.Context, role, content string) (*pipeline.ExecutionResult, error)
	ExecuteWithMessage(ctx context.Context, message types.Message) (*pipeline.ExecutionResult, error)
	ExecuteStream(ctx context.Context, role, content string) (<-chan providers.StreamChunk, error)
	ExecuteStreamWithMessage(ctx context.Context, message types.Message) (<-chan providers.StreamChunk, error)

	// ForkSession creates a new session that is a fork of this one.
	// The new session will have an independent copy of the conversation state.
	ForkSession(ctx context.Context, forkID string, pipeline *pipeline.Pipeline) (UnarySession, error)
}

// DuplexSession manages bidirectional streaming conversations.
// Uses providers.StreamChunk for BOTH input and output for API symmetry.
type DuplexSession interface {
	BaseSession

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

	// ForkSession creates a new session that is a fork of this one.
	// The new session will have an independent copy of the conversation state.
	// For duplex sessions, the fork is not connected to any streams - the consumer
	// must connect streams before using it.
	ForkSession(
		ctx context.Context,
		forkID string,
		pipeline *pipeline.Pipeline,
		provider providers.StreamInputSupport,
	) (DuplexSession, error)
}

// UnarySessionConfig configures a TextSession.
// StateStore should match what's configured in the Pipeline middleware.
type UnarySessionConfig struct {
	ConversationID string
	UserID         string
	StateStore     statestore.Store // Must match Pipeline's StateStore middleware
	Pipeline       *pipeline.Pipeline
	Metadata       map[string]interface{}
	Variables      map[string]string // Initial variables for template substitution
}

// DuplexSessionConfig configures a DuplexSession.
// DuplexSession always uses Pipeline. For duplex sessions, a provider streaming session
// is created first using Provider and Config, then that session is used with the Pipeline.
// StateStore should match what's configured in the Pipeline middleware.
type DuplexSessionConfig struct {
	ConversationID string
	UserID         string
	StateStore     statestore.Store                // Must match Pipeline's StateStore middleware
	Pipeline       *pipeline.Pipeline              // Pipeline to execute (always required)
	Provider       providers.StreamInputSupport    // Provider for creating the streaming session
	Config         *providers.StreamingInputConfig // Configuration for creating provider session
	Metadata       map[string]interface{}
	Variables      map[string]string // Initial variables for template substitution
}
