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
	guard    *turnIndexHook
}

// turnIndexHook is a guardrail that allows everything and records the turn
// index of each completed reply it judges (AfterCall), with the reply's text.
type turnIndexHook struct {
	mu      sync.Mutex
	replies []guardedReply
	// inputs are the caller turns it judged (BeforeCall), with their text.
	inputs []guardedReply
}

type guardedReply struct {
	TurnIndex int
	Text      string
}

func (h *turnIndexHook) Name() string { return "turn-index" }
func (h *turnIndexHook) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" {
		h.mu.Lock()
		h.inputs = append(h.inputs, guardedReply{TurnIndex: req.TurnIndex, Text: req.Messages[n-1].GetContent()})
		h.mu.Unlock()
	}
	return hooks.Allow
}

// inputIndex waits for the guardrail to judge the caller turn saying text and
// returns the turn index it was judged at.
func (h *turnIndexHook) inputIndex(t *testing.T, text string) int {
	t.Helper()
	var idx int
	require.Eventually(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, in := range h.inputs {
			if in.Text == text {
				idx = in.TurnIndex
				return true
			}
		}
		return false
	}, 3*time.Second, 2*time.Millisecond, "the guardrail never judged %q", text)
	return idx
}
func (h *turnIndexHook) AfterCall(_ context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.replies = append(h.replies, guardedReply{TurnIndex: req.TurnIndex, Text: resp.Message.GetContent()})
	return hooks.Allow
}

// indices returns the turn index of each reply the guardrail judged, in order.
func (h *turnIndexHook) indices() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []int
	for _, r := range h.replies {
		out = append(out, r.TurnIndex)
	}
	return out
}

// lateTranscriptProvider is heardEvalProvider for a provider that delivers
// the caller's transcript after the reply to it (as OpenAI Realtime does).
type lateTranscriptProvider struct{ heardEvalProvider }

func (p *lateTranscriptProvider) EmitsLateInputTranscription() bool { return true }

func openDuplexTurnHarness(t *testing.T, extra ...Option) *duplexTurnHarness {
	t.Helper()
	sess := mock.NewMockStreamSession()
	return openDuplexTurnHarnessOn(t,
		&heardEvalProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false), sess: sess}, sess,
		&providers.StreamingInputConfig{Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio}},
		extra...)
}

