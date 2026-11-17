package sdk

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// gatedUpdateHook blocks its first OnSessionUpdate until gate is closed.
type gatedUpdateHook struct {
	gate    chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (h *gatedUpdateHook) Name() string                                             { return "gated-update" }
func (h *gatedUpdateHook) OnSessionStart(context.Context, hooks.SessionEvent) error { return nil }
func (h *gatedUpdateHook) OnSessionEnd(context.Context, hooks.SessionEvent) error   { return nil }
func (h *gatedUpdateHook) OnSessionUpdate(context.Context, hooks.SessionEvent) error {
	first := false
	h.once.Do(func() { first = true; close(h.entered) })
	if first {
		<-h.gate
	}
	return nil
}

// A sender blocked on a full session input, a caller waiting for the
// conversation's write lock, and the session's output goroutine completing a
// turn must not deadlock: the output goroutine never waits on the
// conversation's lock, and a send never holds it while it blocks.
func TestDuplexRealtime_TurnCompletionDoesNotDeadlockWithABlockedSendAndAWriter(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	hook := &gatedUpdateHook{gate: make(chan struct{}), entered: make(chan struct{})}
	conv, err := OpenDuplex(createTestPackFile(t, duplexLifecyclePack), "chat",
		WithProvider(mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi")),
		WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithEvalRegistry(registry), WithSessionHook(hook))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	convDrain(t, conv)

	require.NoError(t, conv.SendText(context.Background(), "first"))
	select {
	case <-hook.entered: // the output goroutine is held inside the turn's completion
	case <-time.After(5 * time.Second):
		t.Fatal("the first turn never completed")
	}

	// A regression must fail, not hang: every blocked call gives up here.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		for range 5000 {
			if conv.SendText(ctx, "more") != nil {
				return
			}
		}
	}()
	time.Sleep(300 * time.Millisecond) // the session input fills; the sender blocks
	clearDone := make(chan struct{})
	go func() {
		defer close(clearDone)
		_ = conv.Clear() // waits for the conversation's write lock
	}()
	// The blocked send holds no lock, so the writer goes ahead while the
	// session is still stalled.
	select {
	case <-clearDone:
	case <-time.After(3 * time.Second):
		close(hook.gate)
		t.Fatal("the writer waited behind a blocked send")
	}
	released := time.Now()
	close(hook.gate) // the completion goes on

	select {
	case <-senderDone:
	case <-ctx.Done():
		t.Fatal("deadlock: the sender is still blocked")
	}
	assert.Less(t, time.Since(released), 3*time.Second, "everything finished once the hook returned")
}

// slowOrderHook is lifecycleOrder whose OnSessionUpdate takes delay.
type slowOrderHook struct {
	*lifecycleOrder
	delay time.Duration
}

func (h *slowOrderHook) OnSessionUpdate(ctx context.Context, e hooks.SessionEvent) error {
	time.Sleep(h.delay)
	return h.lifecycleOrder.OnSessionUpdate(ctx, e)
}

// openOrderedIngestion opens duplexOrderPack as an ingestion session on prov
// with order as eval hook and session hook as given.
func openOrderedIngestion(t *testing.T, prov providers.Provider, order *lifecycleOrder, sh hooks.SessionHook) *Conversation {
	t.Helper()
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	conv, err := OpenDuplex(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation(), WithIngestion(perTurnIngestion),
		WithEvalRegistry(registry), WithEvalHook(order), WithSessionHook(sh))
	require.NoError(t, err)
	convDrain(t, conv)
	return conv
}

// A turn whose completion is still running when the drain gives up (its
// session hook is slow) finishes, update and turn eval, before the session
// ends.
func TestDuplex_CloseWaitsForAnInProgressTurnCompletion(t *testing.T) {
	order := &lifecycleOrder{}
	conv := openOrderedIngestion(t, &recordingTurnProvider{}, order,
		&slowOrderHook{lifecycleOrder: order, delay: 1200 * time.Millisecond})

	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{Content: "hello"}))
	require.NoError(t, closeWithin(t, conv)) // returns once drained; the completion ends the session

	want := []string{
		"start",
		"update:assistant:reply-1",
		"eval:per-turn:assistant:reply-1",
		"end",
		"eval:per-session:assistant:reply-1",
	}
	require.True(t, waitFor(func() bool { return len(order.snapshot()) == len(want) }, 5*time.Second),
		"the session never ended: %v", order.snapshot())
	assert.Equal(t, want, order.snapshot())
}

