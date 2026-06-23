package engine

import (
	"context"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
)

// ConversationExecutor orchestrates full conversation flows
type ConversationExecutor interface {
	// ExecuteConversation runs a complete conversation based on scenario
	ExecuteConversation(ctx context.Context, req ConversationRequest) *ConversationResult

	// ExecuteConversationStream runs a conversation with streaming
	ExecuteConversationStream(ctx context.Context, req ConversationRequest) (<-chan ConversationStreamChunk, error)
}

// ConversationStreamChunk represents a streaming chunk during conversation execution
type ConversationStreamChunk struct {
	// Current turn number (0-indexed)
	TurnIndex int

	// Delta content from this specific chunk
	Delta string

	// Token count (accumulated for current turn)
	TokenCount int

	// Finish reason for current turn (only in last chunk of turn)
	FinishReason *string

	// Complete conversation result (accumulated, updated with each chunk)
	Result *ConversationResult

	// Error if streaming failed
	Error error

	// Metadata
	Metadata map[string]interface{}
}

// ConversationRequest contains all data needed for conversation execution.
// Using a request object makes the API extensible without breaking changes.
type ConversationRequest struct {
	// Required fields
	Provider providers.Provider
	Scenario *config.Scenario
	Eval     *config.Eval // Eval configuration (mutually exclusive with Scenario)
	Config   *config.Config
	Region   string

	// Optional overrides (for future use)
	Temperature *float64 // Override scenario temperature
	MaxTokens   *int     // Override scenario max tokens
	Timeout     *int     // Timeout in seconds

	// For distributed execution and tracing (v0.2.0+)
	RunID    string            // Unique identifier for this run
	Metadata map[string]string // Additional metadata for debugging/tracing

	// Event bus for runtime/TUI events
	EventBus events.Bus

	// State management
	StateStoreConfig *StateStoreConfig // Optional state store configuration
	ConversationID   string            // Conversation identifier for state persistence

	// Per-run eval orchestrator override (for workflow scenarios that need
	// isolated workflow metadata). If nil, the executor's shared orchestrator is used.
	EvalOrchestrator *EvalOrchestrator

	// VoiceSTT is the STT provider for the VAD voice path (Task 7). Nil in ASM mode.
	// Set from --voice-stt by the chat command; consumed by runInteractiveVADVoice.
	VoiceSTT *config.Provider

	// VoiceOutputVoice is the TTS voice ID for the VAD voice path (Task 7).
	// Set from --voice-output-voice by the chat command; consumed by runInteractiveVADVoice.
	VoiceOutputVoice string

	// RecordingConfig enables RecordingStage in the pipeline for message.created events.
	// If nil, no recording stages are added.
	RecordingConfig *stage.RecordingStageConfig

	// EventStore is the destination for RecordingStage writes. Required when
	// RecordingConfig is non-nil; without it, recording stages will not be added
	// to the pipeline.
	EventStore events.EventStore

	// PostTurnHook is called after each turn completes. Used by the workflow
	// engine to commit deferred transitions after the pipeline finishes.
	PostTurnHook func() error

	// ContextEnricher is called before each turn to enrich the pipeline context.
	// Used to inject per-run state (e.g., skill filters) into context for tool execution.
	ContextEnricher func(ctx context.Context) context.Context

	// ActiveCompositionResolver, when non-nil, is called per-turn to determine
	// the active composition for the current workflow state (RFC 0010). It returns
	// nil for states that are not composition-orchestrated.
	ActiveCompositionResolver func() *composition.Composition

	// CompositionRecorder is the per-run recorder for RFC 0010 testability. When
	// non-nil it is stamped onto every TurnRequest so buildStagePipeline can pass
	// it to NewCompositionStageWithRecorder. Reset() is called per turn so stale
	// data from a prior turn does not leak into the next turn's assertions.
	CompositionRecorder *stage.CompositionRecorder

	// AudioRouter, when non-nil, is the per-run AudioRouter for audio
	// monitoring. MonitorTap stages added to the pipeline publish to this
	// router. Lifetime is the run; the engine owns Close().
	AudioRouter *arenaaudio.AudioRouter

	// CurrentWorkflowState returns the live workflow-state metadata for the turn
	// about to run (captured at turn start, before the pipeline builds its
	// prompt). Nil when there is no workflow. Used to stamp current_workflow_state
	// onto each assistant result so composition-orchestrated terminal states —
	// which leave no transition tool-result message — are still visible per turn.
	CurrentWorkflowState func() map[string]interface{}

	// StampWorkflowState attaches meta (captured at turn start) to the assistant
	// message produced by the just-completed turn. No-op when meta is nil. This is
	// side-effect-only and must never alter turn output, error, or flow.
	StampWorkflowState func(meta map[string]interface{})
}

// StateStoreConfig wraps the pipeline StateStore configuration for Arena
type StateStoreConfig struct {
	Store    interface{}            // State store implementation (statestore.Store)
	UserID   string                 // User identifier (optional)
	Metadata map[string]interface{} // Additional metadata to store (optional)
}

// ConversationResult contains the outcome of conversation execution
type ConversationResult struct {
	Messages     []types.Message         // Flat list of all messages in the conversation
	Cost         types.CostInfo          // Total cost across all messages
	ToolStats    *types.ToolStats        // Tool usage statistics
	Violations   []types.ValidationError // Validation errors
	MediaOutputs []MediaOutput           // Media outputs generated by LLMs

	// Conversation-level assertions (test-only gates: passed/failed)
	ConversationAssertionResults []assertions.ConversationValidationResult `json:"conv_assertions_results,omitempty"`

	// EvalResults are the runtime's pack-level eval observations —
	// non-gating measurements that fire during a session (the same
	// thing that would surface in production). Distinct from
	// ConversationAssertionResults because evals don't pass/fail;
	// they emit structured Value/Details for reporting and aggregation.
	EvalResults []evals.EvalResult `json:"eval_results,omitempty"`

	// Self-play metadata
	SelfPlay  bool   `json:"self_play,omitempty"`
	PersonaID string `json:"persona_id,omitempty"`

	// Error handling
	Error  string `json:"error,omitempty"`  // Error message if execution failed
	Failed bool   `json:"failed,omitempty"` // Whether execution failed (but partial results may be available)

	Skipped    bool   `json:"skipped,omitempty"`     // Whether execution was skipped due to transient provider error
	SkipReason string `json:"skip_reason,omitempty"` // Reason for skipping
}
