package sdk

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A Stream turn's lifecycle is done before its ChunkDone: a caller that reads
// the stream to ChunkDone and then closes the conversation finds the session
// ended, session evals run, when Close returns, even with a slow session hook.
func TestStream_CloseAfterTheStreamEndsTheSessionBeforeReturning(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	order := &lifecycleOrder{}
	conv, err := Open(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)), WithSkipSchemaValidation(),
		WithEvalRegistry(registry), WithEvalHook(order),
		WithSessionHook(&slowOrderHook{lifecycleOrder: order, delay: 100 * time.Millisecond}))
	require.NoError(t, err)

	for chunk := range conv.Stream(context.Background(), "hello") {
		require.NoError(t, chunk.Error)
		if chunk.Type == ChunkDone {
			break // the turn's end, as the caller sees it
		}
	}
	require.NoError(t, closeWithin(t, conv))

	assert.Equal(t, wantCloseFromHook, kinds(order.snapshot()))
}

// timedClosingHook records the lifecycle into order and closes conv from its
// first OnSessionUpdate, timing the Close.
type timedClosingHook struct {
	order    *lifecycleOrder
	conv     atomic.Pointer[Conversation]
	once     sync.Once
	closeDur atomic.Int64
}

func (h *timedClosingHook) Name() string { return "timed-closing" }
func (h *timedClosingHook) OnSessionStart(ctx context.Context, e hooks.SessionEvent) error {
	return h.order.OnSessionStart(ctx, e)
}
func (h *timedClosingHook) OnSessionEnd(ctx context.Context, e hooks.SessionEvent) error {
	return h.order.OnSessionEnd(ctx, e)
}
func (h *timedClosingHook) OnSessionUpdate(ctx context.Context, e hooks.SessionEvent) error {
	_ = h.order.OnSessionUpdate(ctx, e)
	h.once.Do(func() {
		start := time.Now()
		_ = h.conv.Load().Close()
		h.closeDur.Store(int64(time.Since(start)))
		h.order.add("close-returned")
	})
	return nil
}

// wantCloseInHook is the lifecycle when OnSessionUpdate closes the
// conversation: Close returns at once, the turn's completion finishes, and
// the session then ends.
var wantCloseInHook = []string{"start", "update", "close-returned", "eval:per-turn", "end", "eval:per-session"}

// A Close made from inside a session hook returns at once, and the session
// ends as soon as that hook's turn has completed; a duplex caller still
// receives the turn's reply.
func TestConversation_CloseFromASessionHookEndsAfterTheTurn(t *testing.T) {
	newRegistry := func() *evals.EvalTypeRegistry {
		r := evals.NewEvalTypeRegistry()
		r.Register(&alwaysPassEvalHandler{})
		return r
	}
	t.Run("unary", func(t *testing.T) {
		order := &lifecycleOrder{}
		hook := &timedClosingHook{order: order}
		conv, err := Open(createTestPackFile(t, duplexOrderPack), "chat",
			WithProvider(mock.NewProvider("mock", "mock-model", false)), WithSkipSchemaValidation(),
			WithEvalRegistry(newRegistry()), WithEvalHook(order), WithSessionHook(hook))
		require.NoError(t, err)
		hook.conv.Store(conv)

		require.NoError(t, within(t, "Send", func() error {
			_, sendErr := conv.Send(context.Background(), "hello")
			return sendErr
		}))
		require.True(t, waitFor(func() bool { return len(order.snapshot()) == len(wantCloseInHook) }, 5*time.Second),
			"the session never ended: %v", order.snapshot())
		assert.Equal(t, wantCloseInHook, kinds(order.snapshot()))
		assert.Less(t, time.Duration(hook.closeDur.Load()), 200*time.Millisecond, "Close returned at once")
	})
	t.Run("duplex", func(t *testing.T) {
		order := &lifecycleOrder{}
		hook := &timedClosingHook{order: order}
		conv, err := OpenDuplex(createTestPackFile(t, duplexOrderPack), "chat",
			WithProvider(mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi")),
			WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
			WithEvalRegistry(newRegistry()), WithEvalHook(order), WithSessionHook(hook))
		require.NoError(t, err)
		hook.conv.Store(conv)
		ch, err := conv.Response()
		require.NoError(t, err)

		require.NoError(t, conv.SendText(context.Background(), "hello"))
		gotReply := false
		timeout := time.After(8 * time.Second)
	read:
		for {
			select {
			case c, ok := <-ch:
				if !ok {
					break read
				}
				if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
					gotReply = true
				}
			case <-timeout:
				t.Fatal("deadlock: the session never ended")
			}
		}
		assert.True(t, gotReply, "the reply's completing chunk reached the caller")
		require.True(t, waitFor(func() bool { return len(order.snapshot()) == len(wantCloseInHook) }, 5*time.Second),
			"the session never ended: %v", order.snapshot())
		assert.Equal(t, wantCloseInHook, kinds(order.snapshot()))
		assert.Less(t, time.Duration(hook.closeDur.Load()), 200*time.Millisecond, "Close returned at once")
	})
}