// A turn the drain cuts off (the model is still answering when the drain
// gives up) does not complete: no update, no turn eval, before or after the
// session ends.
func TestDuplex_TurnCutOffByTheDrainDoesNotComplete(t *testing.T) {
	order := &lifecycleOrder{}
	conv := openOrderedIngestion(t, &slowTurnProvider{delay: 1500 * time.Millisecond}, order, order)

	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{Content: "hello"}))
	require.NoError(t, closeWithin(t, conv))
	time.Sleep(2 * time.Second) // the model's late reply

	got := order.snapshot()
	require.GreaterOrEqual(t, len(got), 3)
	assert.Equal(t, []string{"start", "end"}, got[:2], "no session update")
	assert.Len(t, got, 3, "the session eval only: no turn eval")
	assert.Contains(t, got[2], "eval:per-session:")
}

// openControlledRealtime opens a realtime session on a mock whose replies the
// test emits, and returns the conversation and its session once open.
func openControlledRealtime(t *testing.T, opts ...Option) (*Conversation, *mock.MockStreamSession) {
	t.Helper()
	prov := mock.NewStreamingProvider("rt", "rt-model", false)
	base := []Option{WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio)}
	conv, err := OpenDuplex(createTestPackFile(t, duplexOrderPack), "chat", append(base, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	convDrain(t, conv)
	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: make([]byte, 320)},
	}))
	require.Eventually(t, func() bool { return prov.GetSession() != nil }, 5*time.Second, time.Millisecond)
	return conv, prov.GetSession()
}

// userSpeaks sends a frame of the user's audio, which starts their next turn,
// and emits its final transcript.
func userSpeaks(t *testing.T, conv *Conversation, sess *mock.MockStreamSession, text string) {
	t.Helper()
	sent := len(sess.GetChunks())
	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: make([]byte, 320)},
	}))
	require.Eventually(t, func() bool { return len(sess.GetChunks()) > sent }, 5*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // the audio reaches the session before the transcript
	emitInputTranscript(sess, text, true)
}

// The session evals Close runs see the whole stored transcript, including a
// reply cut short after the last completed turn, numbered as its turn.
func TestDuplexRealtime_SessionEvalsSeeTheTranscriptAsClosed(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	order := &lifecycleOrder{}
	evalTurns := &turnEvalLog{}
	conv, sess := openControlledRealtime(t,
		WithEvalRegistry(registry), WithEvalHook(order), WithEvalHook(evalTurns), WithSessionHook(order))

	emitInputTranscript(sess, "one", true)
	emitTurnText(sess, "hello")
	require.Eventually(t, func() bool { return len(evalTurns.sorted()) == 1 }, 5*time.Second, time.Millisecond)
	userSpeaks(t, conv, sess, "two")
	sess.EmitChunk(&providers.StreamChunk{Content: "half a", Delta: "half a"})
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, closeWithin(t, conv))

	got := order.snapshot()
	require.NotEmpty(t, got)
	assert.Equal(t, "eval:per-session:assistant:half a", got[len(got)-1])
	assert.Equal(t, []turnSeen{{Turn: 1, Last: "assistant:hello"}, {Turn: 2, Last: "assistant:half a"}},
		evalTurns.sorted(), "the session eval is numbered as the cut-short turn")
}

// afterCallTurns records the turn index of each AfterCall.
type afterCallTurns struct {
	mu    sync.Mutex
	turns []int
}

func (h *afterCallTurns) Name() string { return "after-call-turns" }
func (h *afterCallTurns) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}
func (h *afterCallTurns) AfterCall(_ context.Context, req *hooks.ProviderRequest, _ *hooks.ProviderResponse) hooks.Decision {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.turns = append(h.turns, req.TurnIndex)
	return hooks.Allow
}

