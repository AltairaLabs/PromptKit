package middleware

import (
	"errors"
	"fmt"
	"time"

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

	// Prepend system prompt as the first message if present
	// This ensures the system prompt is visible in Arena results
	if execCtx.SystemPrompt != "" {
		textContent := execCtx.SystemPrompt

		// Determine timestamp for system message
		var timestamp time.Time
		if len(execCtx.Messages) > 0 {
			timestamp = execCtx.Messages[0].Timestamp
		} else {
			timestamp = time.Now()
		}

		systemMsg := types.Message{
			Role:    "system",
			Content: execCtx.SystemPrompt,
			Parts: []types.ContentPart{
				{
					Type: "text",
					Text: &textContent,
				},
			},
			Timestamp: timestamp,
		}

		// Create messages array with system message first
		state.Messages = make([]types.Message, 0, len(execCtx.Messages)+1)
		state.Messages = append(state.Messages, systemMsg)
		state.Messages = append(state.Messages, execCtx.Messages...)
	} else {
		// No system prompt, just copy messages
		state.Messages = make([]types.Message, len(execCtx.Messages))
		copy(state.Messages, execCtx.Messages)
	}

	// Copy execution metadata (overwrites state metadata)
	for k, v := range execCtx.Metadata {
		state.Metadata[k] = v
	}

	// Store cost info in metadata
	if execCtx.CostInfo.TotalCost > 0 {
		state.Metadata["total_cost_usd"] = execCtx.CostInfo.TotalCost
		state.Metadata["total_tokens"] = execCtx.CostInfo.InputTokens + execCtx.CostInfo.OutputTokens
	}

	// Store system prompt in metadata for Arena results (for backwards compatibility)
	if execCtx.SystemPrompt != "" {
		state.Metadata["system_prompt"] = execCtx.SystemPrompt
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
