package sdk

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// vadReplyScript is how a scriptedVADProvider answers one call.
type vadReplyScript int

const (
	// vadReplyAck streams "ack" and finishes.
	vadReplyAck vadReplyScript = iota
	// vadReplyFails streams "partial" and then fails.
	vadReplyFails
	// vadReplyHangs streams "partial" and then waits for the call to be
	// canceled (a barge-in).
	vadReplyHangs
)

// scriptedVADProvider is a text model for VAD sessions that answers each call
// by the next script, then with vadReplyAck.
type scriptedVADProvider struct {
	base.Implementation

	mu      sync.Mutex
	scripts []vadReplyScript
	calls   int
	hanging chan struct{} // closed once a vadReplyHangs call has streamed
}

func newScriptedVADProvider(scripts ...vadReplyScript) *scriptedVADProvider {
	return &scriptedVADProvider{scripts: scripts, hanging: make(chan struct{})}
}

func (p *scriptedVADProvider) ID() string                   { return "scripted-vad" }
func (p *scriptedVADProvider) Model() string                { return "scripted-vad-model" }
func (p *scriptedVADProvider) SupportsStreaming() bool      { return true }
func (p *scriptedVADProvider) ShouldIncludeRawOutput() bool { return false }
func (p *scriptedVADProvider) Close() error                 { return nil }
func (p *scriptedVADProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

func (p *scriptedVADProvider) Predict(context.Context, providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{Content: "ack"}, nil
}

func (p *scriptedVADProvider) next() vadReplyScript {
	p.mu.Lock()
	defer p.mu.Unlock()
	script := vadReplyAck
	if p.calls < len(p.scripts) {
		script = p.scripts[p.calls]
	}
	p.calls++
	return script
}

func (p *scriptedVADProvider) PredictStream(
	ctx context.Context, _ providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	script := p.next()
	ch := make(chan providers.StreamChunk, 2)
	stop := "stop"
	go func() {
		defer close(ch)
		if script == vadReplyAck {
			ch <- providers.StreamChunk{Content: "ack", Delta: "ack", FinishReason: &stop}
			return
		}
		ch <- providers.StreamChunk{Content: "partial", Delta: "partial"}
		if script == vadReplyFails {
			ch <- providers.StreamChunk{Error: errors.New("stream broke")}
			return
		}
		close(p.hanging)
		<-ctx.Done()
		ch <- providers.StreamChunk{Error: ctx.Err()}
	}()
	return ch, nil
}

// openScriptedVAD opens duplexLifecyclePack's chat prompt as a VAD session on
// prov, with a per-turn eval and a session hook.
func openScriptedVAD(
	t *testing.T, prov providers.Provider, store statestore.Store, evalLog *turnEvalLog, updates *sessionUpdateLog,
) *Conversation {
	t.Helper()
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	conv, err := OpenDuplex(createTestPackFile(t, duplexLifecyclePack), "chat",
		WithProvider(prov), WithSkipSchemaValidation(),
		WithVADMode(newScriptedSTT("one", "two", "three"), newConvMockTTSService(), vadTestConfig()),
		WithStateStore(store), WithConversationID("vad-cut"),
		WithEvalRegistry(registry), WithEvalHook(evalLog), WithSessionHook(updates))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	convDrain(t, conv)
	return conv
}

// storedReplies returns the stored assistant replies as text/finish reason.
func storedReplies(t *testing.T, store statestore.Store, id string) []string {
	t.Helper()
	state, err := store.Load(context.Background(), id)
	if err != nil {
		return nil // nothing stored yet
	}
	var out []string
	for i := range state.Messages {
		if m := state.Messages[i]; m.Role == "assistant" {
			out = append(out, m.GetContent()+"/"+m.FinishReason)
		}
	}
	return out
}

// A VAD reply the model did not finish completes no turn, whether its stream
// failed or a barge-in cut it off: the cut-short reply is kept, marked
// interrupted, and only the turn that followed runs its session update and
// turn eval, as turn 2 (the cut-short turn keeps its number).
func TestDuplexVAD_CutShortReplyCompletesNoTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	t.Run("stream failed", func(t *testing.T) {
		store := statestore.NewMemoryStore()
		evalLog, updates := &turnEvalLog{}, &sessionUpdateLog{}
		conv := openScriptedVAD(t, newScriptedVADProvider(vadReplyFails), store, evalLog, updates)

		speakOneUtterance(t, conv)
		require.Eventually(t, func() bool { return len(storedReplies(t, store, "vad-cut")) == 1 },
			8*time.Second, 5*time.Millisecond, "the failed turn keeps its partial reply")
		speakOneUtterance(t, conv)
		require.Eventually(t, func() bool { return len(updates.snapshot()) >= 1 },
			8*time.Second, 5*time.Millisecond, "the next turn completes")
		require.NoError(t, closeWithin(t, conv))

		assert.Equal(t, []string{"partial/" + types.FinishReasonInterrupted, "ack/stop"},
			storedReplies(t, store, "vad-cut"))
		want := []turnSeen{{Turn: 2, Last: "assistant:ack"}}
		assert.Equal(t, want, updates.snapshot())
		assert.Equal(t, want, evalLog.sorted())
	})
	t.Run("barge-in", func(t *testing.T) {
		store := statestore.NewMemoryStore()
		evalLog, updates := &turnEvalLog{}, &sessionUpdateLog{}
		prov := newScriptedVADProvider(vadReplyHangs)
		conv := openScriptedVAD(t, prov, store, evalLog, updates)

		speakOneUtterance(t, conv)
		select {
		case <-prov.hanging:
		case <-time.After(8 * time.Second):
			t.Fatal("the first reply never started")
		}
		speakOneUtterance(t, conv) // talks over the reply
		require.Eventually(t, func() bool { return len(updates.snapshot()) >= 1 },
			8*time.Second, 5*time.Millisecond, "the turn after the barge-in completes")
		require.NoError(t, closeWithin(t, conv))

		assert.Equal(t, []string{"partial/" + types.FinishReasonInterrupted, "ack/stop"},
			storedReplies(t, store, "vad-cut"))
		want := []turnSeen{{Turn: 2, Last: "assistant:ack"}}
		assert.Equal(t, want, updates.snapshot())
		assert.Equal(t, want, evalLog.sorted())
	})
}

