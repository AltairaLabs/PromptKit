package sdk

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// duplexTurnEvalPackJSON has one every_turn eval and one on_session_complete
// eval, each of its own recording stub type, and a tool for the tool-round
// case.
const duplexTurnEvalPackJSON = `{
	"$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
	"schema_version": "2025.1",
	"id": "duplex-turn-eval-pack",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
	"evals": [
		{"id": "turn", "type": "duplex_turn_stub", "trigger": "every_turn"},
		{"id": "sess", "type": "duplex_sess_stub", "trigger": "on_session_complete"}
	],
	"tools": {
		"lookup": {"name": "lookup", "description": "look up",
			"parameters": {"type": "object", "properties": {"who": {"type": "string"}}}}
	},
	"prompts": {
		"chat": {
			"id": "chat", "name": "Chat", "description": "Chat",
			"version": "1.0.0", "system_template": "You chat.", "tools": ["lookup"]
		}
	}
}`

// sessionUpdate is one OnSessionUpdate call, reduced to what the tests assert.
type sessionUpdate struct {
	TurnIndex  int
	Transcript []string
	Caller     types.Caller
}

// updateRecordingHook records every OnSessionUpdate call.
type updateRecordingHook struct {
	mu      sync.Mutex
	updates []sessionUpdate
	order   []string // "update" and "end", in the order they ran
}

func (h *updateRecordingHook) Name() string { return "update-recorder" }
func (h *updateRecordingHook) OnSessionStart(context.Context, hooks.SessionEvent) error {
	return nil
}
func (h *updateRecordingHook) OnSessionEnd(context.Context, hooks.SessionEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.order = append(h.order, "end")
	return nil
}
func (h *updateRecordingHook) OnSessionUpdate(_ context.Context, e hooks.SessionEvent) error {
	var texts []string
	for i := range e.Messages {
		texts = append(texts, e.Messages[i].GetContent())
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updates = append(h.updates, sessionUpdate{TurnIndex: e.TurnIndex, Transcript: texts, Caller: e.Caller})
	h.order = append(h.order, "update")
	return nil
}

func (h *updateRecordingHook) calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.order...)
}

func (h *updateRecordingHook) seen() []sessionUpdate {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]sessionUpdate(nil), h.updates...)
}

// duplexTurnHarness is a realtime conversation on one scripted mock session,
// with the turn and session eval stubs, an update hook, and its output read.
type duplexTurnHarness struct {
	conv     *Conversation
	sess     *mock.MockStreamSession
	col      *stepEventCollector
	turnEval *stepRecordingHandler
	sessEval *stepRecordingHandler
	hook     *updateRecordingHook
}

func openDuplexTurnHarness(t *testing.T, extra ...Option) *duplexTurnHarness {
	t.Helper()
	bus, col, registry, _ := newStepEvalHarness(t)
	h := &duplexTurnHarness{
		sess:     mock.NewMockStreamSession(),
		col:      col,
		turnEval: &stepRecordingHandler{typeName: "duplex_turn_stub"},
		sessEval: &stepRecordingHandler{typeName: "duplex_sess_stub"},
		hook:     &updateRecordingHook{},
	}
	registry.Register(h.turnEval)
	registry.Register(h.sessEval)
	opts := append([]Option{
		WithSkipSchemaValidation(),
		WithProvider(&heardEvalProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false), sess: h.sess}),
		WithStateStore(statestore.NewMemoryStore()),
		WithEvalRegistry(registry),
		WithEventBus(bus),
		WithSessionHook(h.hook),
		WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio}}),
	}, extra...)
	conv, err := OpenDuplex(writeWorkflowTestPack(t, duplexTurnEvalPackJSON), "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	h.conv = conv
	return h
}

// say types text and waits until the session has it, the nth text it got.
func (h *duplexTurnHarness) say(t *testing.T, text string, n int) {
	t.Helper()
	require.NoError(t, h.conv.SendText(context.Background(), text))
	require.Eventually(t, func() bool { return len(h.sess.GetTexts()) >= n }, 2*time.Second, 2*time.Millisecond)
}

