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

// collectedData holds all data collected from stream elements.
type collectedData struct {
	messages []types.Message
	metadata map[string]interface{}
	trace    *pipeline.ExecutionTrace
	costInfo *types.CostInfo
}

// collectFromElement extracts data from a single element and updates collected data.
func (d *collectedData) collectFromElement(elem *stage.StreamElement) {
	if elem.Message != nil {
		d.messages = append(d.messages, *elem.Message)
	}
	if elem.Metadata == nil {
		return
	}
	if d.metadata == nil {
		d.metadata = make(map[string]interface{})
	}
	for k, v := range elem.Metadata {
		d.metadata[k] = v
	}
	if t, ok := elem.Metadata["execution_trace"].(*pipeline.ExecutionTrace); ok {
		d.trace = t
	}
	if c, ok := elem.Metadata["cost_info"].(*types.CostInfo); ok {
		d.costInfo = c
	}
}

// forwardElements just forwards elements without collecting.
func forwardElements(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	for elem := range input {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Process collects all messages and saves them with telemetry to Arena state store.
func (s *ArenaStateStoreSaveStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	if s.config == nil || s.config.Store == nil {
		return forwardElements(ctx, input, output)
	}

	arenaStore, ok := s.config.Store.(*statestore.ArenaStateStore)
	if !ok {
		return fmt.Errorf("arena state store save: invalid store type, expected *statestore.ArenaStateStore")
	}

	data := &collectedData{}
	for elem := range input {
		data.collectFromElement(&elem)
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	err := s.saveToArenaStateStore(
		ctx, arenaStore, data.messages, data.metadata, data.trace, data.costInfo)
	if err != nil {
		return fmt.Errorf("arena state store save: %w", err)
	}

	return nil
}

// createNewState creates a new conversation state with config defaults.
func (s *ArenaStateStoreSaveStage) createNewState() *runtimeStatestore.ConversationState {
	state := &runtimeStatestore.ConversationState{
		ID:       s.config.ConversationID,
		UserID:   s.config.UserID,
		Messages: make([]types.Message, 0),
		Metadata: make(map[string]interface{}),
	}
	for k, v := range s.config.Metadata {
		state.Metadata[k] = v
	}
	return state
}

// updateStateMetadata updates state metadata with execution data and cost info.
func updateStateMetadata(
	state *runtimeStatestore.ConversationState,
	metadata map[string]interface{},
	costInfo *types.CostInfo,
) {
	if state.Metadata == nil {
		state.Metadata = make(map[string]interface{})
	}
	for k, v := range metadata {
		state.Metadata[k] = v
	}
	if costInfo != nil && costInfo.TotalCost > 0 {
		state.Metadata["total_cost_usd"] = costInfo.TotalCost
		state.Metadata["total_tokens"] = costInfo.InputTokens + costInfo.OutputTokens
	}
}

// ensureTrace returns the provided trace or creates a default one.
func ensureTrace(trace *pipeline.ExecutionTrace) *pipeline.ExecutionTrace {
	if trace != nil {
		return trace
	}
	return &pipeline.ExecutionTrace{
		StartedAt: time.Now(),
		LLMCalls:  []pipeline.LLMCall{},
		Events:    []pipeline.TraceEvent{},
	}
}

// saveToArenaStateStore saves conversation state with telemetry.
func (s *ArenaStateStoreSaveStage) saveToArenaStateStore(
	ctx context.Context,
	arenaStore *statestore.ArenaStateStore,
	messages []types.Message,
	metadata map[string]interface{},
	trace *pipeline.ExecutionTrace,
	costInfo *types.CostInfo,
) error {
	state, err := arenaStore.Load(ctx, s.config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return fmt.Errorf("failed to load state: %w", err)
	}

	if state == nil {
		state = s.createNewState()
	}

	// Set messages with system prompt if present
	if systemPrompt, ok := metadata["system_prompt"].(string); ok && systemPrompt != "" {
		state.Messages = prependSystemMessage(messages, systemPrompt)
	} else {
		state.Messages = make([]types.Message, len(messages))
		copy(state.Messages, messages)
	}

	updateStateMetadata(state, metadata, costInfo)

	if err := arenaStore.SaveWithTrace(ctx, state, ensureTrace(trace)); err != nil {
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
