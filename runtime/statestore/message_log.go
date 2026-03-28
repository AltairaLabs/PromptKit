package statestore

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MessageLog provides sequence-based message persistence with idempotent append.
// Stores that implement this interface enable per-round write-through during
// tool loops, so messages survive process crashes without waiting for the
// end-of-pipeline save stage.
//
// The sequence number is the message index in the conversation (0-based).
// AppendMessages with startSeq < current length skips already-persisted messages,
// making retries safe.
type MessageLog interface {
	// LogAppend appends messages starting at the given sequence number.
	// If startSeq < current message count, the first (current - startSeq) input
	// messages are skipped (idempotent deduplication). Returns the new total count.
	LogAppend(ctx context.Context, id string, startSeq int, messages []types.Message) (int, error)

	// LogLoad returns messages for the conversation.
	// If recent > 0, returns only the last N messages.
	// If recent == 0, returns all messages.
	// Returns an empty slice (not an error) if the conversation doesn't exist.
	LogLoad(ctx context.Context, id string, recent int) ([]types.Message, error)

	// LogLen returns the total message count for the conversation.
	// Returns 0 (not an error) if the conversation doesn't exist.
	LogLen(ctx context.Context, id string) (int, error)
}
