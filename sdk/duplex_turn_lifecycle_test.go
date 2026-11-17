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
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// duplexLifecyclePack declares one per-turn eval and a tool the model can call.
const duplexLifecyclePack = `{
	"id": "duplex-lifecycle",
	"version": "1.0.0",
	"evals": [{"id": "per-turn", "type": "turn_align_stub", "trigger": "every_turn"}],
	"prompts": {
		"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
			"system_template": "You help.", "tools": ["lookup"]}
	},
	"tools": {
		"lookup": {"name": "lookup", "description": "Look something up",
			"parameters": {"type": "object", "properties": {"q": {"type": "string"}}}}
	}
}`

// roleText renders a message as role:text.
func roleText(m *types.Message) string {
	if m.ToolResult != nil {
		return m.Role + ":" + m.ToolResult.GetTextContent()
	}
	if len(m.ToolCalls) > 0 {
		return m.Role + ":call " + m.ToolCalls[0].Name
	}
	return m.Role + ":" + m.GetContent()
}

// lastRoleText is roleText of the last message, or "" for none.
func lastRoleText(msgs []types.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	return roleText(&msgs[len(msgs)-1])
}

// turnSeen is what a turn's eval or session update saw: the turn number and
// the last message of the transcript.
type turnSeen struct {
	Turn int
	Last string
}

// turnEvalLog records each per-turn eval result.
type turnEvalLog struct {
	mu   sync.Mutex
	seen []turnSeen
}

func (l *turnEvalLog) Name() string { return "turn-eval-log" }

func (l *turnEvalLog) OnEvalResult(_ context.Context, _ *evals.EvalDef, ec *evals.EvalContext, _ *evals.EvalResult) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, turnSeen{Turn: ec.TurnIndex, Last: lastRoleText(ec.Messages)})
}

// sorted returns the results ordered by turn: turn evals run concurrently.
func (l *turnEvalLog) sorted() []turnSeen {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]turnSeen(nil), l.seen...)
	sort.Slice(out, func(i, j int) bool { return out[i].Turn < out[j].Turn })
	return out
}

// sessionUpdateLog records each OnSessionUpdate, once it has taken delay.
type sessionUpdateLog struct {
	delay   time.Duration
	mu      sync.Mutex
	updates []turnSeen
}

func (h *sessionUpdateLog) Name() string { return "session-update-log" }
func (h *sessionUpdateLog) OnSessionStart(context.Context, hooks.SessionEvent) error {
	return nil
}
func (h *sessionUpdateLog) OnSessionUpdate(_ context.Context, e hooks.SessionEvent) error {
	time.Sleep(h.delay)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updates = append(h.updates, turnSeen{Turn: e.TurnIndex, Last: lastRoleText(e.Messages)})
	return nil
}
func (h *sessionUpdateLog) OnSessionEnd(context.Context, hooks.SessionEvent) error { return nil }

func (h *sessionUpdateLog) snapshot() []turnSeen {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]turnSeen(nil), h.updates...)
}

// realtimeToolScript is a realtime model that answers the first user turn
// with a tool call, then the tool result with "the answer", then the second
// user turn with "second reply".
func realtimeToolScript() *mock.StreamingProvider {
	repo := newGateTurnRepo()
	repo.add(1, mock.Turn{Type: "tool_calls", Content: "checking",
		ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"q": "x"}}}})
	repo.add(2, mock.Turn{Type: "text", Content: "the answer"})
	repo.add(3, mock.Turn{Type: "text", Content: "second reply"})
	return mock.NewStreamingProvider("rt", "rt-model", false).
		WithAutoRespond("unused").
		WithMockResponses(repo, "default", "")
}

// openRealtimeLifecycle opens duplexLifecyclePack's chat prompt on a realtime
// session scripted by realtimeToolScript, with a per-turn eval and the
// lookup tool's handler.
func openRealtimeLifecycle(t *testing.T, opts ...Option) *Conversation {
	t.Helper()
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	base := []Option{
		WithProvider(realtimeToolScript()),
		WithSkipSchemaValidation(),
		WithStreamingConfig(convDuplexAudio),
		WithEvalRegistry(registry),
	}
	conv, err := OpenDuplex(createTestPackFile(t, duplexLifecyclePack), "chat", append(base, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "found", nil })
	return conv
}

