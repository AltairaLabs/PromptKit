package persist

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// setForwardTimeout sets how long a canceled turn's elements are offered
// downstream, for the length of the test.
func setForwardTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := core.CanceledForwardTimeout
	core.CanceledForwardTimeout = d
	t.Cleanup(func() { core.CanceledForwardTimeout = prev })
}

// processUnread runs s over a canceled turn's partial reply and error element
// with an output nobody reads, and returns what Process returned, or fails if
// it has not returned within a second.
func processUnread(t *testing.T, s *IncrementalSaveStage) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	partial := types.Message{Role: "assistant", Content: "Sure, I'll", FinishReason: types.FinishReasonInterrupted}
	input := make(chan core.StreamElement, 2)
	input <- core.NewMessageElement(&partial)
	input <- core.NewErrorElement(context.Canceled)
	close(input)

	done := make(chan error, 1)
	go func() { done <- s.Process(ctx, input, make(chan core.StreamElement)) }()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("Process blocked on an output nobody reads")
		return nil
	}
}

// With no store, a canceled turn whose output nobody reads does not block the
// stage: once the partial reply's offer times out, the error element is
// dropped too, and Process returns the cancellation.
func TestIncrementalSaveStage_PassthroughUnreadCanceledTurnReturns(t *testing.T) {
	setForwardTimeout(t, 5*time.Millisecond)
	assert.ErrorIs(t, processUnread(t, NewIncrementalSaveStage(nil)), context.Canceled)
}

// The saving path does not block either, and still stores the partial reply.
func TestIncrementalSaveStage_UnreadCanceledTurnReturnsAndSaves(t *testing.T) {
	setForwardTimeout(t, 5*time.Millisecond)
	store := statestore.NewMemoryStore()
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
	})

	assert.ErrorIs(t, processUnread(t, s), context.Canceled)
	state, err := store.Load(context.Background(), "c")
	require.NoError(t, err)
	require.Len(t, state.Messages, 1)
	assert.Equal(t, "Sure, I'll", state.Messages[0].Content)
}
