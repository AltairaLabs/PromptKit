package middleware

import (
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	runtimeStatestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// arenaStateStoreSaveMiddleware saves conversation state with telemetry to ArenaStateStore.
type arenaStateStoreSaveMiddleware struct {
	config *pipeline.StateStoreConfig
}

// ArenaStateStoreSaveMiddleware saves conversation state with telemetry to ArenaStateStore.
// This middleware captures validation results, turn metrics, and cost information
// for Arena testing and analysis.
func ArenaStateStoreSaveMiddleware(config *pipeline.StateStoreConfig) pipeline.Middleware {
	return &arenaStateStoreSaveMiddleware{config: config}
}

func (m *arenaStateStoreSaveMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Continue to next middleware first
	err := next()

	// Always save state after execution (even if error occurred)
	// Skip if no config provided (no-op)
	if m.config == nil || m.config.Store == nil {
		return err
	}

	// Type assert to ArenaStateStore
	arenaStore, ok := m.config.Store.(*statestore.ArenaStateStore)
	if !ok {
		return fmt.Errorf("arena state store save: invalid store type, expected *statestore.ArenaStateStore")
	}

	// Save state
	saveErr := saveToArenaStateStore(execCtx, arenaStore, m.config)
	if saveErr != nil {
		return fmt.Errorf("arena state store save: failed to save state: %w", saveErr)
	}

	return err // Return the original error from next() if any
}

func saveToArenaStateStore(
	execCtx *pipeline.ExecutionContext,
	arenaStore *statestore.ArenaStateStore,
	config *pipeline.StateStoreConfig,
) error {
	// Load current state (or create new)
	state, err := arenaStore.Load(execCtx.Context, config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return err
	}

	// Create new state if not found
	if state == nil {
		state = &runtimeStatestore.ConversationState{
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

	// Store cost info in metadata
	if execCtx.CostInfo.TotalCost > 0 {
		state.Metadata["total_cost_usd"] = execCtx.CostInfo.TotalCost
		state.Metadata["total_tokens"] = execCtx.CostInfo.InputTokens + execCtx.CostInfo.OutputTokens
	}

	// Save state with trace (Arena-specific method)
	if err := arenaStore.SaveWithTrace(execCtx.Context, state, &execCtx.Trace); err != nil {
		return err
	}

	return nil
}

func (m *arenaStateStoreSaveMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Arena StateStore save middleware doesn't process chunks
	return nil
}
