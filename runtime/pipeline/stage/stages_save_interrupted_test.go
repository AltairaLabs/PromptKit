package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func interruptedReply(content string) types.Message {
	return types.Message{Role: "assistant", Content: content, FinishReason: types.FinishReasonInterrupted}
}

// TestIncrementalSaveStage_IndexSkipsInterruptedReply checks an interrupted
// fragment is saved but never indexed, so retrieval cannot return it to model
// context. The messages around it keep their turn positions.
func TestIncrementalSaveStage_IndexSkipsInterruptedReply(t *testing.T) {
	mockIndex := &mockMessageIndexForSave{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: statestore.NewMemoryStore(), ConversationID: "c"},
		MessageIndex:     mockIndex,
	})

	user := types.Message{Role: "user", Content: "q1"}
	partial := interruptedReply("half an ans")
	next := types.Message{Role: "user", Content: "q2"}
	runTestStage(t, s, []StreamElement{
		NewMessageElement(&user), NewMessageElement(&partial), NewMessageElement(&next),
	})

	require.Len(t, mockIndex.indexCalls, 2)
	assert.Equal(t, "q1", mockIndex.indexCalls[0].message.Content)
	assert.Equal(t, 0, mockIndex.indexCalls[0].turnIndex)
	assert.Equal(t, "q2", mockIndex.indexCalls[1].message.Content)
	assert.Equal(t, 2, mockIndex.indexCalls[1].turnIndex, "the skipped reply keeps its turn position")
}

// recordingSummarizer records the batch it was asked to summarize.
type recordingSummarizer struct {
	batch []types.Message
}

func (r *recordingSummarizer) Summarize(_ context.Context, msgs []types.Message) (string, error) {
	r.batch = msgs
	return "summary", nil
}

// TestIncrementalSaveStage_SummaryExcludesInterruptedReply checks a summary,
// which replaces its turns in model context, is not built from a fragment.
func TestIncrementalSaveStage_SummaryExcludesInterruptedReply(t *testing.T) {
	store := statestore.NewMemoryStore()
	summarizer := &recordingSummarizer{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig:   &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
		Summarizer:         summarizer,
		SummarizeThreshold: 2,
		SummarizeBatchSize: 3,
	})

	u1 := types.Message{Role: "user", Content: "q1"}
	partial := interruptedReply("half an ans")
	u2 := types.Message{Role: "user", Content: "q2"}
	runTestStage(t, s, []StreamElement{
		NewMessageElement(&u1), NewMessageElement(&partial), NewMessageElement(&u2),
	})

	require.Len(t, summarizer.batch, 2, "the fragment must not be summarized")
	for _, m := range summarizer.batch {
		assert.False(t, m.IsInterrupted())
	}
	summaries, err := store.LoadSummaries(context.Background(), "c")
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, 3, summaries[0].EndTurn, "the summary still covers the fragment's turn")
}

// hangingStore never finishes an append until its context ends.
type hangingStore struct {
	*statestore.MemoryStore
}

func (h hangingStore) AppendMessages(ctx context.Context, _ string, _ []types.Message) error {
	<-ctx.Done()
	return ctx.Err()
}

// TestIncrementalSaveStage_CanceledSaveIsBounded checks a canceled turn's save
// cannot hold the caller forever when the store hangs, and skips the model
// calls indexing would make.
func TestIncrementalSaveStage_CanceledSaveIsBounded(t *testing.T) {
	prev := canceledSaveTimeout
	canceledSaveTimeout = 20 * time.Millisecond
	t.Cleanup(func() { canceledSaveTimeout = prev })

	mockIndex := &mockMessageIndexForSave{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store: hangingStore{statestore.NewMemoryStore()}, ConversationID: "c",
		},
		MessageIndex: mockIndex,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	input := make(chan StreamElement, 1)
	msg := types.Message{Role: "user", Content: "q"}
	input <- NewMessageElement(&msg)
	close(input)
	output := make(chan StreamElement, 1)

	done := make(chan error, 1)
	go func() { done <- s.Process(ctx, input, output) }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded, "the bounded save must give up, not hang")
	case <-time.After(5 * time.Second):
		t.Fatal("a canceled turn's save hung on the store")
	}
	assert.Empty(t, mockIndex.indexCalls, "a canceled turn must not run indexing")
}

// TestIncrementalSaveStage_CanceledTurnStillSaves checks the store append runs
// for a canceled turn and the cancellation is returned after it.
func TestIncrementalSaveStage_CanceledTurnStillSaves(t *testing.T) {
	store := statestore.NewMemoryStore()
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	input := make(chan StreamElement, 2)
	user := types.Message{Role: "user", Content: "q"}
	partial := interruptedReply("half")
	input <- NewMessageElement(&user)
	input <- NewMessageElement(&partial)
	close(input)
	output := make(chan StreamElement, 2)

	require.ErrorIs(t, s.Process(ctx, input, output), context.Canceled)
	state, err := store.Load(context.Background(), "c")
	require.NoError(t, err)
	require.Len(t, state.Messages, 2)
	assert.True(t, state.Messages[1].IsInterrupted())
}