// openDuplexTurnHarnessOn is openDuplexTurnHarness on prov, whose sessions
// are sess, opened with cfg.
func openDuplexTurnHarnessOn(
	t *testing.T, prov providers.Provider, sess *mock.MockStreamSession, cfg *providers.StreamingInputConfig,
	extra ...Option,
) *duplexTurnHarness {
	t.Helper()
	bus, col, registry, _ := newStepEvalHarness(t)
	h := &duplexTurnHarness{
		sess:     sess,
		col:      col,
		turnEval: &stepRecordingHandler{typeName: "duplex_turn_stub"},
		sessEval: &stepRecordingHandler{typeName: "duplex_sess_stub"},
		hook:     &updateRecordingHook{},
		guard:    &turnIndexHook{},
	}
	registry.Register(h.turnEval)
	registry.Register(h.sessEval)
	opts := append([]Option{
		WithSkipSchemaValidation(),
		WithProvider(prov),
		WithStateStore(statestore.NewMemoryStore()),
		WithEvalRegistry(registry),
		WithEventBus(bus),
		WithSessionHook(h.hook),
		WithProviderHook(h.guard),
		WithStreamingConfig(cfg),
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
	assert.Equal(t, []int{0, 0}, h.guard.indices(), "the guardrails place both replies in the caller's turn")
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
			guard := &turnIndexHook{}
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
				WithProviderHook(guard),
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
			assert.Equal(t, tc.want, guard.indices(), "the guardrails place each turn where its evals do")
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

// caller audio is one chunk of the caller's audio.
var callerAudio = &providers.StreamChunk{MediaData: &providers.StreamMediaData{
	Data: make([]byte, 3200), MIMEType: "audio/pcm", SampleRate: 16000, Channels: 1}}

// speak sends one chunk of caller audio and waits until the session has it,
// the nth chunk it got.
func (h *duplexTurnHarness) speak(t *testing.T, n int) {
	t.Helper()
	require.NoError(t, h.conv.SendChunk(context.Background(), callerAudio))
	require.Eventually(t, func() bool { return len(h.sess.GetChunks()) >= n }, 2*time.Second, 2*time.Millisecond)
}

// The turn index counts the caller's turns, whether or not a transcript of
// them is ever stored: spoken turns with no transcript are turns 0, 1, 2, for
// the turn evals, the guardrails and the session evals alike, and nothing
// waits for a transcript that is not coming.
func TestDuplexTurnIndex_SpokenTurnsWithoutTranscripts(t *testing.T) {
	h := openDuplexTurnHarness(t)
	convDrain(t, h.conv)
	for i := 0; i < 3; i++ {
		h.speak(t, i+1)
		start := time.Now()
		h.reply("Reply.")
		require.Eventually(t, func() bool { return len(h.col.byTrigger(evals.TriggerEveryTurn)) == i+1 },
			time.Second, 2*time.Millisecond, "turn %d was not reported at once", i)
		assert.Less(t, time.Since(start), 500*time.Millisecond)
	}
	assert.Equal(t, []int{0, 1, 2}, h.turnEvals(t, 3))
	assert.Equal(t, []int{0, 1, 2}, h.guard.indices())
	require.NoError(t, h.conv.Close())
	require.Eventually(t, func() bool { return len(h.col.byTrigger(evals.TriggerOnSessionComplete)) == 1 },
		5*time.Second, 5*time.Millisecond)
	assert.Equal(t, 2, h.col.byTrigger(evals.TriggerOnSessionComplete)[0].TurnIndex)
}

// A provider that delivers transcripts late, with transcription off, sends
// none: its turns are counted and reported at once.
func TestDuplexTurnIndex_TranscriptionOffDoesNotWait(t *testing.T) {
	sess := mock.NewMockStreamSession()
	prov := &lateTranscriptProvider{heardEvalProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false),
		sess: sess}}
	h := openDuplexTurnHarnessOn(t, prov, sess, &providers.StreamingInputConfig{
		Config:   types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		Metadata: map[string]any{"input_transcription": false, "input_transcript_ordering": "passthrough"},
	})
	convDrain(t, h.conv)
	for i := 0; i < 2; i++ {
		h.speak(t, i+1)
		start := time.Now()
		h.reply("Reply.")
		require.Eventually(t, func() bool { return len(h.col.byTrigger(evals.TriggerEveryTurn)) == i+1 },
			time.Second, 2*time.Millisecond)
		assert.Less(t, time.Since(start), 500*time.Millisecond, "nothing waits for a transcript")
	}
	assert.Equal(t, []int{0, 1}, h.turnEvals(t, 2))
	assert.Equal(t, []int{0, 1}, h.guard.indices())
}

// A reply the model makes before the caller says anything (a greeting) is
// turn 0, and the caller's first turn after it is turn 1.
func TestDuplexTurnIndex_ModelGreetingFirst(t *testing.T) {
	h := openDuplexTurnHarness(t)
	convDrain(t, h.conv)
	h.speak(t, 1) // opens the session; the model greets before the caller speaks
	h.sess.EmitChunk(&providers.StreamChunk{Content: "Welcome!", Delta: "Welcome!"})
	stop := "stop"
	h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
	assert.Equal(t, []int{0}, h.turnEvals(t, 1))
	h.say(t, "hi", 1)
	h.reply("Hello.")
	assert.Equal(t, []int{0, 1}, h.turnEvals(t, 2))
	assert.Equal(t, []int{0, 1}, h.guard.indices())
}

// A second reply to the same spoken input (no new caller input between) keeps
// the turn's index, for the evals and the guardrails.
func TestDuplexTurnIndex_SecondReplyToTheSameInput(t *testing.T) {
	h := openDuplexTurnHarness(t)
	convDrain(t, h.conv)
	h.speak(t, 1)
	h.reply("First part.")
	assert.Equal(t, []int{0}, h.turnEvals(t, 1))
	h.reply("Second part.")
	assert.Equal(t, []int{0, 0}, h.turnEvals(t, 2))
	assert.Equal(t, []int{0, 0}, h.guard.indices())
	h.speak(t, 2)
	h.reply("Next.")
	assert.Equal(t, []int{0, 0, 1}, h.turnEvals(t, 3))
	assert.Equal(t, []int{0, 0, 1}, h.guard.indices())
}

// With the provider's own transcript order, a spoken turn's transcript
// arrives after its reply: the turn waits for it, and the evals see both at
// the turn's index, the guardrails' index.
func TestDuplexTurnIndex_LateTranscriptIsWaitedFor(t *testing.T) {
	sess := mock.NewMockStreamSession()
	prov := &lateTranscriptProvider{heardEvalProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false),
		sess: sess}}
	h := openDuplexTurnHarnessOn(t, prov, sess, &providers.StreamingInputConfig{
		Config:   types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		Metadata: map[string]any{"input_transcription": true, "input_transcript_ordering": "passthrough"},
	})
	convDrain(t, h.conv)
	h.speak(t, 1)
	// The provider names the caller's input the reply answers, as OpenAI
	// Realtime does once it has committed the caller's speech.
	named := map[string]any{"type": "output_transcription", providers.ReplyToInputKey: "item-1"}
	h.sess.EmitChunk(&providers.StreamChunk{Delta: "Sure.", Metadata: named})
	stop := "stop"
	h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop, Metadata: named})
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, h.col.byTrigger(evals.TriggerEveryTurn), "the turn waits for its transcript")
	h.sess.EmitChunk(&providers.StreamChunk{Metadata: map[string]any{
		"type": "input_transcription", "transcription": "hello there", "transcription_final": true,
		providers.InputItemIDKey: "item-1"}})
	assert.Equal(t, []int{0}, h.turnEvals(t, 1))
	seen := h.turnEval.transcripts()
	require.Len(t, seen, 1)
	assert.ElementsMatch(t, []string{"hello there", "Sure."}, seen[0])
	assert.Equal(t, []int{0}, h.guard.indices())
}