// reply has the session stream text and end the reply.
func (h *duplexTurnHarness) reply(text string) {
	stop := "stop"
	h.sess.EmitChunk(&providers.StreamChunk{Content: text, Delta: text})
	h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
}

// turnEvals waits for n every_turn results, then gives stragglers a moment so
// an over-count is caught too, and returns their turn indices in order.
func (h *duplexTurnHarness) turnEvals(t *testing.T, n int) []int {
	t.Helper()
	require.Eventually(t, func() bool { return len(h.col.byTrigger(evals.TriggerEveryTurn)) >= n },
		5*time.Second, 5*time.Millisecond, "expected %d every_turn eval(s)", n)
	time.Sleep(50 * time.Millisecond)
	var idx []int
	for _, e := range h.col.byTrigger(evals.TriggerEveryTurn) {
		idx = append(idx, e.TurnIndex)
	}
	sort.Ints(idx)
	return idx
}

// sortedTranscripts returns the transcripts an eval stub saw, shortest
// first: turn evals run concurrently, so they record in any order.
func sortedTranscripts(h *stepRecordingHandler) [][]string {
	seen := h.transcripts()
	sort.Slice(seen, func(i, j int) bool { return len(seen[i]) < len(seen[j]) })
	return seen
}

// Each completed realtime turn runs the turn evals once, against the
// transcript up to and including that turn, at its 0-based turn index, and
// calls OnSessionUpdate once, as a text turn does. A reply barge-in cut short
// is a completed turn: its evals see the reply as far as the caller got it.
func TestDuplexTurnEvals_RunOncePerCompletedTurn(t *testing.T) {
	member := types.Caller{ID: "member@example.com"}
	h := openDuplexTurnHarness(t, WithCaller(member))
	convDrain(t, h.conv)

	h.say(t, "hi", 1)
	h.reply("Hello there.")
	assert.Equal(t, []int{0}, h.turnEvals(t, 1))

	h.say(t, "tell me more", 2)
	h.sess.EmitChunk(&providers.StreamChunk{Content: "Well, once", Delta: "Well, once"})
	h.sess.EmitChunk(&providers.StreamChunk{Interrupted: true})
	assert.Equal(t, []int{0, 1}, h.turnEvals(t, 2))

	assert.Equal(t, [][]string{
		{"hi", "Hello there."},
		{"hi", "Hello there.", "tell me more", "Well, once"},
	}, sortedTranscripts(h.turnEval))
	assert.Equal(t, []sessionUpdate{
		{TurnIndex: 1, Transcript: []string{"hi", "Hello there."}, Caller: member},
		{TurnIndex: 2, Transcript: []string{"hi", "Hello there.", "tell me more", "Well, once"}, Caller: member},
	}, h.hook.seen())

	require.NoError(t, h.conv.Close())
	require.Eventually(t, func() bool { return len(h.col.byTrigger(evals.TriggerOnSessionComplete)) == 1 },
		5*time.Second, 5*time.Millisecond, "the session evals never ran")
	assert.Equal(t, 1, h.col.byTrigger(evals.TriggerOnSessionComplete)[0].TurnIndex,
		"the session evals report the last turn, the one barge-in cut short")
	assert.Len(t, h.col.byTrigger(evals.TriggerEveryTurn), 2, "closing runs no turn evals of its own")
	assert.Len(t, h.hook.seen(), 2, "closing calls OnSessionUpdate for no turn")
}

