package middleware

import (
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// stateStoreLoadMiddleware loads conversation history from StateStore.
type stateStoreLoadMiddleware struct {
	config *pipeline.StateStoreConfig
}

// StateStoreLoadMiddleware loads conversation history from StateStore.
// It should be placed FIRST in the pipeline, before any other middleware.
// If the conversation doesn't exist, it starts with an empty history.
func StateStoreLoadMiddleware(config *pipeline.StateStoreConfig) pipeline.Middleware {
	return &stateStoreLoadMiddleware{config: config}
}

// Process loads conversation history from the state store and prepends it to ExecutionContext.Messages.
// This middleware should be placed first in the pipeline before any other middleware.
// If the conversation doesn't exist, it starts with an empty history.
func (m *stateStoreLoadMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Load state before continuing to next middleware
	// Skip if no config provided (no-op)
	if m.config != nil && m.config.Store != nil {
		if err := m.loadAndApplyState(execCtx); err != nil {
			return err
		}
	}

	// Continue to next middleware
	return next()
}

// loadAndApplyState loads conversation state and applies it to execution context
func (m *stateStoreLoadMiddleware) loadAndApplyState(execCtx *pipeline.ExecutionContext) error {
	// Type assert to statestore.Store
	store, ok := m.config.Store.(statestore.Store)
	if !ok {
		return fmt.Errorf("state store load: invalid store type")
	}

	// Load conversation history from StateStore
	state, err := store.Load(execCtx.Context, m.config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return fmt.Errorf("state store load: failed to load state: %w", err)
	}

	// Apply loaded state to context
	m.applyStateToContext(execCtx, state)
	m.storeMetadata(execCtx)

	return nil
}

// applyStateToContext prepends loaded messages to execution context
func (m *stateStoreLoadMiddleware) applyStateToContext(
	execCtx *pipeline.ExecutionContext,
	state *statestore.ConversationState,
) {
	// If conversation exists, prepend history to messages
	if state != nil && len(state.Messages) > 0 {
		// Mark all loaded messages with Source="statestore"
		for i := range state.Messages {
			state.Messages[i].Source = "statestore"
		}
		execCtx.Messages = append(state.Messages, execCtx.Messages...)

		if execCtx.EventEmitter != nil {
			execCtx.EventEmitter.StateLoaded(m.config.ConversationID, len(state.Messages))
		}
	}
}

// storeMetadata stores conversation metadata for other middleware
func (m *stateStoreLoadMiddleware) storeMetadata(execCtx *pipeline.ExecutionContext) {
	if execCtx.Metadata == nil {
		execCtx.Metadata = make(map[string]interface{})
	}
	execCtx.Metadata["conversation_id"] = m.config.ConversationID
	if m.config.UserID != "" {
		execCtx.Metadata["user_id"] = m.config.UserID
	}

	// Copy any additional metadata from config
	for k, v := range m.config.Metadata {
		execCtx.Metadata[k] = v
	}
}

// StreamChunk is a no-op for state store load middleware as it doesn't process stream chunks.
func (m *stateStoreLoadMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// StateStore load middleware doesn't process chunks
	return nil
}