// slowEvalHandler is the eval stub, taking delay over each eval.
type slowEvalHandler struct{ delay time.Duration }

func (h *slowEvalHandler) Type() string { return "turn_align_stub" }
func (h *slowEvalHandler) Eval(context.Context, *evals.EvalContext, map[string]any) (*evals.EvalResult, error) {
	time.Sleep(h.delay)
	score := 1.0
	return &evals.EvalResult{Score: &score}, nil
}

// A Complete made while no session hook is executing (the turn's eval still
// running) returns only once everything has run: the turn eval, the session
// end, the session evals and the conversation evals.
func TestConversation_CompleteWaitsForTheSessionToEnd(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&slowEvalHandler{delay: 200 * time.Millisecond})
	pack := `{
		"id": "complete-wait", "version": "1.0.0",
		"evals": [
			{"id": "per-turn", "type": "turn_align_stub", "trigger": "every_turn"},
			{"id": "per-session", "type": "turn_align_stub", "trigger": "on_session_complete"},
			{"id": "per-conv", "type": "turn_align_stub", "trigger": "on_conversation_complete"}
		],
		"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0", "system_template": "You help."}}
	}`
	order := &lifecycleOrder{}
	conv, err := Open(createTestPackFile(t, pack), "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)), WithSkipSchemaValidation(),
		WithEvalRegistry(registry), WithEvalHook(order), WithSessionHook(order))
	require.NoError(t, err)

	_, err = conv.Send(context.Background(), "hello") // its turn eval is still running
	require.NoError(t, err)
	require.NoError(t, within(t, "Complete", conv.Complete))

	assert.Equal(t, []string{"start", "update", "eval:per-turn", "end", "eval:per-session", "eval:per-conv"},
		kinds(order.snapshot()), "Complete returned before the session had ended")
}

// flakyWriteStore fails its first append, as a store with a transient fault.
type flakyWriteStore struct {
	*statestore.MemoryStore
	calls atomic.Int32
}

func (s *flakyWriteStore) AppendMessages(ctx context.Context, id string, msgs []types.Message) error {
	if s.calls.Add(1) == 1 {
		return assert.AnError
	}
	return s.MemoryStore.AppendMessages(ctx, id, msgs)
}

// A turn the store failed to write runs its lifecycle once a later write
// stores it: both turns run, in order, each numbered and seen as itself.
func TestDuplexRealtime_FailedWriteRunsTheTurnOnceStored(t *testing.T) {
	updates := &sessionUpdateLog{}
	conv, err := OpenDuplex(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hey")),
		WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithStateStore(&flakyWriteStore{MemoryStore: statestore.NewMemoryStore()}), WithSessionHook(updates))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	ch, err := conv.Response()
	require.NoError(t, err)

	realtimeTurn(t, conv, ch, "one")
	realtimeTurn(t, conv, ch, "two")

	assert.Equal(t, []turnSeen{{Turn: 1, Last: "assistant:hey"}, {Turn: 2, Last: "assistant:hey"}}, updates.snapshot())
}

// With no session hooks and no evals a completed duplex turn loads nothing.
func TestDuplexRealtime_TurnWithNothingListeningLoadsNothing(t *testing.T) {
	store := &countingStore{MemoryStore: statestore.NewMemoryStore()}
	conv, err := OpenDuplex(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hey")),
		WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio), WithStateStore(store), WithEvalsDisabled())
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	ch, err := conv.Response()
	require.NoError(t, err)

	realtimeTurn(t, conv, ch, "one") // the session's first input loads its history
	before := store.loads.Load()
	realtimeTurn(t, conv, ch, "two")
	assert.Equal(t, int32(0), store.loads.Load()-before)
}