// ingestionTurnEvals opens an ingestion duplex session (each SendChunk text is
// a user message and a turn boundary) on store, resuming id when it holds
// turns, runs two turns and returns the turn numbers its turn evals report.
func ingestionTurnEvals(t *testing.T, store statestore.Store, id string, ingest IngestionFunc) []int {
	t.Helper()
	return ingestionTurnEvalsWith(t, store, id, ingest, func(conv *Conversation, turn int) {
		text := []string{"first turn", "second turn"}[turn]
		require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{Content: text, Source: "caller"}))
	})
}

// ingestionTurnEvalsWith is ingestionTurnEvals with send making each turn.
func ingestionTurnEvalsWith(
	t *testing.T, store statestore.Store, id string, ingest IngestionFunc, send func(conv *Conversation, turn int),
) []int {
	t.Helper()
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	evalLog := &turnEvalLog{}
	updates := &sessionUpdateLog{}
	conv, err := OpenDuplex(createTestPackFile(t, duplexLifecyclePack), "chat",
		WithProvider(&recordingTurnProvider{}), WithSkipSchemaValidation(),
		WithIngestion(ingest), WithStateStore(store), WithConversationID(id),
		WithEvalRegistry(registry), WithEvalHook(evalLog), WithSessionHook(updates))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	convDrain(t, conv)

	send(conv, 0)
	require.Eventually(t, func() bool { return len(updates.snapshot()) == 1 }, 8*time.Second, 5*time.Millisecond)
	send(conv, 1)
	require.Eventually(t, func() bool { return len(updates.snapshot()) == 2 }, 8*time.Second, 5*time.Millisecond)
	require.NoError(t, closeWithin(t, conv))

	var turns []int
	for _, s := range evalLog.sorted() {
		turns = append(turns, s.Turn)
	}
	return turns
}

// perTurnIngestion is a WithIngestion sub-graph emitting a user message and a
// turn boundary for each text input.
func perTurnIngestion(b *stage.PipelineBuilder) (string, error) {
	b.AddStage(newTurnEmitterStage("turn_emitter"))
	return "turn_emitter", nil
}

// An ingestion session's user messages pass through the pipeline's load
// stage, which counts each one onto the turn state. Its turn evals still
// number each completed turn once, on from the turns the store already held.
func TestDuplexIngestion_TurnEvalsNumberEachTurnOnce(t *testing.T) {
	t.Run("new conversation", func(t *testing.T) {
		assert.Equal(t, []int{1, 2}, ingestionTurnEvals(t, statestore.NewMemoryStore(), "ingest-new", perTurnIngestion))
	})
	t.Run("resumed conversation", func(t *testing.T) {
		store := statestore.NewMemoryStore()
		convPersisted(t, store, "ingest-resumed") // one persisted user turn
		assert.Equal(t, []int{2, 3}, ingestionTurnEvals(t, store, "ingest-resumed", perTurnIngestion))
	})
}

