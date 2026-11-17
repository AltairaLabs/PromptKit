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

// slowAppendStore takes a while over each AppendMessages, as a remote store
// does.
type slowAppendStore struct {
	*statestore.MemoryStore
}

func (s *slowAppendStore) AppendMessages(ctx context.Context, id string, msgs []types.Message) error {
	time.Sleep(30 * time.Millisecond)
	return s.MemoryStore.AppendMessages(ctx, id, msgs)
}

// With FlushEachTurn a turn is stored before its assistant message is passed
// on, so whoever receives the reply (a duplex session completing the turn, and
// the session hooks and evals it then runs) can read the turn back.
func TestIncrementalSaveStage_FlushStoresTheTurnBeforePassingTheReplyOn(t *testing.T) {
	store := &slowAppendStore{MemoryStore: statestore.NewMemoryStore()}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
		FlushEachTurn:    true,
	})
	input := make(chan StreamElement)
	output := make(chan StreamElement)
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), input, output) }()

	input <- newTestMsgElement("user", "hi")
	<-output
	input <- newTestMsgElement("assistant", "hello")
	<-output
	assert.Equal(t, []string{"user:hi", "assistant:hello"}, storedRoles(t, store.MemoryStore, "c"),
		"the turn is stored by the time its reply is passed on")

	close(input)
	require.NoError(t, <-done)
	assert.Equal(t, []string{"user:hi", "assistant:hello"}, storedRoles(t, store.MemoryStore, "c"),
		"and stored once")
}

// A reply cut short is passed on first and stored after: nothing reads the
// turn back on it, and the next turn should not wait on a slow store. An
// assistant message making tool calls is stored first, as a completing reply
// is: clients resolve its tool call IDs from the store.
func TestIncrementalSaveStage_FlushStoresOnlyACutShortReplyAfterPassingItOn(t *testing.T) {
	toolCall := types.Message{Role: "assistant",
		ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "lookup", Args: []byte(`{}`)}}}
	partial := types.Message{Role: "assistant", Content: "half", FinishReason: types.FinishReasonInterrupted}
	for name, tc := range map[string]struct {
		msg         types.Message
		storedFirst bool
	}{"tool call": {toolCall, true}, "cut short": {partial, false}} {
		t.Run(name, func(t *testing.T) {
			store := &slowAppendStore{MemoryStore: statestore.NewMemoryStore()}
			s := NewIncrementalSaveStage(&IncrementalSaveConfig{
				StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
				FlushEachTurn:    true,
			})
			input := make(chan StreamElement)
			output := make(chan StreamElement)
			done := make(chan error, 1)
			go func() { done <- s.Process(context.Background(), input, output) }()

			input <- newTestMsgElement("user", "hi")
			<-output
			m := tc.msg
			input <- NewMessageElement(&m)
			<-output
			if tc.storedFirst {
				assert.Len(t, storedRoles(t, store.MemoryStore, "c"), 2, "stored before it is passed on")
			} else {
				assert.Empty(t, storedRoles(t, store.MemoryStore, "c"), "passed on before it is stored")
			}
			require.Eventually(t, func() bool { return len(storedRoles(t, store.MemoryStore, "c")) == 2 },
				time.Second, time.Millisecond, "stored mid-session")

			close(input)
			require.NoError(t, <-done)
			assert.Len(t, storedRoles(t, store.MemoryStore, "c"), 2, "and stored once")
		})
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

// A tool round's assistant message is stored when it passes, but the turn is
// indexed once, whole, when its reply completes it, and nothing is stored
// twice.
func TestIncrementalSaveStage_FlushIndexesAToolTurnOnceWhenItCompletes(t *testing.T) {
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

	call := types.Message{Role: "assistant", Content: "checking",
		ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "lookup", Args: []byte(`{}`)}}}
	result := types.Message{Role: "tool", Content: "found",
		ToolResult: &types.MessageToolResult{ID: "c1", Name: "lookup"}}
	input <- newTestMsgElement("user", "ask")
	<-output
	input <- NewMessageElement(&call)
	<-output
	assert.Equal(t, []string{"user:ask", "assistant:checking"}, storedRoles(t, store, "c"),
		"the tool call is stored before it is passed on")
	assert.Empty(t, idx.snapshot(), "a turn waiting on its tool call is not indexed")

	input <- NewMessageElement(&result)
	<-output
	input <- newTestMsgElement("assistant", "the answer")
	<-output
	require.Eventually(t, func() bool { return len(idx.snapshot()) == 4 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"ask", "checking", "found", "the answer"}, idx.snapshot(), "the turn, indexed once")

	close(input)
	require.NoError(t, <-done)
	assert.Equal(t, []string{"user:ask", "assistant:checking", "tool:found", "assistant:the answer"},
		storedRoles(t, store, "c"), "each message stored once")
	assert.Len(t, idx.snapshot(), 4, "and indexed once")
}

// downAppendStore fails every append.
type downAppendStore struct {
	*statestore.MemoryStore
}

func (s *downAppendStore) AppendMessages(context.Context, string, []types.Message) error {
	return errors.New("store unavailable")
}

// With FlushEachTurn the element that ends a turn says whether the turn was
// stored: the completing reply, and an EndOfTurn boundary after it. A tool
// round's message says nothing.
func TestIncrementalSaveStage_FlushMarksWhetherTheTurnWasStored(t *testing.T) {
	stored := func(e StreamElement) string {
		if e.Meta.TurnStored == nil {
			return "unmarked"
		}
		if *e.Meta.TurnStored {
			return "stored"
		}
		return "not stored"
	}
	call := types.Message{Role: "assistant",
		ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "lookup", Args: []byte(`{}`)}}}
	for name, tc := range map[string]struct {
		store statestore.Store
		want  []string
	}{
		"write succeeds": {statestore.NewMemoryStore(), []string{"unmarked", "unmarked", "stored", "stored"}},
		"a failed write retried": {&flakyAppendStore{MemoryStore: statestore.NewMemoryStore()},
			[]string{"unmarked", "unmarked", "stored", "stored"}},
		"write fails": {&downAppendStore{MemoryStore: statestore.NewMemoryStore()},
			[]string{"unmarked", "unmarked", "not stored", "not stored"}},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewIncrementalSaveStage(&IncrementalSaveConfig{
				StateStoreConfig: &pipeline.StateStoreConfig{Store: tc.store, ConversationID: "c"},
				FlushEachTurn:    true,
			})
			in := make(chan StreamElement, 4)
			out := make(chan StreamElement, 4)
			for _, e := range []StreamElement{
				newTestMsgElement("user", "hi"), NewMessageElement(&call),
				newTestMsgElement("assistant", "done"), NewEndOfTurnElement(),
			} {
				in <- e
			}
			close(in)
			_ = s.Process(context.Background(), in, out) // a store that is down fails the final save too
			var got []string
			for e := range out {
				got = append(got, stored(e))
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
