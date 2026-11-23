package persist

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// otherWriterStore is a MemoryStore shared with another writer on the same
// conversation. The other writer's next message lands just before the store
// next answers a LoadRecentMessages, which is between the stage's
// MessageCount and its read of the tail.
type otherWriterStore struct {
	*statestore.MemoryStore
	pending []types.Message
}

func (s *otherWriterStore) LoadRecentMessages(ctx context.Context, id string, n int) ([]types.Message, error) {
	if len(s.pending) > 0 {
		next := s.pending[0]
		s.pending = s.pending[1:]
		if err := s.MemoryStore.AppendMessages(ctx, id, []types.Message{next}); err != nil {
			return nil, err
		}
	}
	return s.MemoryStore.LoadRecentMessages(ctx, id, n)
}

// seededOtherWriterStore holds numberedMessages(n) in conversation "conv-ms".
func seededOtherWriterStore(t *testing.T, n int) *otherWriterStore {
	t.Helper()
	store := &otherWriterStore{MemoryStore: statestore.NewMemoryStore()}
	require.NoError(t, store.AppendMessages(context.Background(), "conv-ms", numberedMessages(n)))
	return store
}

// A message another writer appends between the count and the read shifts the
// tail the stage reads by one. A summary built from it would be labeled turns
// 0-4 while holding messages 1-4, and message 0 would never be summarized. No
// summary is written from that read; the next turn summarizes turns 0-4 from
// messages 0-3.
func TestIncrementalSaveStage_ConcurrentAppendNeverMislabelsASummary(t *testing.T) {
	store := seededOtherWriterStore(t, 4)
	store.pending = []types.Message{{Role: "user", Content: "other writer"}}
	summarizer := &recordingSummarizer{}
	stage := func() *IncrementalSaveStage { return summarizingStage(store, summarizer, 5, 4) }

	runTestStage(t, stage(), messageElements([]types.Message{
		{Role: "user", Content: "turn 1 q"}, {Role: "assistant", Content: "turn 1 a"},
	}))

	summaries, err := store.LoadSummaries(context.Background(), "conv-ms")
	require.NoError(t, err)
	assert.Empty(t, summaries, "no summary from a tail the other writer shifted")
	assert.Nil(t, summarizer.batch, "the summarizer is not called")

	runTestStage(t, stage(), messageElements([]types.Message{
		{Role: "user", Content: "turn 2 q"}, {Role: "assistant", Content: "turn 2 a"},
	}))

	summaries, err = store.LoadSummaries(context.Background(), "conv-ms")
	require.NoError(t, err)
	assert.Equal(t, []statestore.Summary{{StartTurn: 0, EndTurn: 4, Content: "summary"}}, summaries)
	assert.Equal(t, []string{"msg 0", "msg 1", "msg 2", "msg 3"}, contents(summarizer.batch))
}

// positionIndex records the transcript position each message is indexed at.
type positionIndex struct {
	recordingIndex
	at map[string]int
}

func (p *positionIndex) Index(ctx context.Context, convID string, turn int, m types.Message) error {
	if p.at == nil {
		p.at = make(map[string]int)
	}
	p.at[m.Content] = turn
	return p.recordingIndex.Index(ctx, convID, turn, m)
}

// appendAfterStore is a MemoryStore whose other writer appends right after
// the stage's own append, so the turn's messages are no longer the last ones
// when the stage indexes them.
type appendAfterStore struct {
	*statestore.MemoryStore
	other []types.Message
}

func (s *appendAfterStore) AppendMessages(ctx context.Context, id string, msgs []types.Message) error {
	if err := s.MemoryStore.AppendMessages(ctx, id, msgs); err != nil {
		return err
	}
	other := s.other
	s.other = nil
	if len(other) == 0 {
		return nil
	}
	return s.MemoryStore.AppendMessages(ctx, id, other)
}

// A turn's messages are indexed at the positions they are stored at, even when
// another writer appended after them, and never at the other writer's.
func TestIncrementalSaveStage_IndexesTheTurnWhereItWasStored(t *testing.T) {
	store := &appendAfterStore{MemoryStore: statestore.NewMemoryStore()}
	require.NoError(t, store.MemoryStore.AppendMessages(context.Background(), "c", numberedMessages(2)))
	store.other = []types.Message{{Role: "user", Content: "other writer"}}
	idx := &positionIndex{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
		MessageIndex:     idx,
	})

	runTestStage(t, s, messageElements([]types.Message{
		{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"},
	}))

	assert.Equal(t, map[string]int{"q": 2, "a": 3}, idx.at)
}