// A reply that calls a tool is not the end of the caller's turn: the reply
// that answers with the tool's result is. The turn evals and OnSessionUpdate
// run once, after it.
func TestDuplexTurnEvals_ToolRoundIsOneTurn(t *testing.T) {
	h := openDuplexTurnHarness(t)
	// The session answers the tool result with this reply.
	stop := "stop"
	h.sess.WithResponseChunks([]providers.StreamChunk{{Content: "Found her.", Delta: "Found her."}, {FinishReason: &stop}})
	h.conv.OnTool("lookup", func(map[string]any) (any, error) { return map[string]any{"found": true}, nil })
	convDrain(t, h.conv)

	h.say(t, "find ada", 1)
	toolUse := "tool_calls"
	h.sess.EmitChunk(&providers.StreamChunk{
		ToolCalls:    []types.MessageToolCall{{ID: "call-1", Name: "lookup", Args: json.RawMessage(`{"who":"ada"}`)}},
		FinishReason: &toolUse,
	})
	assert.Equal(t, []int{0}, h.turnEvals(t, 1),
		"a tool round does not advance the caller's turn")
	require.Len(t, h.hook.seen(), 1)
	assert.Equal(t, 1, h.hook.seen()[0].TurnIndex)
	seen := h.turnEval.transcripts()
	require.Len(t, seen, 1)
	assert.Equal(t, "find ada", seen[0][0])
	assert.Equal(t, "Found her.", seen[0][len(seen[0])-1])
}

// A reply still playing is not yet in the transcript, so its turn evals wait
// until the caller has heard it (here, until the caller barges in), and see
// it with what was heard.
func TestDuplexTurnEvals_HeldReplyRunsWhenHeard(t *testing.T) {
	h := openDuplexTurnHarness(t, WithPlayoutTracking())
	ch, err := h.conv.Response()
	require.NoError(t, err)
	turns := make(chan struct{}, 4)
	go func() {
		for c := range ch {
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				turns <- struct{}{}
			}
		}
	}()

	h.say(t, "tell me a story", 1)
	h.sess.EmitChunk(&providers.StreamChunk{Delta: "Once upon a time.",
		Metadata: map[string]any{"type": "output_transcription"}})
	h.sess.EmitChunk(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: make([]byte, 5*24000*2), MIMEType: "audio/pcm", SampleRate: 24000, Channels: 1}})
	stop := "stop"
	h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
	select {
	case <-turns:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "the reply never completed")
	}
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, h.col.byTrigger(evals.TriggerEveryTurn), "the reply is still playing")
	assert.Empty(t, h.hook.seen(), "the reply is still playing")

	h.sess.EmitChunk(&providers.StreamChunk{Interrupted: true})
	assert.Equal(t, []int{0}, h.turnEvals(t, 1))
	assert.Equal(t, [][]string{{"tell me a story", "Once upon a time."}}, h.turnEval.transcripts())
	require.Len(t, h.hook.seen(), 1)
	assert.Equal(t, 1, h.hook.seen()[0].TurnIndex)
}

// A cascaded (VAD) conversation runs the turn evals and OnSessionUpdate once
// per turn too. A conversation that resumes a stored one places its turns
// after the stored turns.
func TestDuplexTurnEvals_VADModeRunsPerTurn(t *testing.T) {
	cases := []struct {
		name  string
		prior []types.Message
		want  []int
	}{
		{name: "new", want: []int{0, 1}},
		{name: "resumed", prior: []types.Message{{Role: "user", Content: "earlier"}, {Role: "assistant", Content: "ok"}},
			want: []int{1, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus, col, registry, _ := newStepEvalHarness(t)
			turnEval := &stepRecordingHandler{typeName: "duplex_turn_stub"}
			registry.Register(turnEval)
			registry.Register(&stepRecordingHandler{typeName: "duplex_sess_stub"})
			hook := &updateRecordingHook{}
			store := statestore.NewMemoryStore()
			var earlier []string
			if tc.prior != nil {
				require.NoError(t, store.Save(context.Background(),
					&statestore.ConversationState{ID: "vad-1", Messages: tc.prior}))
				earlier = []string{"earlier", "ok"}
			}
			conv, err := OpenDuplex(writeWorkflowTestPack(t, duplexTurnEvalPackJSON), "chat",
				WithSkipSchemaValidation(),
				WithProvider(&recordingTextProvider{}),
				WithStateStore(store),
				WithConversationID("vad-1"),
				WithEvalRegistry(registry),
				WithEventBus(bus),
				WithSessionHook(hook),
				WithVADMode(&convMockSTTService{}, newConvMockTTSService(), vadTestConfig()),
			)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			convDrain(t, conv)

			require.NoError(t, conv.SendText(context.Background(), "one"))
			require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerEveryTurn)) == 1 },
				5*time.Second, 5*time.Millisecond)
			require.NoError(t, conv.SendText(context.Background(), "two"))
			require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerEveryTurn)) == 2 },
				5*time.Second, 5*time.Millisecond)
			time.Sleep(50 * time.Millisecond)

			var idx []int
			for _, e := range col.byTrigger(evals.TriggerEveryTurn) {
				idx = append(idx, e.TurnIndex)
			}
			sort.Ints(idx)
			assert.Equal(t, tc.want, idx)
			assert.Equal(t, [][]string{
				append(append([]string(nil), earlier...), "one", "ack"),
				append(append([]string(nil), earlier...), "one", "ack", "two", "ack"),
			}, sortedTranscripts(turnEval))
			updates := hook.seen()
			require.Len(t, updates, 2)
			assert.Equal(t, []int{1, 2}, []int{updates[0].TurnIndex, updates[1].TurnIndex},
				"OnSessionUpdate counts the turns this conversation ran, as on the text path")
		})
	}
}