// realtimeTurn sends text and reads the responses until the chunk that
// completes the assistant turn, then waits for the turn's lifecycle, which a
// duplex conversation runs asynchronously.
func realtimeTurn(t *testing.T, conv *Conversation, ch <-chan providers.StreamChunk, text string) {
	t.Helper()
	require.NoError(t, conv.SendText(context.Background(), text))
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case c, ok := <-ch:
			require.True(t, ok, "response channel closed during the turn for %q", text)
			require.NoError(t, c.Error)
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				conv.awaitTurnLifecycles()
				return
			}
		case <-timer.C:
			t.Fatalf("no completed assistant turn for %q", text)
		}
	}
}

// A realtime conversation's assistant replies reach the event bus as
// message.created, as a text conversation's do: the user turn, the tool-call
// turn, the tool result and the reply, each once, at the index each holds in
// the persisted transcript.
func TestDuplexRealtime_BroadcastsEveryMessageAtItsTranscriptIndex(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	var mu sync.Mutex
	published := map[int]string{}
	var dup []int
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		d, ok := e.Data.(*events.MessageCreatedData)
		if !ok {
			return
		}
		text := d.Role + ":" + d.GetContent()
		if len(d.ToolCalls) > 0 {
			text = d.Role + ":call " + d.ToolCalls[0].Name
		}
		mu.Lock()
		defer mu.Unlock()
		if _, seen := published[d.Index]; seen {
			dup = append(dup, d.Index)
		}
		published[d.Index] = text
	})
	store := statestore.NewMemoryStore()
	conv := openRealtimeLifecycle(t, WithEventBus(bus), WithStateStore(store), WithConversationID("rt-bus"))
	ch, err := conv.Response()
	require.NoError(t, err)

	realtimeTurn(t, conv, ch, "first")
	realtimeTurn(t, conv, ch, "second")

	want := map[int]string{
		0: "user:first",
		1: "assistant:call lookup",
		2: "tool:found",
		3: "assistant:the answer",
		4: "user:second",
		5: "assistant:second reply",
	}
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(published) >= len(want)
	}, 5*time.Second, 5*time.Millisecond)
	require.NoError(t, closeWithin(t, conv))
	bus.Close() // delivers any stray publish, so an extra event is caught

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, want, published)
	assert.Empty(t, dup, "a message was published more than once")

	state, err := store.Load(context.Background(), "rt-bus")
	require.NoError(t, err)
	persisted := map[int]string{}
	for i := range state.Messages {
		persisted[i] = roleText(&state.Messages[i])
	}
	assert.Equal(t, want, persisted, "the bus indices are the persisted transcript's")
}

// Each completed realtime turn runs OnSessionUpdate and the per-turn evals, as
// a text turn does: once per user turn, not per tool round, numbered 1 and 2,
// and over a transcript that holds the turn's reply. A duplex conversation runs
// them asynchronously, in order.
func TestDuplexRealtime_EachTurnRunsSessionUpdateAndTurnEvals(t *testing.T) {
	evalLog := &turnEvalLog{}
	// A slow hook: the turn's completion still waits for it.
	updates := &sessionUpdateLog{delay: 50 * time.Millisecond}
	conv := openRealtimeLifecycle(t, WithEvalHook(evalLog), WithSessionHook(updates))
	ch, err := conv.Response()
	require.NoError(t, err)

	realtimeTurn(t, conv, ch, "first")
	assert.Equal(t, []turnSeen{{Turn: 1, Last: "assistant:the answer"}}, updates.snapshot(),
		"the slow session update ran, in order")
	realtimeTurn(t, conv, ch, "second")

	want := []turnSeen{{Turn: 1, Last: "assistant:the answer"}, {Turn: 2, Last: "assistant:second reply"}}
	assert.Equal(t, want, updates.snapshot())
	require.NoError(t, closeWithin(t, conv)) // waits for the turn evals in flight
	assert.Equal(t, want, evalLog.sorted())
}

// A resumed realtime conversation numbers its turn evals on from the
// persisted transcript, as a resumed text conversation does.
func TestDuplexRealtime_ResumedTurnEvalsContinueTheTranscriptsNumbering(t *testing.T) {
	store := statestore.NewMemoryStore()
	convPersisted(t, store, "voice-1") // one persisted user turn
	evalLog := &turnEvalLog{}
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	conv, err := ResumeDuplex("voice-1", createTestPackFile(t, duplexLifecyclePack), "chat",
		WithProvider(realtimeToolScript()), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithStateStore(store), WithEvalRegistry(registry), WithEvalHook(evalLog))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "found", nil })
	ch, err := conv.Response()
	require.NoError(t, err)

	realtimeTurn(t, conv, ch, "first")
	realtimeTurn(t, conv, ch, "second")
	require.NoError(t, closeWithin(t, conv))

	got := evalLog.sorted()
	turns := make([]int, len(got))
	for i := range got {
		turns[i] = got[i].Turn
	}
	assert.Equal(t, []int{2, 3}, turns)
}

