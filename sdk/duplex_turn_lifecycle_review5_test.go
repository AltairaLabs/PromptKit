package sdk

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A realtime session whose provider reports no user transcripts still runs
// every turn's lifecycle: a turn is the reply the session completes, not a
// new user-turn number.
func TestDuplexRealtime_EveryTurnRunsWithoutUserTranscripts(t *testing.T) {
	updates := &sessionUpdateLog{}
	_, sess := openControlledRealtime(t, WithSessionHook(updates))

	emitTurnText(sess, "r1")
	require.Eventually(t, func() bool { return len(updates.snapshot()) == 1 }, 5*time.Second, time.Millisecond)
	emitTurnText(sess, "r2")
	require.Eventually(t, func() bool { return len(updates.snapshot()) == 2 }, 5*time.Second, time.Millisecond)

	assert.Equal(t, []turnSeen{{Turn: 0, Last: "assistant:r1"}, {Turn: 0, Last: "assistant:r2"}}, updates.snapshot())
}

// A turn after Clear runs its lifecycle, though the cleared conversation
// numbers it as an earlier turn was.
func TestConversation_TurnAfterClearRuns(t *testing.T) {
	updates := &sessionUpdateLog{}
	conv, err := Open(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)), WithSkipSchemaValidation(),
		WithSessionHook(updates))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })

	_, err = conv.Send(context.Background(), "one")
	require.NoError(t, err)
	require.NoError(t, conv.Clear())
	_, err = conv.Send(context.Background(), "two")
	require.NoError(t, err)

	turns := updates.snapshot()
	require.Len(t, turns, 2)
	assert.Equal(t, []int{1, 1}, []int{turns[0].Turn, turns[1].Turn})
}

// nthWriteFailsStore fails its n-th append.
type nthWriteFailsStore struct {
	*statestore.MemoryStore
	n     int32
	calls atomic.Int32
}

func (s *nthWriteFailsStore) AppendMessages(ctx context.Context, id string, msgs []types.Message) error {
	if s.calls.Add(1) == s.n {
		return assert.AnError
	}
	return s.MemoryStore.AppendMessages(ctx, id, msgs)
}

// A fork's turn whose write fails runs no lifecycle: in particular it does
// not run one for the transcript the fork copied from its parent.
func TestDuplexRealtime_ForkTurnWhoseWriteFailsRunsNothing(t *testing.T) {
	updates := &sessionUpdateLog{}
	store := &nthWriteFailsStore{MemoryStore: statestore.NewMemoryStore(), n: 2}
	conv, err := OpenDuplex(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hey")),
		WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio), WithStateStore(store),
		WithSessionHook(updates))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	ch, err := conv.Response()
	require.NoError(t, err)
	realtimeTurn(t, conv, ch, "parent turn") // write 1 succeeds
	require.Len(t, updates.snapshot(), 1)

	fork, err := conv.Fork()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, fork) })
	fch, err := fork.Response()
	require.NoError(t, err)
	realtimeTurn(t, fork, fch, "fork turn") // write 2 fails

	assert.Len(t, updates.snapshot(), 1, "the fork's unwritten turn ran nothing")
}

// A Close made from a session hook leaves the end to that turn's completion,
// which drains the session first: a turn the drain completes still runs its
// lifecycle before the session ends.
func TestDuplexRealtime_DeferredEndDrainsBeforeEndingTurns(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	order := &lifecycleOrder{}
	hook := &timedClosingHook{order: order}
	conv, sess := openControlledRealtime(t, WithEvalRegistry(registry), WithEvalHook(order), WithSessionHook(hook))
	hook.conv.Store(conv)

	go func() {
		emitTurnText(sess, "r1")
		emitTurnText(sess, "r2") // completed by the drain
	}()
	require.True(t, waitFor(func() bool {
		for _, e := range order.snapshot() {
			if e == "end" {
				return true
			}
		}
		return false
	}, 8*time.Second), "the session never ended: %v", order.snapshot())

	got := order.snapshot()
	updates := 0
	for _, e := range got {
		if len(e) > 6 && e[:7] == "update:" {
			updates++
		}
		if e == "end" {
			break
		}
	}
	assert.Equal(t, 2, updates, "both turns ran before the session ended: %v", got)
}

// gatedLoadStore blocks the gateAt-th Load until release is closed.
type gatedLoadStore struct {
	*statestore.MemoryStore
	loads   atomic.Int32
	gateAt  atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (s *gatedLoadStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
	if s.loads.Add(1) == s.gateAt.Load() {
		close(s.entered)
		<-s.release
	}
	return s.MemoryStore.Load(ctx, id)
}

// A Close made while a turn's completion is loading its transcript (no
// session hook executing) waits for that completion, then ends the session:
// when it returns, the turn eval, the session end and the session eval have
// run.
func TestConversation_CloseDuringATurnsTranscriptLoadWaits(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	order := &lifecycleOrder{}
	store := &gatedLoadStore{MemoryStore: statestore.NewMemoryStore(),
		entered: make(chan struct{}), release: make(chan struct{})}
	conv, err := Open(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)), WithSkipSchemaValidation(),
		WithStateStore(store), WithEvalRegistry(registry), WithEvalHook(order), WithSessionHook(order))
	require.NoError(t, err)
	// A Send loads once in its pipeline, then once to complete the turn.
	store.gateAt.Store(store.loads.Load() + 2)

	go func() { _, _ = conv.Send(context.Background(), "hello") }()
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never completed")
	}
	closed := make(chan error, 1)
	go func() { closed <- conv.Close() }()
	select {
	case <-closed:
		t.Fatal("Close returned while the turn's completion was still loading")
	case <-time.After(100 * time.Millisecond):
	}
	close(store.release)
	require.NoError(t, within(t, "Close", func() error { return <-closed }))

	assert.Equal(t, []string{"start", "update", "eval:per-turn", "end", "eval:per-session"}, kinds(order.snapshot()))
}