// A conversation with a custom ingestion sub-graph keeps each turn as it
// completes, so its turn evals see that turn.
func TestDuplexTurnEvals_IngestionRunsPerTurn(t *testing.T) {
	bus, col, registry, _ := newStepEvalHarness(t)
	turnEval := &stepRecordingHandler{typeName: "duplex_turn_stub"}
	registry.Register(turnEval)
	registry.Register(&stepRecordingHandler{typeName: "duplex_sess_stub"})
	conv, err := OpenDuplex(writeWorkflowTestPack(t, duplexTurnEvalPackJSON), "chat",
		WithSkipSchemaValidation(),
		WithProvider(&recordingTurnProvider{}),
		WithStateStore(statestore.NewMemoryStore()),
		WithEvalRegistry(registry),
		WithEventBus(bus),
		WithIngestion(func(b *stage.PipelineBuilder) (string, error) {
			b.AddStage(newTurnEmitterStage("turn_emitter"))
			return "turn_emitter", nil
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	convDrain(t, conv)

	require.NoError(t, conv.SendText(context.Background(), "one"))
	require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerEveryTurn)) == 1 },
		5*time.Second, 5*time.Millisecond)
	require.NoError(t, conv.SendText(context.Background(), "two"))
	require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerEveryTurn)) == 2 },
		5*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, [][]string{{"one", "reply-1"}, {"one", "reply-1", "two", "reply-2"}},
		sortedTranscripts(turnEval))
	var idx []int
	for _, e := range col.byTrigger(evals.TriggerEveryTurn) {
		idx = append(idx, e.TurnIndex)
	}
	sort.Ints(idx)
	assert.Equal(t, []int{0, 1}, idx)
	require.NoError(t, conv.Close())
	require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerOnSessionComplete)) == 1 },
		5*time.Second, 5*time.Millisecond)
	assert.Equal(t, 1, col.byTrigger(evals.TriggerOnSessionComplete)[0].TurnIndex)
}