// A resumed conversation goes on from the caller turns it stored.
func TestDuplexTurnIndex_ResumeContinues(t *testing.T) {
	store := statestore.NewMemoryStore()
	require.NoError(t, store.Save(context.Background(), &statestore.ConversationState{ID: "rt-1",
		Messages: []types.Message{{Role: "user", Content: "a"}, {Role: "assistant", Content: "b"},
			{Role: "user", Content: "c"}, {Role: "assistant", Content: "d"}}}))
	h := openDuplexTurnHarness(t, WithStateStore(store), WithConversationID("rt-1"))
	convDrain(t, h.conv)
	h.say(t, "e", 1)
	h.reply("f")
	assert.Equal(t, []int{2}, h.turnEvals(t, 1))
	h.speak(t, 1)
	h.reply("g")
	assert.Equal(t, []int{2, 3}, h.turnEvals(t, 2))
	assert.Equal(t, []int{2, 3}, h.guard.indices())
}

// cancelingUpdateHook's OnSessionUpdate cancels Start's context, waits for
// Start to begin stopping, then calls back into the conversation.
type cancelingUpdateHook struct {
	updateRecordingHook
	cancel context.CancelFunc
	conv   func() *Conversation
}

func (h *cancelingUpdateHook) OnSessionUpdate(ctx context.Context, e hooks.SessionEvent) error {
	h.cancel()
	time.Sleep(100 * time.Millisecond) // Start is stopping the session now
	h.conv().SetVar("last_turn", "x")
	return h.updateRecordingHook.OnSessionUpdate(ctx, e)
}

// A session hook that calls back into the conversation while Start stops the
// session (its context canceled) does not hold Start up: Start does not keep
// the conversation's lock while it waits for the session to stop.
func TestDuplexTurnEvals_StartCancelLetsAHookTouchTheConversation(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a voice session")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hook := &cancelingUpdateHook{cancel: cancel}
	prov := mock.NewStreamingProvider("rt", "rt-model", false)
	v := openVoiceUnderTest(t, ctx, WithProvider(prov), WithStreamingConfig(convDuplexAudio),
		WithSessionHook(hook))
	hook.conv = func() *Conversation { return v.conv }
	require.Eventually(t, func() bool { return prov.GetSession() != nil }, 5*time.Second, time.Millisecond)
	sess := prov.GetSession()
	stop := "stop"
	start := time.Now()
	sess.EmitChunk(&providers.StreamChunk{Content: "Hello.", Delta: "Hello."})
	sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})

	select {
	case err := <-v.startErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after its context was canceled")
	}
	took := time.Since(start)
	require.Eventually(t, func() bool { return len(hook.calls()) == 1 }, 3*time.Second, 5*time.Millisecond)
	assert.Less(t, took, 700*time.Millisecond, "Start waited on a hook that waited on Start's lock")
}