// Each completed cascaded (VAD) voice turn runs OnSessionUpdate and the
// per-turn evals, as a text turn does.
func TestDuplexVAD_EachTurnRunsSessionUpdateAndTurnEvals(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	evalLog := &turnEvalLog{}
	updates := &sessionUpdateLog{}
	conv, err := OpenDuplex(createTestPackFile(t, duplexLifecyclePack), "chat",
		WithProvider(&turnRecordingProvider{}), WithSkipSchemaValidation(),
		WithVADMode(newScriptedSTT("one", "two"), newConvMockTTSService(), vadTestConfig()),
		WithEvalRegistry(registry), WithEvalHook(evalLog), WithSessionHook(updates))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	convDrain(t, conv)

	speakOneUtterance(t, conv)
	require.Eventually(t, func() bool { return len(updates.snapshot()) == 1 },
		8*time.Second, 5*time.Millisecond, "the first turn runs its session update")
	speakOneUtterance(t, conv)
	require.Eventually(t, func() bool { return len(updates.snapshot()) == 2 },
		8*time.Second, 5*time.Millisecond, "the second turn runs its session update")
	require.NoError(t, closeWithin(t, conv))

	want := []turnSeen{{Turn: 1, Last: "assistant:ack"}, {Turn: 2, Last: "assistant:ack"}}
	assert.Equal(t, want, updates.snapshot())
	assert.Equal(t, want, evalLog.sorted())
}

// A realtime reply the session closes on before the model finishes it is kept
// as an interrupted reply, but it does not complete a turn: no session update
// and no per-turn eval run for it.
func TestDuplexRealtime_ReplyCutShortAtCloseCompletesNoTurn(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})
	evalLog := &turnEvalLog{}
	updates := &sessionUpdateLog{}
	store := statestore.NewMemoryStore()
	prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("unused").
		WithResponseChunks(providers.StreamChunk{Content: "half a", Delta: "half a"})
	conv, err := OpenDuplex(createTestPackFile(t, duplexLifecyclePack), "chat",
		WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithStateStore(store), WithConversationID("cut-short"),
		WithEvalRegistry(registry), WithEvalHook(evalLog), WithSessionHook(updates))
	require.NoError(t, err)
	ch, err := conv.Response()
	require.NoError(t, err)

	require.NoError(t, conv.SendText(context.Background(), "go on"))
	streamed := false
	for !streamed {
		select {
		case c := <-ch:
			streamed = c.Delta == "half a"
		case <-time.After(5 * time.Second):
			t.Fatal("the reply never started streaming")
		}
	}
	go func() {
		for range ch { //nolint:revive // drained so the session can close
		}
	}()
	require.NoError(t, closeWithin(t, conv))

	state, err := store.Load(context.Background(), "cut-short")
	require.NoError(t, err)
	var stored []string
	for i := range state.Messages {
		stored = append(stored, roleText(&state.Messages[i])+"/"+state.Messages[i].FinishReason)
	}
	assert.Equal(t, []string{"user:go on/", "assistant:half a/" + types.FinishReasonInterrupted}, stored,
		"the cut-short reply is kept, marked interrupted")
	assert.Empty(t, updates.snapshot())
	assert.Empty(t, evalLog.sorted())
}

// Turns can be reported stored while Close runs: their lifecycles are
// ordered with Close's wait for them and the eval drain (the race detector
// reports a race otherwise), and a turn reported once Close has ended the
// session runs nothing.
func TestDuplex_TurnStoredDuringCloseIsOrderedWithTheEvalDrain(t *testing.T) {
	evalLog := &turnEvalLog{}
	conv := openRealtimeLifecycle(t, WithEvalHook(evalLog))
	ch, err := conv.Response()
	require.NoError(t, err)
	realtimeTurn(t, conv, ch, "first")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 50 {
			conv.turnsStored([]int{-1}) // as the save stage reports a stored turn
		}
	}()
	require.NoError(t, closeWithin(t, conv))
	wg.Wait()

	before := len(evalLog.sorted())
	conv.turnsStored([]int{-1})
	time.Sleep(50 * time.Millisecond)
	assert.Len(t, evalLog.sorted(), before, "a turn stored after Close runs no eval")
}