// The session evals score the whole stored transcript, including what was
// stored after the last turn the turn evals ran for.
func TestDuplexTurnEvals_SessionEvalsSeeMessagesAfterTheLastTurn(t *testing.T) {
	h := openDuplexTurnHarness(t)
	convDrain(t, h.conv)

	h.say(t, "hi", 1)
	h.reply("Hello there.")
	assert.Equal(t, []int{0}, h.turnEvals(t, 1))

	// A reply that calls a tool the host answers later: stored, but not the
	// end of a turn.
	h.conv.OnToolAsync("lookup", func(map[string]any) sdktools.PendingResult {
		return sdktools.PendingResult{Reason: "later"}
	}, func(map[string]any) (any, error) { return "found", nil })
	h.say(t, "find ada", 2)
	toolUse := "tool_calls"
	h.sess.EmitChunk(&providers.StreamChunk{Content: "Looking.", Delta: "Looking.",
		ToolCalls:    []types.MessageToolCall{{ID: "call-1", Name: "lookup", Args: json.RawMessage(`{"who":"ada"}`)}},
		FinishReason: &toolUse,
	})
	require.Eventually(t, func() bool {
		msgs := h.conv.Messages(context.Background())
		return len(msgs) >= 4 && msgs[3].GetContent() == "Looking."
	}, 2*time.Second, 2*time.Millisecond, "the tool-calling reply was never stored")

	require.NoError(t, h.conv.Close())
	require.Eventually(t, func() bool { return len(h.sessEval.transcripts()) == 1 },
		5*time.Second, 5*time.Millisecond, "the session evals never ran")
	assert.Equal(t, []string{"hi", "Hello there.", "find ada", "Looking.",
		"Tool lookup not executed: the session closed before it ran"}, h.sessEval.transcripts()[0])
	assert.Len(t, h.col.byTrigger(evals.TriggerEveryTurn), 1, "the held tool call ended no turn")
}

// Closing while a reply still plays releases it into the transcript, and the
// turn it completes is reported, its turn evals and OnSessionUpdate, before
// the session ends.
func TestDuplexTurnEvals_CloseReportsTheTurnStillPlaying(t *testing.T) {
	h := openDuplexTurnHarness(t, WithPlayoutTracking())
	convDrain(t, h.conv)

	h.say(t, "tell me a story", 1)
	h.sess.EmitChunk(&providers.StreamChunk{Delta: "Once upon a time.",
		Metadata: map[string]any{"type": "output_transcription"}})
	h.sess.EmitChunk(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: make([]byte, 5*24000*2), MIMEType: "audio/pcm", SampleRate: 24000, Channels: 1}})
	stop := "stop"
	h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, h.hook.seen(), "the reply is still playing")

	start := time.Now()
	require.NoError(t, h.conv.Close())
	assert.Less(t, time.Since(start), duplexTurnReportTimeout,
		"Close waits for the last report, not for its bound")
	assert.Equal(t, []string{"update", "end"}, h.hook.calls())
	assert.Equal(t, []int{0}, h.turnEvals(t, 1))
	assert.Equal(t, [][]string{{"tell me a story", "Once upon a time."}}, h.turnEval.transcripts())
}

// slowFirstUpdateHook is an updateRecordingHook whose first OnSessionUpdate
// takes a while, so the pipeline stores what follows the first turn before
// that turn's evals read the transcript.
type slowFirstUpdateHook struct {
	updateRecordingHook
	slowed bool
}

func (h *slowFirstUpdateHook) OnSessionUpdate(ctx context.Context, e hooks.SessionEvent) error {
	h.mu.Lock()
	first := !h.slowed
	h.slowed = true
	h.mu.Unlock()
	err := h.updateRecordingHook.OnSessionUpdate(ctx, e)
	if first {
		time.Sleep(300 * time.Millisecond)
	}
	return err
}

