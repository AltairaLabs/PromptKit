package session

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
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
	ForkSession(ctx context.Context, forkID string, pipeline *stage.StreamPipeline) (UnarySession, error)
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
	ForkSession(
		ctx context.Context,
		forkID string,
		pipelineBuilder PipelineBuilder,
	) (DuplexSession, error)
}

// UnarySessionConfig configures a TextSession.
// StateStore should match what's configured in the Pipeline stages.
type UnarySessionConfig struct {
	ConversationID string
	UserID         string
	StateStore     statestore.Store // Must match Pipeline's StateStore stages
	Pipeline       *stage.StreamPipeline
	Metadata       map[string]interface{}
	Variables      map[string]string // Initial variables for template substitution
}

// PipelineBuilder creates a StreamPipeline for a DuplexSession.
// This is typically a closure created in SDK that captures configuration.
//
// For ASM mode: session will be non-nil, builder creates pipeline with DuplexProviderStage that uses it.
// For VAD mode: session will be nil, builder creates pipeline with VAD/TTS stages.
type PipelineBuilder func(
	ctx context.Context,
	provider providers.Provider, // Provider for making LLM calls (required)
	session providers.StreamInputSession, // nil for VAD mode, set for ASM mode
	conversationID string,
	store statestore.Store,
) (*stage.StreamPipeline, error)

// DuplexSessionConfig configures a DuplexSession.
//
// PipelineBuilder and Provider are required.
// PipelineBuilder is typically a closure created in SDK that captures configuration.
//
// Two modes based on Config field:
//
// ASM Mode (Config provided):
//   - DuplexSession creates persistent provider session
//   - Calls PipelineBuilder with provider and session
//   - Builder creates pipeline with provider middleware that uses the session
//   - Single long-running pipeline execution for continuous streaming
//
// VAD Mode (Config nil):
//   - No provider session created
//   - Calls PipelineBuilder with provider and nil session
//   - Builder creates pipeline with VAD middleware and provider middleware for one-shot calls
//   - Multiple pipeline executions, one per detected turn
//
// StateStore should match what's configured in the Pipeline middleware.
type DuplexSessionConfig struct {
	ConversationID  string
	UserID          string
	StateStore      statestore.Store                // StateStore for conversation history
	PipelineBuilder PipelineBuilder                 // Function to build pipeline (required, typically a closure from SDK)
	Provider        providers.Provider              // Provider for LLM calls (required)
	Config          *providers.StreamingInputConfig // For ASM mode: streaming config. For VAD mode: nil
	Metadata        map[string]interface{}
	Variables       map[string]string // Initial variables for template substitution
}
