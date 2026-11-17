package sdk

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
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

// closingHook records the lifecycle into order and closes conv from its
// first OnSessionUpdate.
type closingHook struct {
	order *lifecycleOrder
	conv  atomic.Pointer[Conversation]
	once  sync.Once
}

func (h *closingHook) Name() string { return "closing" }
func (h *closingHook) OnSessionStart(ctx context.Context, e hooks.SessionEvent) error {
	return h.order.OnSessionStart(ctx, e)
}
func (h *closingHook) OnSessionEnd(ctx context.Context, e hooks.SessionEvent) error {
	return h.order.OnSessionEnd(ctx, e)
}
func (h *closingHook) OnSessionUpdate(ctx context.Context, e hooks.SessionEvent) error {
	_ = h.order.OnSessionUpdate(ctx, e)
	h.once.Do(func() { _ = h.conv.Load().Close() })
	return nil
}

// kinds reduces lifecycle events to their kind (update, eval:id, ...).
func kinds(events []string) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		parts := strings.SplitN(e, ":", 3)
		if parts[0] == "eval" {
			out = append(out, parts[0]+":"+parts[1])
			continue
		}
		out = append(out, parts[0])
	}
	return out
}

// wantCloseFromHook is the lifecycle when OnSessionUpdate closes the
// conversation: the turn finishes, then the session ends.
var wantCloseFromHook = []string{"start", "update", "eval:per-turn", "end", "eval:per-session"}

// greetingProvider is a realtime provider whose session greets the caller as
// soon as it opens, before any input.
type greetingProvider struct {
	*mock.StreamingProvider
}

func (p *greetingProvider) CreateStreamSession(
	ctx context.Context, req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	sess, err := p.StreamingProvider.CreateStreamSession(ctx, req)
	if err != nil {
		return nil, err
	}
	ms := sess.(*mock.MockStreamSession)
	go emitTurnText(ms, "hello there")
	return ms, nil
}

// A model that speaks first completes a turn as soon as the session opens: it
// is numbered (no user turn yet: 0) and its session update sees the greeting.
func TestDuplexRealtime_GreetingTurnSeesItsTranscript(t *testing.T) {
	updates := &sessionUpdateLog{}
	conv, err := OpenDuplex(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(&greetingProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false)}),
		WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio), WithSessionHook(updates))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	convDrain(t, conv)
	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: make([]byte, 320)},
	}))

	require.Eventually(t, func() bool { return len(updates.snapshot()) == 1 }, 5*time.Second, time.Millisecond)
	assert.Equal(t, []turnSeen{{Turn: 0, Last: "assistant:hello there"}}, updates.snapshot())
}

// With no session hooks or evals a completed turn is still numbered as the
// transcript numbers it: a resumed conversation's turn continues the count.
func TestConversation_TurnIsNumberedWithoutHooksOrEvals(t *testing.T) {
	store := statestore.NewMemoryStore()
	convPersisted(t, store, "text-1") // one persisted user turn
	conv, err := Resume("text-1", createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)), WithSkipSchemaValidation(),
		WithStateStore(store), WithEvalsDisabled())
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })

	_, err = conv.Send(context.Background(), "again")
	require.NoError(t, err)
	assert.Equal(t, 2, conv.sessionHooks.TurnIndex())
}

// Complete loads the transcript once for the session and conversation evals.
func TestConversation_CompleteLoadsTheTranscriptOnce(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	store := &countingStore{MemoryStore: statestore.NewMemoryStore()}
	pack := `{
		"id": "complete-once", "version": "1.0.0",
		"evals": [
			{"id": "sess", "type": "turn_align_stub", "trigger": "on_session_complete"},
			{"id": "conv", "type": "turn_align_stub", "trigger": "on_conversation_complete"}
		],
		"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0", "system_template": "You help."}}
	}`
	evalLog := &turnEvalLog{}
	conv, err := Open(createTestPackFile(t, pack), "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)), WithSkipSchemaValidation(),
		WithStateStore(store), WithEvalRegistry(registry), WithEvalHook(evalLog))
	require.NoError(t, err)
	_, err = conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	before := store.loads.Load()
	require.NoError(t, within(t, "Complete", conv.Complete))
	assert.Equal(t, int32(1), store.loads.Load()-before)
	assert.Len(t, evalLog.sorted(), 2, "both evals ran")
}

// openReorderedRealtime is openControlledRealtime on a session whose input
// transcripts arrive late and are reordered (as on OpenAI Realtime).
func openReorderedRealtime(t *testing.T, opts ...Option) (*Conversation, *mock.MockStreamSession) {
	t.Helper()
	cfg := *convDuplexAudio
	cfg.Metadata = map[string]any{"input_transcript_ordering": "reorder"}
	prov := mock.NewStreamingProvider("rt", "rt-model", false)
	base := []Option{WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(&cfg)}
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

// On a session whose user transcripts arrive late and are reordered ahead of
// the reply they answer, the session updates and turn evals number a turn as
// the stored transcript does: the late transcript is that reply's turn,
// whether it lands while the reply streams, after it completes, or never (the
// placeholder).
func TestDuplexRealtime_LateTranscriptNumbersAgreeWithTheStoredTranscript(t *testing.T) {
	stop := "stop"
	shapes := map[string]func(t *testing.T, conv *Conversation, sess *mock.MockStreamSession, guard *afterCallTurns){
		"transcript while the reply streams": func(t *testing.T, conv *Conversation, sess *mock.MockStreamSession, _ *afterCallTurns) {
			sess.EmitChunk(&providers.StreamChunk{Content: "r1", Delta: "r1"})
			emitInputTranscript(sess, "one", true)
			sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
		},
		"transcript after the reply": func(t *testing.T, conv *Conversation, sess *mock.MockStreamSession, guard *afterCallTurns) {
			emitTurnText(sess, "r1")
			// The stage has completed the reply; the reorder stage holds it for
			// the transcript.
			require.Eventually(t, func() bool { return len(guard.snapshot()) == 1 }, 5*time.Second, time.Millisecond)
			userSpeaks(t, conv, sess, "one")
		},
		"no transcript": func(t *testing.T, conv *Conversation, sess *mock.MockStreamSession, _ *afterCallTurns) {
			emitTurnText(sess, "r1")
		},
	}
	for name, firstTurn := range shapes {
		t.Run(name, func(t *testing.T) {
			registry := evals.NewEvalTypeRegistry()
			registry.Register(&alwaysPassEvalHandler{})
			evalLog := &turnEvalLog{}
			updates := &sessionUpdateLog{}
			guard := &afterCallTurns{}
			conv, sess := openReorderedRealtime(t,
				WithEvalRegistry(registry), WithEvalHook(evalLog), WithSessionHook(updates), WithProviderHook(guard))

			firstTurn(t, conv, sess, guard)
			require.Eventually(t, func() bool { return len(updates.snapshot()) == 1 }, 8*time.Second, time.Millisecond)
			userSpeaks(t, conv, sess, "two")
			emitTurnText(sess, "r2")
			require.Eventually(t, func() bool { return len(updates.snapshot()) == 2 }, 8*time.Second, time.Millisecond)
			require.Eventually(t, func() bool { return len(evalLog.sorted()) == 2 }, 5*time.Second, time.Millisecond)

			assert.Equal(t, []int{1, 2}, turnsOf(updates.snapshot()), "session updates")
			assert.Equal(t, []int{1, 2}, turnsOf(evalLog.sorted()), "turn evals")
		})
	}
}
