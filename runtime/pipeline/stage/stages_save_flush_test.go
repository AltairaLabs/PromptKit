package stage

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// flakyAppendStore fails its first AppendMessages call.
type flakyAppendStore struct {
	*statestore.MemoryStore
	calls atomic.Int32
}

func (s *flakyAppendStore) AppendMessages(ctx context.Context, id string, msgs []types.Message) error {
	if s.calls.Add(1) == 1 {
		return errors.New("store unavailable")
	}
	return s.MemoryStore.AppendMessages(ctx, id, msgs)
}

func storedRoles(t *testing.T, store statestore.Store, id string) []string {
	t.Helper()
	state, err := store.Load(context.Background(), id)
	if errors.Is(err, statestore.ErrNotFound) {
		return nil
	}
	require.NoError(t, err)
	var out []string
	for _, m := range state.Messages {
		out = append(out, m.Role+":"+m.Content)
	}
	return out
}

// With FlushEachTurn the stage persists each turn when its assistant message
// passes, while input is still open, and stores nothing twice at the end.
// Without it nothing is stored until input closes.
func TestIncrementalSaveStage_FlushEachTurn(t *testing.T) {
	for _, flush := range []bool{true, false} {
		store := statestore.NewMemoryStore()
		s := NewIncrementalSaveStage(&IncrementalSaveConfig{
			StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
			FlushEachTurn:    flush,
		})
		input := make(chan StreamElement)
		output := make(chan StreamElement, 8)
		done := make(chan error, 1)
		go func() { done <- s.Process(context.Background(), input, output) }()

		input <- newTestMsgElement("user", "hi")
		<-output
		input <- newTestMsgElement("system", "context")
		<-output
		assert.Empty(t, storedRoles(t, store, "c"), "a turn is stored when its assistant message passes, not before")
		input <- newTestMsgElement("assistant", "hello")
		<-output
		if flush {
			require.Eventually(t, func() bool { return len(storedRoles(t, store, "c")) == 3 },
				time.Second, time.Millisecond, "the turn is stored while the session is open")
		} else {
			assert.Empty(t, storedRoles(t, store, "c"), "nothing is stored before input closes")
		}
		input <- newTestMsgElement("user", "bye")
		close(input)
		require.NoError(t, <-done)
		assert.Equal(t, []string{"user:hi", "system:context", "assistant:hello", "user:bye"}, storedRoles(t, store, "c"))
	}
}

// A mid-session write that fails keeps the messages for the final save.
func TestIncrementalSaveStage_FlushFailureIsRetriedAtTheEnd(t *testing.T) {
	store := &flakyAppendStore{MemoryStore: statestore.NewMemoryStore()}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
		FlushEachTurn:    true,
	})
	results := runTestStage(t, s, []StreamElement{
		newTestMsgElement("user", "hi"), newTestMsgElement("assistant", "hello"),
	})
	require.Len(t, results, 2)
	assert.Equal(t, []string{"user:hi", "assistant:hello"}, storedRoles(t, store.MemoryStore, "c"))
}

// recordingIndex records the messages it is asked to index.
type recordingIndex struct {
	mu      sync.Mutex
	indexed []string
}

func (r *recordingIndex) Index(_ context.Context, _ string, _ int, m types.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.indexed = append(r.indexed, m.Content)
	return nil
}
func (r *recordingIndex) Search(context.Context, string, string, int) ([]statestore.IndexResult, error) {
	return nil, nil
}
func (r *recordingIndex) Delete(context.Context, string) error { return nil }
func (r *recordingIndex) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.indexed...)
}

// A flushed turn is indexed for retrieval when it is stored, not at the end.
func TestIncrementalSaveStage_FlushIndexesTheTurn(t *testing.T) {
	store := statestore.NewMemoryStore()
	idx := &recordingIndex{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
		MessageIndex:     idx,
		FlushEachTurn:    true,
	})
	input := make(chan StreamElement)
	output := make(chan StreamElement, 8)
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), input, output) }()
	input <- newTestMsgElement("user", "hi")
	input <- newTestMsgElement("assistant", "hello")
	<-output
	<-output
	require.Eventually(t, func() bool { return len(idx.snapshot()) == 2 }, time.Second, time.Millisecond,
		"the turn is indexed while the session is open")
	close(input)
	require.NoError(t, <-done)
	assert.Equal(t, []string{"hi", "hello"}, idx.snapshot())
}