// A multi-track ingestion session (one speaker-labeled user message per track
// turn) numbers its turn evals once per completed turn too.
func TestDuplexMultiTrackIngestion_TurnEvalsNumberEachTurnOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns on two tracks")
	}
	ingest := MultiTrackIngestion(MultiTrackIngestionConfig{Tracks: []IngestionTrack{
		{Source: "caller", Speaker: "CUSTOMER", STT: &fixedSTT{line: "hello from caller"}},
		{Source: "agent", Speaker: "AGENT", STT: &fixedSTT{line: "hi from agent"}},
	}})
	speak := func(conv *Conversation, turn int) {
		source := []string{"caller", "agent"}[turn]
		loud := make([]byte, 4800) // 300ms of 8kHz PCM16
		for i := 0; i+1 < len(loud); i += 2 {
			loud[i+1] = 0x20
		}
		pcm := append(loud, make([]byte, 48000)...) // then 3s of silence
		for off := 0; off < len(pcm); off += bytesPerTestFrame {
			end := min(off+bytesPerTestFrame, len(pcm))
			require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
				MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: pcm[off:end], SampleRate: 8000, Channels: 1},
				Source:    source,
			}))
		}
	}
	assert.Equal(t, []int{1, 2}, ingestionTurnEvalsWith(t, statestore.NewMemoryStore(), "multitrack", ingest, speak))
}

// lifecycleOrder records the order of session and eval lifecycle events.
type lifecycleOrder struct {
	mu     sync.Mutex
	events []string
}

func (o *lifecycleOrder) add(e string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, e)
}

func (o *lifecycleOrder) snapshot() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.events...)
}

func (o *lifecycleOrder) Name() string { return "lifecycle-order" }
func (o *lifecycleOrder) OnSessionStart(context.Context, hooks.SessionEvent) error {
	o.add("start")
	return nil
}
func (o *lifecycleOrder) OnSessionUpdate(_ context.Context, e hooks.SessionEvent) error {
	o.add("update:" + lastRoleText(e.Messages))
	return nil
}
func (o *lifecycleOrder) OnSessionEnd(context.Context, hooks.SessionEvent) error {
	o.add("end")
	return nil
}
func (o *lifecycleOrder) OnEvalResult(_ context.Context, def *evals.EvalDef, ec *evals.EvalContext, _ *evals.EvalResult) {
	o.add("eval:" + def.ID + ":" + lastRoleText(ec.Messages))
}

// duplexOrderPack declares a per-turn and a session eval.
const duplexOrderPack = `{
	"id": "duplex-order",
	"version": "1.0.0",
	"evals": [
		{"id": "per-turn", "type": "turn_align_stub", "trigger": "every_turn"},
		{"id": "per-session", "type": "turn_align_stub", "trigger": "on_session_complete"}
	],
	"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0", "system_template": "You help."}}
}`

// slowTurnProvider is recordingTurnProvider taking delay over each reply.
type slowTurnProvider struct {
	recordingTurnProvider
	delay time.Duration
}

func (p *slowTurnProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	time.Sleep(p.delay)
	return p.recordingTurnProvider.Predict(ctx, req)
}

// A turn the closing drain completes runs its session update and turn eval
// before the session ends and the session evals run; a turn completing once
// the conversation is closed runs nothing.
func TestDuplex_CloseCompletesTheDrainedTurnBeforeEndingTheSession(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	order := &lifecycleOrder{}
	prov := &slowTurnProvider{delay: 100 * time.Millisecond}
	conv, err := OpenDuplex(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation(), WithIngestion(perTurnIngestion),
		WithEvalRegistry(registry), WithEvalHook(order), WithSessionHook(order))
	require.NoError(t, err)
	convDrain(t, conv)

	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{Content: "hello"}))
	require.NoError(t, closeWithin(t, conv)) // the reply is still being generated

	assert.Equal(t, []string{
		"start",
		"update:assistant:reply-1",
		"eval:per-turn:assistant:reply-1",
		"end",
		"eval:per-session:assistant:reply-1",
	}, order.snapshot())

	conv.completeTurn(context.Background())
	assert.Len(t, order.snapshot(), 5, "a turn completing after Close runs no hook and no eval")
}

