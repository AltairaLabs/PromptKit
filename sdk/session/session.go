package session

import (
	"context"
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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

	// ResumeWithToolResults injects tool result messages into the session
	// history and re-executes the pipeline so the LLM can continue.
	ResumeWithToolResults(ctx context.Context, toolResults []types.Message) (*pipeline.ExecutionResult, error)

	// ResumeStreamWithToolResults is the streaming equivalent of ResumeWithToolResults.
	// It injects tool result messages and returns a streaming channel.
	ResumeStreamWithToolResults(ctx context.Context, toolResults []types.Message) (<-chan providers.StreamChunk, error)

	// ForkSession creates a new session that is a fork of this one.
	// The new session will have an independent copy of the conversation state.
	ForkSession(ctx context.Context, forkID string, pipeline *stage.StreamPipeline) (UnarySession, error)
}

// DuplexSession manages bidirectional streaming conversations.
// Uses providers.StreamChunk for BOTH input and output for API symmetry.
type DuplexSession interface {
	BaseSession

	// SendChunk sends a chunk to the session (populate MediaData for media, Content for text).
	// This method is thread-safe and can be called from multiple goroutines.
	SendChunk(ctx context.Context, chunk *providers.StreamChunk) error

	// SendText is a convenience method for sending text directly.
	SendText(ctx context.Context, text string) error

	// SendFrame sends an image frame to the session for realtime video scenarios.
	// This is a convenience method that wraps SendChunk with proper image formatting.
	SendFrame(ctx context.Context, frame *ImageFrame) error

	// SendVideoChunk sends a video chunk to the session for encoded video streaming.
	// This is a convenience method that wraps SendChunk with proper video formatting.
	SendVideoChunk(ctx context.Context, chunk *VideoChunk) error

	// Response returns a receive-only channel for streaming responses.
	// The channel emits StreamChunks containing LLM responses (text, media, tool calls, etc).
	Response() <-chan providers.StreamChunk

	// Close ends the streaming session and releases resources.
	Close() error

	// Drain gracefully stops the session: sends EndOfStream to the pipeline,
	// waits for the provider to finish processing (up to the given timeout),
	// then closes. Returns nil if the pipeline completes within the timeout.
	Drain(ctx context.Context) error

	// Done returns a channel that's closed when the session ends.
	Done() <-chan struct{}

	// Error returns any error that occurred during the session.
	Error() error

	// SubmitToolResults sends resolved/rejected tool results back into the
	// duplex pipeline so they flow to the provider via ToolResponseSupport.
	// Used after HITL approval or client tool fulfillment.
	SubmitToolResults(ctx context.Context, responses []providers.ToolResponse) error

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
// For ASM mode: streamProvider will be non-nil, builder passes it to
// DuplexProviderStage, which creates the session lazily using
// TurnState.SystemPrompt (published by PromptAssemblyStage).
//
// For VAD mode: streamProvider will be nil, builder creates pipeline with VAD/TTS stages.
type PipelineBuilder func(
	ctx context.Context,
	provider providers.Provider, // Provider for making LLM calls (required)
	streamProvider providers.StreamInputSupport, // For ASM mode: streaming provider. For VAD mode: nil
	streamConfig *providers.StreamingInputConfig, // Base config for session creation (system prompt comes from pipeline)
	conversationID string,
	store statestore.Store,
) (*stage.StreamPipeline, error)

// AsyncToolCheckResult describes the outcome of an HITL check on a tool call.
type AsyncToolCheckResult struct {
	// ShouldWait is true if the tool requires human approval before execution.
	ShouldWait bool
	// PendingInfo provides context when ShouldWait is true.
	PendingInfo *tools.PendingToolInfo
	// Handled is true when the checker executed the handler directly
	// (i.e., the tool was an async tool but the check passed, so it ran immediately).
	Handled bool
	// HandlerResult contains the JSON-encoded result when Handled is true.
	HandlerResult json.RawMessage
	// HandlerError is set when Handled is true but execution failed.
	HandlerError error
}

// AsyncToolChecker is called before a duplex session executes a tool call,
// to determine if it requires human approval. Returns nil if the tool is not
// an async tool (falls through to normal registry execution). The context is
// the tool-execution context so a checker persisting the held call honors
// cancellation and deadlines. It is consulted only when the session runs tool
// calls itself (see DuplexSessionConfig.PipelineRunsTools).
type AsyncToolChecker func(ctx context.Context, callID, name string, args map[string]any) *AsyncToolCheckResult

// DuplexSessionConfig configures a DuplexSession.
//
// PipelineBuilder and Provider are required.
// PipelineBuilder is typically a closure created in SDK that captures configuration.
//
// Two modes based on Config field:
//
// ASM Mode (Config provided):
//   - Pipeline is the single source of truth
//   - DuplexProviderStage creates the session lazily using TurnState.SystemPrompt
//   - PipelineBuilder receives streaming provider + base config (NOT a pre-created session)
//   - Single long-running pipeline execution for continuous streaming
//
// VAD Mode (Config nil):
//   - No provider session created
//   - Calls PipelineBuilder with nil streaming provider
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
	Config          *providers.StreamingInputConfig // For ASM mode: base streaming config. For VAD mode: nil
	// ToolRegistry runs the session's tool calls. Who runs them depends on
	// PipelineRunsTools; with no registry nothing does, and an assistant turn
	// that made tool calls is forwarded for the caller to answer with
	// SubmitToolResults.
	ToolRegistry *tools.Registry
	// PipelineRunsTools is set when the pipeline's DuplexProviderStage runs
	// the session's tool calls (stage.DuplexProviderStage.SetToolExecution),
	// as the SDK's pipelines do: the session then runs none, does not forward
	// a tool-call turn, and passes on the calls the stage holds for approval.
	// Unset, a session with a ToolRegistry runs the calls itself, gated by
	// AsyncToolChecker, and sends their results back into the pipeline.
	PipelineRunsTools bool
	// AsyncToolChecker gates the calls the session runs itself; it is not
	// consulted when PipelineRunsTools is set (the stage's ApprovalGate holds
	// calls there).
	AsyncToolChecker AsyncToolChecker
	// Caller is who the conversation acts for (sdk.WithCaller). Every tool call
	// the session runs carries it; with PipelineRunsTools the pipeline is given
	// it (stage.DuplexToolConfig.Caller). The zero value means none was set.
	Caller    types.Caller
	Metadata  map[string]interface{}
	Variables map[string]string // Initial variables for template substitution
	// OnTurnComplete, when set, is called once for each turn the session
	// completes, before the output that completes it reaches Response(): a
	// realtime assistant reply that makes no tool calls and was not cut short,
	// or a cascaded (VAD) turn's end-of-turn boundary, whose messages the state
	// store holds by then. A turn the store failed to write is not reported.
	// It runs on the session's output goroutine, so the session's output waits
	// for it.
	OnTurnComplete func(ctx context.Context)
}