// A caller who barges in starts a turn of their own, even before any more of
// their audio reaches the session: the reply after the interruption answers
// it.
func TestDuplexTurnIndex_BargeInStartsATurn(t *testing.T) {
	h := openDuplexTurnHarness(t)
	convDrain(t, h.conv)
	h.say(t, "hi", 1)
	h.reply("Hello there.")
	h.say(t, "tell me a story", 2)
	h.sess.EmitChunk(&providers.StreamChunk{Content: "Once upon", Delta: "Once upon"})
	h.sess.EmitChunk(&providers.StreamChunk{Interrupted: true})
	h.reply("Sure, what then?")
	assert.Equal(t, []int{0, 1, 2}, h.turnEvals(t, 3))
	assert.Equal(t, []int{0, 2}, h.guard.indices(), "the guardrails judge the completed replies")
}

// With a provider whose transcripts come late, only a spoken turn whose
// transcript has not arrived waits for it: a typed turn, and a spoken one
// whose transcript came before its reply, are reported at once.
func TestDuplexTurnIndex_OnlyAnUntranscribedSpokenTurnWaits(t *testing.T) {
	sess := mock.NewMockStreamSession()
	prov := &lateTranscriptProvider{heardEvalProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false),
		sess: sess}}
	h := openDuplexTurnHarnessOn(t, prov, sess, &providers.StreamingInputConfig{
		Config:   types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		Metadata: map[string]any{"input_transcription": true, "input_transcript_ordering": "passthrough"},
	})
	convDrain(t, h.conv)
	reportedAtOnce := func(n int) {
		t.Helper()
		start := time.Now()
		require.Eventually(t, func() bool { return len(h.col.byTrigger(evals.TriggerEveryTurn)) == n },
			time.Second, 2*time.Millisecond)
		assert.Less(t, time.Since(start), 500*time.Millisecond)
	}

	h.say(t, "hi", 1)
	h.reply("Hello.")
	reportedAtOnce(1)

	h.speak(t, 1)
	h.sess.EmitChunk(&providers.StreamChunk{Metadata: map[string]any{
		"type": "input_transcription", "transcription": "how are you", "transcription_final": true}})
	time.Sleep(50 * time.Millisecond)
	h.reply("Fine.")
	reportedAtOnce(2)
	assert.Equal(t, []int{0, 1}, h.turnEvals(t, 2))
	assert.Equal(t, []int{0, 1}, h.guard.indices())
}

// With a provider whose transcripts come late, a reply before the caller has
// said anything, and a reply known only from the caller's audio, wait for no
// transcript.
func TestDuplexTurnIndex_GreetingWithALateTranscriberDoesNotWait(t *testing.T) {
	sess := mock.NewMockStreamSession()
	prov := &lateTranscriptProvider{heardEvalProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false),
		sess: sess}}
	h := openDuplexTurnHarnessOn(t, prov, sess, &providers.StreamingInputConfig{
		Config:   types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		Metadata: map[string]any{"input_transcription": true, "input_transcript_ordering": "passthrough"},
	})
	convDrain(t, h.conv)
	h.speak(t, 1)
	start := time.Now()
	h.reply("Welcome!")
	require.Eventually(t, func() bool { return len(h.col.byTrigger(evals.TriggerEveryTurn)) == 1 },
		time.Second, 2*time.Millisecond)
	assert.Less(t, time.Since(start), 500*time.Millisecond)
	assert.Equal(t, []int{0}, h.turnEvals(t, 1))
}

// namedReply has the session stream a reply answering the caller input named
// input, as OpenAI Realtime names it, and end it.
func (h *duplexTurnHarness) namedReply(text, input string) {
	meta := map[string]any{"type": "output_transcription", providers.ReplyToInputKey: input}
	h.sess.EmitChunk(&providers.StreamChunk{Delta: text, Metadata: meta})
	stop := "stop"
	h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop, Metadata: meta})
}

