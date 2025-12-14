package stages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	runtimeStatestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// ArenaStateStoreSaveStage saves conversation state with telemetry to ArenaStateStore.
// This stage captures validation results, turn metrics, and cost information
// for Arena testing and analysis.
type ArenaStateStoreSaveStage struct {
	stage.BaseStage
	config *pipeline.StateStoreConfig
}

// NewArenaStateStoreSaveStage creates a new Arena state store save stage.
func NewArenaStateStoreSaveStage(config *pipeline.StateStoreConfig) *ArenaStateStoreSaveStage {
	return &ArenaStateStoreSaveStage{
		BaseStage: stage.NewBaseStage("arena_statestore_save", stage.StageTypeSink),
		config:    config,
	}
}

// Process collects all messages and saves them with telemetry to Arena state store.
//
//nolint:gocognit,lll // Stream processing with state store operations is inherently complex
func (s *ArenaStateStoreSaveStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	// Skip if no config provided
	if s.config == nil || s.config.Store == nil {
		// Just forward elements
		for elem := range input {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}

	// Type assert to ArenaStateStore
	arenaStore, ok := s.config.Store.(*statestore.ArenaStateStore)
	if !ok {
		return fmt.Errorf("arena state store save: invalid store type, expected *statestore.ArenaStateStore")
	}

	// Collect all messages and metadata while forwarding
	var messages []types.Message
	var metadata map[string]interface{}
	var trace *pipeline.ExecutionTrace
	var costInfo *types.CostInfo

	for elem := range input {
		// Collect messages
		if elem.Message != nil {
			messages = append(messages, *elem.Message)
		}

		// Collect metadata
		if elem.Metadata != nil {
			if metadata == nil {
				metadata = make(map[string]interface{})
			}
			for k, v := range elem.Metadata {
				metadata[k] = v
			}
		}

		// Extract trace if present
		if elem.Metadata != nil {
			if t, ok := elem.Metadata["execution_trace"].(*pipeline.ExecutionTrace); ok {
				trace = t
			}
		}

		// Extract cost info if present
		if elem.Metadata != nil {
			if c, ok := elem.Metadata["cost_info"].(*types.CostInfo); ok {
				costInfo = c
			}
		}

		// Forward element
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Save to Arena state store
	if err := s.saveToArenaStateStore(ctx, arenaStore, messages, metadata, trace, costInfo); err != nil {
		return fmt.Errorf("arena state store save: %w", err)
	}

	return nil
}

// saveToArenaStateStore saves conversation state with telemetry.
//
//nolint:gocognit // State store save with telemetry tracking is complex
func (s *ArenaStateStoreSaveStage) saveToArenaStateStore(
	ctx context.Context,
	arenaStore *statestore.ArenaStateStore,
	messages []types.Message,
	metadata map[string]interface{},
	trace *pipeline.ExecutionTrace,
	costInfo *types.CostInfo,
) error {
	// Load current state (or create new)
	state, err := arenaStore.Load(ctx, s.config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Create new state if not found
	if state == nil {
		state = &runtimeStatestore.ConversationState{
			ID:       s.config.ConversationID,
			UserID:   s.config.UserID,
			Messages: make([]types.Message, 0),
			Metadata: make(map[string]interface{}),
		}

		// Initialize with config metadata if provided
		for k, v := range s.config.Metadata {
			state.Metadata[k] = v
		}
	}

	// Prepend system prompt as the first message if present
	// This ensures the system prompt is visible in Arena results
	if systemPrompt, ok := metadata["system_prompt"].(string); ok && systemPrompt != "" {
		state.Messages = prependSystemMessage(messages, systemPrompt)
	} else {
		// No system prompt, just copy messages
		state.Messages = make([]types.Message, len(messages))
		copy(state.Messages, messages)
	}

	// Initialize state metadata if nil
	if state.Metadata == nil {
		state.Metadata = make(map[string]interface{})
	}

	// Copy execution metadata (overwrites state metadata)
	for k, v := range metadata {
		state.Metadata[k] = v
	}

	// Store cost info in metadata
	if costInfo != nil && costInfo.TotalCost > 0 {
		state.Metadata["total_cost_usd"] = costInfo.TotalCost
		state.Metadata["total_tokens"] = costInfo.InputTokens + costInfo.OutputTokens
	}

	// Store system prompt in metadata for Arena results (backwards compatibility)
	if systemPrompt, ok := metadata["system_prompt"].(string); ok && systemPrompt != "" {
		state.Metadata["system_prompt"] = systemPrompt
	}

	// Create default trace if not present
	if trace == nil {
		trace = &pipeline.ExecutionTrace{
			StartedAt: time.Now(),
			LLMCalls:  []pipeline.LLMCall{},
			Events:    []pipeline.TraceEvent{},
		}
	}

	// Save state with trace (Arena-specific method)
	if err := arenaStore.SaveWithTrace(ctx, state, trace); err != nil {
		return fmt.Errorf("failed to save with trace: %w", err)
	}

	return nil
}

// createSystemMessage creates a system message with the given prompt and timestamp.
func createSystemMessage(systemPrompt string, timestamp time.Time) types.Message {
	textContent := systemPrompt
	return types.Message{
		Role:    "system",
		Content: systemPrompt,
		Parts: []types.ContentPart{
			{
				Type: "text",
				Text: &textContent,
			},
		},
		Timestamp: timestamp,
	}
}

// prependSystemMessage prepends a system message if not already present.
func prependSystemMessage(messages []types.Message, systemPrompt string) []types.Message {
	// Check if first message is already a system message
	if len(messages) > 0 && messages[0].Role == "system" {
		// Already has system message, return as-is
		result := make([]types.Message, len(messages))
		copy(result, messages)
		return result
	}

	// Determine timestamp for system message
	var timestamp time.Time
	if len(messages) > 0 {
		timestamp = messages[0].Timestamp
	} else {
		timestamp = time.Now()
	}

	// Create new slice with system message prepended
	systemMsg := createSystemMessage(systemPrompt, timestamp)
	result := make([]types.Message, 0, len(messages)+1)
	result = append(result, systemMsg)
	result = append(result, messages...)
	return result
}