func (h *afterCallTurns) snapshot() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int(nil), h.turns...)
}

// turnsOf returns the turn numbers of seen, in order.
func turnsOf(seen []turnSeen) []int {
	out := make([]int, 0, len(seen))
	for _, s := range seen {
		out = append(out, s.Turn)
	}
	sort.Ints(out)
	return out
}

// A turn is numbered by its user turn's position among the conversation's
// user turns. A turn barge-in cut short uses its number though it never
// completes, so the turn after it is turn 3; the session update and the turn
// eval both report that number.
func TestDuplexRealtime_TurnsAreNumberedByUserTurnEverywhere(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	evalLog := &turnEvalLog{}
	updates := &sessionUpdateLog{}
	guard := &afterCallTurns{}
	conv, sess := openControlledRealtime(t,
		WithEvalRegistry(registry), WithEvalHook(evalLog), WithSessionHook(updates), WithProviderHook(guard))

	stop := "stop"
	emitInputTranscript(sess, "one", true)
	emitTurnText(sess, "r1")
	require.Eventually(t, func() bool { return len(updates.snapshot()) == 1 }, 5*time.Second, time.Millisecond)
	userSpeaks(t, conv, sess, "two")
	sess.EmitChunk(&providers.StreamChunk{Content: "r2 cut", Delta: "r2 cut"})
	sess.EmitChunk(&providers.StreamChunk{Interrupted: true})
	sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
	userSpeaks(t, conv, sess, "three")
	emitTurnText(sess, "r3")
	require.Eventually(t, func() bool { return len(updates.snapshot()) == 2 }, 5*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return len(evalLog.sorted()) == 2 }, 5*time.Second, time.Millisecond)

	assert.Equal(t, []int{1, 3}, turnsOf(updates.snapshot()), "session updates")
	assert.Equal(t, []int{1, 3}, turnsOf(evalLog.sorted()), "turn evals")
}

// A resumed realtime conversation's session updates continue the persisted
// numbering, as its turn evals do.
func TestDuplexRealtime_ResumedSessionUpdatesContinueTheNumbering(t *testing.T) {
	store := statestore.NewMemoryStore()
	convPersisted(t, store, "voice-2") // one persisted user turn
	updates := &sessionUpdateLog{}
	conv, err := ResumeDuplex("voice-2", createTestPackFile(t, duplexLifecyclePack), "chat",
		WithProvider(realtimeToolScript()), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithStateStore(store), WithSessionHook(updates))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "found", nil })
	ch, err := conv.Response()
	require.NoError(t, err)

	realtimeTurn(t, conv, ch, "first")
	realtimeTurn(t, conv, ch, "second")

	assert.Equal(t, []int{2, 3}, turnsOf(updates.snapshot()))
}

// The session's output goroutine completes a turn without the conversation's
// lock: a turn completing while a caller waits for the write lock behind a
// long reader still finishes, update and turn eval dispatch included.
func TestDuplexRealtime_TurnCompletionNeverWaitsOnTheConversationLock(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	hook := &gatedUpdateHook{gate: make(chan struct{}), entered: make(chan struct{})}
	evalLog := &turnEvalLog{}
	conv, err := OpenDuplex(createTestPackFile(t, duplexLifecyclePack), "chat",
		WithProvider(mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi")),
		WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithEvalRegistry(registry), WithEvalHook(evalLog), WithSessionHook(hook))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	convDrain(t, conv)

	require.NoError(t, conv.SendText(context.Background(), "first"))
	select {
	case <-hook.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never completed")
	}
	conv.mu.RLock() // a long reader
	clearDone := make(chan struct{})
	go func() {
		defer close(clearDone)
		_ = conv.Clear() // a writer queued behind it
	}()
	time.Sleep(50 * time.Millisecond)
	close(hook.gate)

	completed := waitFor(func() bool { return len(evalLog.sorted()) == 1 }, 3*time.Second)
	conv.mu.RUnlock()
	<-clearDone
	assert.True(t, completed, "the turn's completion waited on the conversation's lock")
}