// A fork of a duplex conversation runs its own session update for each turn
// its session completes, over its own transcript.
func TestDuplexRealtime_ForkRunsItsOwnTurnLifecycle(t *testing.T) {
	updates := &sessionUpdateLog{}
	conv := openRealtimeLifecycle(t, WithSessionHook(updates))
	fork, err := conv.Fork()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, fork) })
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "found", nil })
	fork.OnTool("lookup", func(map[string]any) (any, error) { return "found", nil })
	ch, err := fork.Response()
	require.NoError(t, err)

	realtimeTurn(t, fork, ch, "first")

	assert.Equal(t, []turnSeen{{Turn: 1, Last: "assistant:the answer"}}, updates.snapshot())
}

// A turn completing while Clear runs reads the conversation's session under
// its lock (the race detector reports it otherwise).
func TestDuplexRealtime_TurnCompletingDuringClearIsRaceFree(t *testing.T) {
	updates := &sessionUpdateLog{}
	conv := openRealtimeLifecycle(t, WithSessionHook(updates))
	ch, err := conv.Response()
	require.NoError(t, err)
	realtimeTurn(t, conv, ch, "first")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 50 {
			conv.completeTurn(context.Background())
		}
	}()
	for range 10 {
		require.NoError(t, conv.Clear())
	}
	wg.Wait()
	assert.Len(t, updates.snapshot(), 51, "every reported completion runs, Clear or not")
}

// countingStore counts transcript loads.
type countingStore struct {
	*statestore.MemoryStore
	loads atomic.Int32
}

func (s *countingStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
	s.loads.Add(1)
	return s.MemoryStore.Load(ctx, id)
}

// A completed turn loads its transcript once, for the session hook and the
// turn evals alike.
func TestConversation_CompletedTurnLoadsTheTranscriptOnce(t *testing.T) {
	store := &countingStore{MemoryStore: statestore.NewMemoryStore()}
	evalLog := &turnEvalLog{}
	updates := &sessionUpdateLog{}
	conv := openRealtimeLifecycle(t, WithStateStore(store), WithEvalHook(evalLog), WithSessionHook(updates))
	ch, err := conv.Response()
	require.NoError(t, err)
	realtimeTurn(t, conv, ch, "first") // its tool turn, and the session's history load

	before := store.loads.Load()
	realtimeTurn(t, conv, ch, "second")
	conv.evalMW.wait()

	assert.Equal(t, int32(1), store.loads.Load()-before)
	assert.Len(t, updates.snapshot(), 2)
	assert.Len(t, evalLog.sorted(), 2)
}

// heldTurnEvalHandler holds each eval until released.
type heldTurnEvalHandler struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (h *heldTurnEvalHandler) Type() string { return "turn_align_stub" }
func (h *heldTurnEvalHandler) Eval(context.Context, *evals.EvalContext, map[string]any) (*evals.EvalResult, error) {
	h.calls.Add(1)
	h.once.Do(func() { close(h.started) })
	<-h.release
	score := 1.0
	return &evals.EvalResult{Score: &score}, nil
}

// A turn eval dispatched while Close waits for the in-flight turn evals
// returns at once and runs nothing: it is dropped, not queued behind them.
func TestEvalDispatchDuringCloseDoesNotWaitForInFlightEvals(t *testing.T) {
	handler := &heldTurnEvalHandler{started: make(chan struct{}), release: make(chan struct{})}
	registry := evals.NewEvalTypeRegistry()
	registry.Register(handler)
	conv, err := OpenDuplex(createTestPackFile(t, duplexLifecyclePack), "chat",
		WithProvider(realtimeToolScript()), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithEvalRegistry(registry))
	require.NoError(t, err)

	conv.evalMW.dispatchTurnEvals(context.Background())
	<-handler.started
	closed := make(chan error, 1)
	go func() { closed <- conv.Close() }()
	require.Eventually(t, func() bool { return conv.evalMW.isStopped() }, 5*time.Second, time.Millisecond,
		"Close stops dispatching before it waits")

	dispatched := make(chan struct{})
	go func() {
		conv.evalMW.dispatchTurnEvals(context.Background())
		close(dispatched)
	}()
	select {
	case <-dispatched:
	case <-time.After(time.Second):
		t.Fatal("a dispatch during Close waited for the in-flight evals")
	}
	close(handler.release)
	require.NoError(t, within(t, "Close", func() error { return <-closed }))
	assert.Equal(t, int32(1), handler.calls.Load(), "the eval dispatched during Close never ran")
}

// isStopped reports whether close has stopped turn-eval dispatch.
func (em *evalMiddleware) isStopped() bool {
	em.lifeMu.Lock()
	defer em.lifeMu.Unlock()
	return em.stopped
}