// transcribe has the session deliver the final transcript of caller input
// named input.
func (h *duplexTurnHarness) transcribe(text, input string) {
	h.sess.EmitChunk(&providers.StreamChunk{Metadata: map[string]any{
		"type": "input_transcription", "transcription": text, "transcription_final": true,
		providers.InputItemIDKey: input}})
}

// A spoken session's turns are stored with their caller-turn index, so a
// session that resumes it goes on from them, though no transcript of them was
// stored; a stored conversation without them goes on from its user turns.
func TestDuplexTurnIndex_ResumeAfterUntranscribedTurns(t *testing.T) {
	store := statestore.NewMemoryStore()
	first := openDuplexTurnHarness(t, WithStateStore(store), WithConversationID("spoken-1"))
	convDrain(t, first.conv)
	for i := 0; i < 2; i++ {
		first.speak(t, i+1)
		first.reply("Reply.")
		require.Eventually(t, func() bool { return len(first.col.byTrigger(evals.TriggerEveryTurn)) == i+1 },
			2*time.Second, 2*time.Millisecond)
	}
	assert.Equal(t, []int{0, 1}, first.turnEvals(t, 2))
	require.NoError(t, first.conv.Close())

	resumed := openDuplexTurnHarness(t, WithStateStore(store), WithConversationID("spoken-1"))
	convDrain(t, resumed.conv)
	resumed.speak(t, 1)
	resumed.reply("Again.")
	assert.Equal(t, []int{2}, resumed.turnEvals(t, 1))
	assert.Equal(t, []int{2}, resumed.guard.indices())

	old := statestore.NewMemoryStore()
	require.NoError(t, old.Save(context.Background(), &statestore.ConversationState{ID: "old-1",
		Messages: []types.Message{{Role: "user", Content: "a"}, {Role: "assistant", Content: "b"}}}))
	legacy := openDuplexTurnHarness(t, WithStateStore(old), WithConversationID("old-1"))
	convDrain(t, legacy.conv)
	legacy.speak(t, 1)
	legacy.reply("c")
	assert.Equal(t, []int{1}, legacy.turnEvals(t, 1), "a transcript stored without the index counts its user turns")
}

// A late transcript is judged at the turn of the speech it transcribes, even
// once the caller has typed their next turn.
func TestDuplexTurnIndex_LateTranscriptJudgedAtItsTurn(t *testing.T) {
	sess := mock.NewMockStreamSession()
	prov := &lateTranscriptProvider{heardEvalProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false),
		sess: sess}}
	h := openDuplexTurnHarnessOn(t, prov, sess, &providers.StreamingInputConfig{
		Config:   types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		Metadata: map[string]any{"input_transcription": true, "input_transcript_ordering": "passthrough"},
	})
	convDrain(t, h.conv)
	h.speak(t, 1)
	h.namedReply("Sure.", "item-1")
	require.Eventually(t, func() bool { return len(h.guard.indices()) == 1 }, 2*time.Second, 2*time.Millisecond,
		"the reply never completed")
	h.say(t, "and then?", 1)
	h.transcribe("hello there", "item-1")
	assert.Equal(t, 0, h.guard.inputIndex(t, "hello there"))
	assert.Equal(t, 1, h.guard.inputIndex(t, "and then?"))
}

// Speech the provider takes as new input while a reply plays, without
// cutting the reply (OpenAI Realtime with interrupt_response off), is the
// next turn: its guardrail and the reply that answers it report it.
func TestDuplexTurnIndex_SpeechOverAReplyIsTheNextTurn(t *testing.T) {
	h := openDuplexTurnHarness(t)
	convDrain(t, h.conv)
	h.speak(t, 1)
	meta := map[string]any{"type": "output_transcription", providers.ReplyToInputKey: "item-1"}
	h.sess.EmitChunk(&providers.StreamChunk{Delta: "Let me think.", Metadata: meta})
	h.transcribe("actually, never mind", "item-2")
	stop := "stop"
	h.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop, Metadata: meta})
	h.namedReply("Okay.", "item-2")
	assert.Equal(t, []int{0, 1}, h.turnEvals(t, 2))
	assert.Equal(t, []int{0, 1}, h.guard.indices())
	assert.Equal(t, 1, h.guard.inputIndex(t, "actually, never mind"))
}
