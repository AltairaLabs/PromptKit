// Package turnexecutors provides execution strategies for different conversation patterns.
//
// This package implements various turn execution modes:
//   - Scripted: Pre-defined user messages from scenario configuration
//   - Self-play: AI-generated user messages for autonomous testing
//   - Pipeline: Direct LLM pipeline execution for single-turn scenarios
//
// Each executor handles the specifics of message generation, provider interaction,
// validation, and result collection for its execution mode.
//
// Key interfaces:
//   - Executor: Base interface for all turn execution strategies
//   - TurnConfig: Configuration for individual turn execution
//
// Implementations:
//   - ScriptedExecutor: Executes pre-defined conversation scripts
//   - SelfPlayExecutor: Generates dynamic user messages via persona LLM
//   - PipelineExecutor: Direct pipeline execution for simple scenarios
package turnexecutors

import (
	"context"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// TurnExecutor executes one complete conversation turn (user message + AI response + tools)
// Messages are persisted to StateStore, not returned
type TurnExecutor interface {
	ExecuteTurn(ctx context.Context, req TurnRequest) error
	ExecuteTurnStream(ctx context.Context, req TurnRequest) (<-chan MessageStreamChunk, error)
}

// MessageStreamChunk represents a streaming chunk during turn execution
type MessageStreamChunk struct {
	// All messages accumulated so far (user + assistant + tool results)
	Messages []types.Message

	// Content delta from this specific chunk
	Delta string

	// Which message is being updated (index in Messages array)
	MessageIndex int

	// Token count (accumulated)
	TokenCount int

	// Finish reason (only in final chunk)
	FinishReason *string

	// Error if streaming failed
	Error error

	// Metadata
	Metadata map[string]interface{}
}

// TurnRequest contains all information needed to execute a turn
type TurnRequest struct {
	// Provider and configuration
	Provider    providers.Provider
	Scenario    *config.Scenario
	Temperature float64
	MaxTokens   int
	Seed        *int

	// Prompt assembly (moved to pipeline)
	PromptRegistry *prompt.Registry
	TaskType       string
	Region         string
	PromptVars     map[string]string // Variable overrides from arena.yaml prompt_configs[].vars

	// Base directory for resolving relative file paths in media content
	BaseDir string

	// State management (StateStore handles all history)
	StateStoreConfig *StateStoreConfig // State store configuration
	ConversationID   string            // Conversation identifier for state persistence

	// Executor-specific fields (only one should be set depending on executor type)
	ScriptedContent string                   // For ScriptedExecutor: the user message content (legacy text-only)
	ScriptedParts   []config.TurnContentPart // For ScriptedExecutor: multimodal content parts (takes precedence over ScriptedContent)
	SelfPlayRole    string                   // For SelfPlayExecutor: the self-play role
	SelfPlayPersona string                   // For SelfPlayExecutor: the persona to use

	// Assertions to validate after turn execution
	Assertions []assertions.AssertionConfig
}

// StateStoreConfig wraps the state store configuration for turn executors
type StateStoreConfig struct {
	Store    interface{}            // State store implementation (statestore.Store)
	UserID   string                 // User identifier (optional)
	Metadata map[string]interface{} // Additional metadata to store (optional)
}
