package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// errFailedToForkState is shared by the fork-with-bulk-fallback paths in
// both unarySession and duplexSession. Defined as a const so the literal
// doesn't appear three times in each file (Sonar's go:S1192 threshold).
const errFailedToForkState = "failed to fork state: %w"

// loadMessages returns the conversation's messages, treating ErrNotFound
// as an empty conversation. Conversations are created lazily on the first
// typed write, so a brand-new session may not yet exist in the store.
func loadMessages(ctx context.Context, store statestore.Store, id string) ([]types.Message, error) {
	state, err := store.Load(ctx, id)
	if err != nil {
		if errors.Is(err, statestore.ErrNotFound) {
			return []types.Message{}, nil
		}
		return nil, err
	}
	return state.Messages, nil
}

// clearSession resets the conversation by writing an empty state. Bulk
// operation — requires the store to implement BulkWriter. Stores without
// bulk-write support cannot honor Clear and the function returns an error.
func clearSession(ctx context.Context, store statestore.Store, id string) error {
	bulkWriter, ok := store.(statestore.BulkWriter)
	if !ok {
		return fmt.Errorf("session clear: store does not implement BulkWriter")
	}
	return bulkWriter.Save(ctx, &statestore.ConversationState{
		ID:       id,
		Messages: nil,
	})
}

// forkOrCreate forks the source conversation to a new ID. If the source
// has not yet been materialized (lazy-created), it falls back to creating
// an empty fork target via BulkWriter when the store supports it.
func forkOrCreate(ctx context.Context, store statestore.Store, sourceID, forkID string) error {
	if err := store.Fork(ctx, sourceID, forkID); err != nil {
		if !errors.Is(err, statestore.ErrNotFound) {
			return fmt.Errorf(errFailedToForkState, err)
		}
		// Source doesn't exist yet (lazy-created). Try to create an empty
		// fork target via BulkWriter; surface a precise error when the
		// store can't help.
		bulkWriter, ok := store.(statestore.BulkWriter)
		if !ok {
			return fmt.Errorf(
				"fork: source conversation not found and store does not implement BulkWriter for fallback: %w", err)
		}
		if saveErr := bulkWriter.Save(ctx, &statestore.ConversationState{ID: forkID}); saveErr != nil {
			return fmt.Errorf(errFailedToForkState, saveErr)
		}
	}
	return nil
}
