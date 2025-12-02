package middleware

import (
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// stateStoreSaveMiddleware saves conversation state to StateStore.
type stateStoreSaveMiddleware struct {
	config *pipeline.StateStoreConfig
}

// StateStoreSaveMiddleware saves conversation state to StateStore.
// It should be placed LAST in the pipeline, after all other middleware.
// It saves even if the pipeline execution failed (to preserve partial state).
func StateStoreSaveMiddleware(config *pipeline.StateStoreConfig) pipeline.Middleware {
	return &stateStoreSaveMiddleware{config: config}
}

// Process saves the conversation state to the state store after execution completes.
// This middleware should be placed last in the pipeline after all other middleware.
// It saves state even if execution failed to preserve partial state for debugging and recovery.
func (m *stateStoreSaveMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Continue to next middleware first
	err := next()

	// Always save state after execution (even if error occurred)
	// This preserves partial state for debugging/recovery

	// Skip if no config provided (no-op)
	if m.config == nil || m.config.Store == nil {
		return err // Return the error from next() if any
	}

	// Type assert to statestore.Store
	store, ok := m.config.Store.(statestore.Store)
	if !ok {
		return fmt.Errorf("state store save: invalid store type")
	}

	// ALWAYS save state (even if error occurred in execution)
	saveErr := saveToStateStore(execCtx, store, m.config)
	if saveErr != nil {
		return fmt.Errorf("state store save: failed to save state: %w", saveErr)
	}

	if execCtx.EventEmitter != nil {
		execCtx.EventEmitter.StateSaved(m.config.ConversationID, len(execCtx.Messages))
	}

	return err // Return the original error from next() if any
}

func saveToStateStore(execCtx *pipeline.ExecutionContext, store statestore.Store, config *pipeline.StateStoreConfig) error {
	// Load current state (or create new)
	state, err := store.Load(execCtx.Context, config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return err
	}

	// Create new state if not found
	if state == nil {
		state = &statestore.ConversationState{
			ID:       config.ConversationID,
			UserID:   config.UserID,
			Messages: make([]types.Message, 0),
			Metadata: make(map[string]interface{}),
		}

		// Initialize with config metadata if provided
		for k, v := range config.Metadata {
			state.Metadata[k] = v
		}
	}

	// Update state with all messages from execution
	// Copy messages to preserve immutability
	state.Messages = make([]types.Message, len(execCtx.Messages))
	copy(state.Messages, execCtx.Messages)

	// Copy execution metadata (overwrites state metadata)
	for k, v := range execCtx.Metadata {
		state.Metadata[k] = v
	}

	// Note: statestore.ConversationState doesn't have TotalCost/TotalTokens fields
	// Those are tracked separately in execCtx.CostInfo
	// We can store them in Metadata if needed:
	if execCtx.CostInfo.TotalCost > 0 {
		state.Metadata["total_cost_usd"] = execCtx.CostInfo.TotalCost
		state.Metadata["total_tokens"] = execCtx.CostInfo.InputTokens + execCtx.CostInfo.OutputTokens
	}

	// Save to store (MemoryStore will set LastAccessedAt automatically)
	return store.Save(execCtx.Context, state)
}

// StreamChunk is a no-op for state store save middleware as it doesn't process stream chunks.
func (m *stateStoreSaveMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// StateStore save middleware doesn't process chunks
	return nil
}