// With no other writer, the turn is indexed at the tail of the transcript.
func TestIncrementalSaveStage_IndexesTheTurnAtTheTail(t *testing.T) {
	store := &appendAfterStore{MemoryStore: statestore.NewMemoryStore()}
	require.NoError(t, store.MemoryStore.AppendMessages(context.Background(), "c", numberedMessages(3)))
	idx := &positionIndex{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
		MessageIndex:     idx,
	})

	runTestStage(t, s, messageElements([]types.Message{
		{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"},
	}))

	assert.Equal(t, map[string]int{"q": 3, "a": 4}, idx.at)
}

// idlessStore is a MemoryStore that gives messages back without their ids,
// as a store that does not keep message metadata would.
type idlessStore struct {
	*statestore.MemoryStore
}

func (s *idlessStore) LoadRecentMessages(ctx context.Context, id string, n int) ([]types.Message, error) {
	msgs, err := s.MemoryStore.LoadRecentMessages(ctx, id, n)
	for i := range msgs {
		msgs[i].Meta = nil
	}
	return msgs, err
}

// A store that keeps no message ids still has the turn indexed, at the tail
// of the transcript.
func TestIncrementalSaveStage_IndexesTheTurnAtTheTailWhenTheStoreKeepsNoIDs(t *testing.T) {
	store := &idlessStore{MemoryStore: statestore.NewMemoryStore()}
	require.NoError(t, store.AppendMessages(context.Background(), "c", numberedMessages(3)))
	idx := &positionIndex{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
		MessageIndex:     idx,
	})

	runTestStage(t, s, messageElements([]types.Message{
		{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"},
	}))

	assert.Equal(t, map[string]int{"q": 3, "a": 4}, idx.at)
}

// tailSizeStore is a MemoryStore that records the largest tail it is asked
// for.
type tailSizeStore struct {
	*statestore.MemoryStore
	largest int
}

func (s *tailSizeStore) LoadRecentMessages(ctx context.Context, id string, n int) ([]types.Message, error) {
	s.largest = max(s.largest, n)
	return s.MemoryStore.LoadRecentMessages(ctx, id, n)
}

// Indexing a turn reads back only the messages stored since the turn began,
// not the conversation: a long conversation costs no more to index than a
// short one.
func TestIncrementalSaveStage_IndexingReadsBackOnlyTheTurn(t *testing.T) {
	store := &tailSizeStore{MemoryStore: statestore.NewMemoryStore()}
	require.NoError(t, store.AppendMessages(context.Background(), "c", numberedMessages(50)))
	idx := &positionIndex{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
		MessageIndex:     idx,
	})

	runTestStage(t, s, messageElements([]types.Message{
		{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"},
	}))

	assert.Equal(t, map[string]int{"q": 50, "a": 51}, idx.at)
	assert.Equal(t, 2, store.largest, "the tail read is the turn's two messages")
}

// loggedTurnStage indexes a turn whose messages, carrying ids, the provider
// stage's MessageLog already stored in store.
func loggedTurnStage(t *testing.T, store *statestore.MemoryStore, idx statestore.MessageIndex) (
	*IncrementalSaveStage, []types.Message,
) {
	t.Helper()
	turn := []types.Message{
		types.EnsureMessageID(types.Message{Role: "user", Content: "q"}),
		types.EnsureMessageID(types.Message{Role: "assistant", Content: "a"}),
	}
	require.NoError(t, store.AppendMessages(context.Background(), "c", turn))
	return NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
		MessageIndex:     idx,
		MessageLog:       store,
	}), turn
}

// A turn the MessageLog stored is indexed at the positions it holds.
func TestIncrementalSaveStage_IndexesALoggedTurnWhereItWasStored(t *testing.T) {
	store := statestore.NewMemoryStore()
	require.NoError(t, store.AppendMessages(context.Background(), "c", numberedMessages(3)))
	idx := &positionIndex{}
	s, turn := loggedTurnStage(t, store, idx)

	runTestStage(t, s, messageElements(turn))

	assert.Equal(t, map[string]int{"q": 3, "a": 4}, idx.at)
}

// A logged turn another writer appended after has its messages indexed only
// where they are found: the message the other writer's moved out of the turn's
// tail is not indexed at the other writer's position.
func TestIncrementalSaveStage_LoggedTurnIsNeverIndexedAtAnotherWritersPosition(t *testing.T) {
	store := statestore.NewMemoryStore()
	require.NoError(t, store.AppendMessages(context.Background(), "c", numberedMessages(3)))
	idx := &positionIndex{}
	s, turn := loggedTurnStage(t, store, idx)
	require.NoError(t, store.AppendMessages(context.Background(), "c",
		[]types.Message{{Role: "user", Content: "other writer"}}))

	runTestStage(t, s, messageElements(turn))

	assert.Equal(t, map[string]int{"a": 4}, idx.at)
}