// Two replies held while they play are released into the transcript
// together when the caller barges in. Each turn's evals and OnSessionUpdate
// see the transcript up to and including its own reply, never the later one.
func TestDuplexTurnEvals_ReleasedTogetherEachSeesItsOwnTurn(t *testing.T) {
	hook := &slowFirstUpdateHook{}
	h := openDuplexTurnHarness(t, WithPlayoutTracking(), WithSessionHook(hook))
	ch, err := h.conv.Response()
	require.NoError(t, err)
	turns := make(chan struct{}, 4)
	go func() {
		for c := range ch {
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				turns <- struct{}{}
			}
		}
	}()
	spokenReply := func(text string) {
		h.sess.EmitChunk(&providers.StreamChunk{Delta: text, Metadata: map[string]any{"type": "output_transcription"}})
		h.sess.EmitChunk(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
			Data: make([]byte, 5*24000*2), MIMEType: "audio/pcm", SampleRate: 24000, Channels: 1}})
		stop := "stop"
		h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
		select {
		case <-turns:
		case <-time.After(3 * time.Second):
			require.FailNow(t, "the reply never completed")
		}
	}

	h.say(t, "tell me a story", 1)
	spokenReply("Once upon a time.")
	h.say(t, "and then?", 2)
	spokenReply("The end.")
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, h.col.byTrigger(evals.TriggerEveryTurn), "both replies are still playing")

	h.sess.EmitChunk(&providers.StreamChunk{Interrupted: true})
	assert.Equal(t, []int{0, 1}, h.turnEvals(t, 2))
	assert.Equal(t, [][]string{
		{"tell me a story", "Once upon a time."},
		{"tell me a story", "Once upon a time.", "and then?", "The end."},
	}, sortedTranscripts(h.turnEval))
	updates := hook.seen()
	require.Len(t, updates, 2)
	assert.Equal(t, []string{"tell me a story", "Once upon a time."}, updates[0].Transcript)
	assert.Equal(t, []string{"tell me a story", "Once upon a time.", "and then?", "The end."}, updates[1].Transcript)
}

// Closing while a reply still streams stores the partial reply as the turn's
// reply: its turn evals run, and the session evals report the same turn.
func TestDuplexTurnEvals_CloseMidReplyAgreesWithSessionEvals(t *testing.T) {
	h := openDuplexTurnHarness(t)
	convDrain(t, h.conv)

	h.say(t, "hi", 1)
	h.reply("Hello there.")
	assert.Equal(t, []int{0}, h.turnEvals(t, 1))
	h.say(t, "tell me more", 2)
	h.sess.EmitChunk(&providers.StreamChunk{Content: "Well, once", Delta: "Well, once"})
	require.Eventually(t, func() bool {
		return len(h.sess.GetTexts()) == 2
	}, 2*time.Second, 2*time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, h.conv.Close())
	require.Eventually(t, func() bool { return len(h.col.byTrigger(evals.TriggerOnSessionComplete)) == 1 },
		5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []int{0, 1}, h.turnEvals(t, 2))
	assert.Equal(t, 1, h.col.byTrigger(evals.TriggerOnSessionComplete)[0].TurnIndex,
		"the session evals report the last turn the turn evals ran for")
}

// blockingUpdateHook's OnSessionUpdate waits for release before returning,
// and may touch the conversation it reports on first.
type blockingUpdateHook struct {
	updateRecordingHook
	release chan struct{}
	conv    func() *Conversation
}

func (h *blockingUpdateHook) OnSessionUpdate(ctx context.Context, e hooks.SessionEvent) error {
	if h.conv != nil {
		if c := h.conv(); c != nil {
			c.SetVar("last_turn", "x")
		}
	}
	<-h.release
	return h.updateRecordingHook.OnSessionUpdate(ctx, e)
}

// A slow OnSessionUpdate on the turn Close releases, one that also touches the
// conversation, still runs before OnSessionEnd, and that turn's evals run.
func TestDuplexTurnEvals_CloseWaitsForASlowUpdate(t *testing.T) {
	hook := &blockingUpdateHook{release: make(chan struct{})}
	h := openDuplexTurnHarness(t, WithPlayoutTracking(), WithSessionHook(hook))
	hook.conv = func() *Conversation { return h.conv }
	convDrain(t, h.conv)

	h.say(t, "tell me a story", 1)
	h.sess.EmitChunk(&providers.StreamChunk{Delta: "Once upon a time.",
		Metadata: map[string]any{"type": "output_transcription"}})
	h.sess.EmitChunk(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: make([]byte, 5*24000*2), MIMEType: "audio/pcm", SampleRate: 24000, Channels: 1}})
	stop := "stop"
	h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
	time.Sleep(100 * time.Millisecond)

	go func() {
		// Longer than the session takes to stop: its drain grace, teardown
		// bound and Close's wait for it together.
		time.Sleep(2500 * time.Millisecond)
		close(hook.release)
	}()
	require.NoError(t, h.conv.Close())
	assert.Equal(t, []string{"update", "end"}, hook.calls())
	assert.Equal(t, []int{0}, h.turnEvals(t, 1), "the turn Close released is scored")
}

// A session hook that never returns does not hold Close past its bound.
func TestDuplexTurnEvals_CloseDoesNotHangOnAStuckUpdate(t *testing.T) {
	prev := duplexTurnReportTimeout
	duplexTurnReportTimeout = 300 * time.Millisecond
	t.Cleanup(func() { duplexTurnReportTimeout = prev })
	hook := &blockingUpdateHook{release: make(chan struct{})}
	t.Cleanup(func() { close(hook.release) })
	h := openDuplexTurnHarness(t, WithPlayoutTracking(), WithSessionHook(hook))
	convDrain(t, h.conv)

	h.say(t, "tell me a story", 1)
	h.sess.EmitChunk(&providers.StreamChunk{Delta: "Once upon a time.",
		Metadata: map[string]any{"type": "output_transcription"}})
	h.sess.EmitChunk(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: make([]byte, 5*24000*2), MIMEType: "audio/pcm", SampleRate: 24000, Channels: 1}})
	stop := "stop"
	h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	require.NoError(t, h.conv.Close())
	assert.Less(t, time.Since(start), 5*time.Second, "Close waited past its bound")
}

// With the provider's own transcript order (no reordering), the caller's
// transcript arrives after the reply to it. The turn's evals and
// OnSessionUpdate wait for it, so they see the caller's words and the reply.
func TestDuplexTurnEvals_WaitForALateCallerTranscript(t *testing.T) {
	h := openDuplexTurnHarness(t)
	convDrain(t, h.conv)

	require.NoError(t, h.conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{Data: make([]byte, 320), MIMEType: "audio/pcm"}}))
	require.Eventually(t, func() bool { return len(h.sess.GetChunks()) == 1 }, 2*time.Second, 2*time.Millisecond)
	h.sess.EmitChunk(&providers.StreamChunk{Delta: "Sure.", Metadata: map[string]any{"type": "output_transcription"}})
	stop := "stop"
	h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
	time.Sleep(50 * time.Millisecond)
	h.sess.EmitChunk(&providers.StreamChunk{Metadata: map[string]any{
		"type": "input_transcription", "transcription": "hello there", "transcription_final": true}})

	assert.Equal(t, []int{0}, h.turnEvals(t, 1))
	seen := h.turnEval.transcripts()
	require.Len(t, seen, 1)
	assert.ElementsMatch(t, []string{"hello there", "Sure."}, seen[0])
	require.Len(t, h.hook.seen(), 1)
	assert.ElementsMatch(t, []string{"hello there", "Sure."}, h.hook.seen()[0].Transcript)
}

// A reply the provider reports cut short by an interruption on the chunk
// carrying its partial text (Gemini Live's shape) ends its turn on Response(),
// as a reply the provider ends with an interrupted finish (OpenAI Realtime's)
// does.
func TestDuplexTurnEvals_InterruptedReplyEndsItsTurnOnResponse(t *testing.T) {
	h := openDuplexTurnHarness(t)
	ch, err := h.conv.Response()
	require.NoError(t, err)
	ends := make(chan string, 4)
	go func() {
		for c := range ch {
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				fr, _ := c.Metadata["finish_reason"].(string)
				ends <- fr
			}
		}
	}()

	h.say(t, "tell me a story", 1)
	h.sess.EmitChunk(&providers.StreamChunk{Content: "Once upon", Delta: "Once upon"})
	h.sess.EmitChunk(&providers.StreamChunk{Interrupted: true})
	select {
	case fr := <-ends:
		assert.Equal(t, "interrupted", fr)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "the interrupted reply never ended its turn")
	}
	select {
	case fr := <-ends:
		assert.Failf(t, "a second turn end", "finish %q", fr)
	case <-time.After(100 * time.Millisecond):
	}
}
