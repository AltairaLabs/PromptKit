package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// An output guardrail firing on a reply means the same on every path: a
// streamed text reply (Stream), a whole one (Send) and a realtime one
// (OpenDuplex). A blocked reply's tool calls never run; a check that denies or
// fails gives the guardrail's message; every guardrail that fired is
// recorded; a banned word split across chunks never reaches the caller; and a
// session with no guardrail streams exactly what the model sent.

// lookupCall is the tool call the models in these tests make with their text.
func lookupCall() []types.MessageToolCall {
	return []types.MessageToolCall{{ID: "call-1", Name: "lookup", Args: json.RawMessage(`{"who":"ada"}`)}}
}

// oneChunkToolProvider streams a reply the way Gemini does: its text and its
// function call arrive in one chunk. After a tool result it answers "done".
type oneChunkToolProvider struct {
	*mock.ToolProvider
	text  string
	calls atomic.Int32
}

func newOneChunkToolProvider(text string) *oneChunkToolProvider {
	return &oneChunkToolProvider{ToolProvider: mock.NewToolProvider("gem", "gem-model", false, nil), text: text}
}

func (p *oneChunkToolProvider) SupportsStreaming() bool { return true }

func (p *oneChunkToolProvider) PredictStreamWithTools(
	_ context.Context, req providers.PredictionRequest, _ interface{}, _ string,
) (<-chan providers.StreamChunk, error) {
	p.calls.Add(1)
	out := make(chan providers.StreamChunk, 1)
	if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "tool" {
		stop := types.FinishReasonStop
		out <- providers.StreamChunk{Content: "done", Delta: "done", FinishReason: &stop}
	} else {
		toolUse := types.FinishReasonToolUse
		out <- providers.StreamChunk{Content: p.text, Delta: p.text, ToolCalls: lookupCall(), FinishReason: &toolUse}
	}
	close(out)
	return out, nil
}

// lookupCounter counts the lookup calls a conversation executes.
func lookupCounter(conv *Conversation) *atomic.Int32 {
	var n atomic.Int32
	conv.OnTool("lookup", func(map[string]any) (any, error) {
		n.Add(1)
		return map[string]any{"found": true}, nil
	})
	return &n
}

// streamOneChunkTurn streams one turn of a conversation on a one-chunk tool
// model and returns what the caller saw, the lookups run and the model calls.
func streamOneChunkTurn(t *testing.T, text string, opts ...Option) (streamedTurn, int32, int32) {
	t.Helper()
	prov := newOneChunkToolProvider(text)
	conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
		append([]Option{WithProvider(prov), WithSkipSchemaValidation()}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	lookups := lookupCounter(conv)
	var got streamedTurn
	for c := range conv.Stream(context.Background(), "who is ada?") {
		require.NoError(t, c.Error)
		switch c.Type {
		case ChunkText:
			got.deltas = append(got.deltas, c.Text)
			got.replaces = append(got.replaces, c.ReplacesText)
		case ChunkDone:
			got.resp = c.Message
		}
	}
	require.NotNil(t, got.resp)
	return got, lookups.Load(), prov.calls.Load()
}

// A streamed reply a chunk guardrail replaces does not run the tool calls
// that came with its text, as a whole reply it blocks does not: the turn ends
// with the guardrail's message, and the model is not called again.
func TestStream_ReplacedReplyRunsNoToolCalls(t *testing.T) {
	got, lookups, calls := streamOneChunkTurn(t, "Let me check pineapple prices.", noPineapple())

	assert.Equal(t, int32(0), lookups, "a blocked reply's tool calls must not run")
	assert.Equal(t, int32(1), calls, "the turn ends with the block")
	assert.Equal(t, []string{noPineappleMessage}, got.deltas)
	assert.Equal(t, noPineappleMessage, got.resp.Text())
	assert.Empty(t, got.resp.Message().ToolCalls)
	assert.Equal(t, types.FinishReasonSafety, got.resp.Message().FinishReason)
}

// A truncated streamed reply's tool calls do not run either.
func TestStream_TruncatedReplyRunsNoToolCalls(t *testing.T) {
	got, lookups, calls := streamOneChunkTurn(t, "Let me look that up.",
		WithGuardrail(guardrails.Output("max_length", map[string]any{"max_characters": 6})))

	assert.Equal(t, int32(0), lookups, "a truncated reply's tool calls must not run")
	assert.Equal(t, int32(1), calls)
	assert.Equal(t, "Let me", got.resp.Text())
	assert.Equal(t, []string{"Let me"}, got.deltas)
	assert.Empty(t, got.resp.Message().ToolCalls)
}

// Unguarded, the same reply runs its tool call and the turn goes on.
func TestStream_UnguardedOneChunkReplyRunsItsToolCall(t *testing.T) {
	got, lookups, calls := streamOneChunkTurn(t, "Let me check pineapple prices.")

	assert.Equal(t, int32(1), lookups)
	assert.Equal(t, int32(2), calls)
	assert.Equal(t, "done", got.resp.Text())
}

// failingCheck is a streaming eval type whose every check fails with an
// error, as a classifier that is down does.
type failingCheck struct{}

func (failingCheck) Type() string { return "failing_check" }

func (failingCheck) Eval(context.Context, *evals.EvalContext, map[string]any) (*evals.EvalResult, error) {
	return nil, errors.New("classifier unavailable")
}

func (failingCheck) EvalPartial(context.Context, string, map[string]any) (*evals.EvalResult, error) {
	return nil, errors.New("classifier unavailable")
}

const failingCheckMessage = "I can't check that right now."

// failingGuardrail is an output guardrail whose check fails.
func failingGuardrail() []Option {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(failingCheck{})
	return []Option{
		WithEvalRegistry(registry),
		WithGuardrail(guardrails.Output("failing_check", nil, guardrails.WithMessage(failingCheckMessage))),
	}
}

// validationEvents records the output validation events, as
// "<event>/<type>".
type validationEvents struct {
	mu  sync.Mutex
	got []string
}

func newValidationEvents(t *testing.T) (*validationEvents, events.Bus) {
	t.Helper()
	v := &validationEvents{}
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	for _, et := range []events.EventType{
		events.EventValidationStarted, events.EventValidationPassed, events.EventValidationFailed,
	} {
		bus.Subscribe(et, func(e *events.Event) {
			d, ok := e.Data.(*events.ValidationEventData)
			if !ok || d.Direction != hooks.DirectionOutput {
				return
			}
			key := string(e.Type) + "/" + d.ValidatorType
			v.mu.Lock()
			v.got = append(v.got, key)
			v.mu.Unlock()
		})
	}
	return v, bus
}

// settled waits for n events, then for the bus to deliver any stragglers,
// and returns every event recorded.
func (v *validationEvents) settled(t *testing.T, n int) []string {
	t.Helper()
	require.Eventually(t, func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		return len(v.got) >= n
	}, 5*time.Second, time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.got...)
}

// A streaming check that fails gives the guardrail's message on a streamed
// reply, as it does on a whole one: the turn does not error, none of the
// unjudged reply reaches the caller, and the check reports its failure.
func TestStream_FailedChunkCheckGivesTheGuardrailMessage(t *testing.T) {
	rec, bus := newValidationEvents(t)
	got := streamChunks(t, []string{"Hello", " there."}, append(failingGuardrail(), WithEventBus(bus))...)

	assert.Equal(t, []string{failingCheckMessage}, got.deltas)
	assert.Equal(t, []bool{true}, got.replaces)
	assert.Equal(t, failingCheckMessage, got.resp.Text())
	assert.Equal(t, types.FinishReasonSafety, got.resp.Message().FinishReason)
	require.Len(t, got.resp.Validations(), 1)
	assert.Equal(t, "error", got.resp.Validations()[0].Details["reason"])
	// A chunk's check reports only its firing: no started or passed per
	// chunk, and the replaced reply is not judged again.
	assert.Equal(t, []string{"validation.failed/failing_check"}, rec.settled(t, 1))
}

// The same check fails a whole reply (Send) with the same message.
func TestSend_FailedCheckGivesTheGuardrailMessage(t *testing.T) {
	repo := &sequenceRepo{turns: []mock.Turn{{Type: "text", Content: "Hello there."}}}
	conv, err := Open(writeGatePack(t), "assist", append([]Option{
		WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)),
		WithSkipSchemaValidation(),
	}, failingGuardrail()...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "hi")

	require.NoError(t, err)
	assert.Equal(t, failingCheckMessage, resp.Text())
	assert.Equal(t, types.FinishReasonSafety, resp.Message().FinishReason)
}

// denyingInterceptor denies every chunk without enforcing, as a host's own
// chunk hook may.
type denyingInterceptor struct{}

func (denyingInterceptor) Name() string { return "denying" }
func (denyingInterceptor) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}
func (denyingInterceptor) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}
func (denyingInterceptor) OnChunk(context.Context, *providers.StreamChunk) hooks.Decision {
	return hooks.Deny("not on my watch")
}

// A chunk hook that denies ends a streamed reply with the blocked message, as
// a realtime one does; the stream does not abort.
func TestStream_ChunkDenyGivesTheBlockedMessage(t *testing.T) {
	got := streamChunks(t, []string{"Hello", " there."}, WithProviderHook(denyingInterceptor{}))

	assert.Equal(t, []string{prompt.DefaultBlockedMessage}, got.deltas)
	assert.Equal(t, prompt.DefaultBlockedMessage, got.resp.Text())
	assert.Equal(t, types.FinishReasonSafety, got.resp.Message().FinishReason)
}

// truncateThenBan is a length guardrail that truncates to "Pineapple " and a
// content guardrail that then bans what is left: both fire on one chunk.
func truncateThenBan() Option {
	return WithGuardrail(
		guardrails.Output("max_length", map[string]any{"max_characters": 10}),
		guardrails.Output("content_excludes", map[string]any{"patterns": []any{"pineapple"}},
			guardrails.WithMessage(noPineappleMessage)),
	)
}

// validationTypes lists the types of the failed validations in vs.
func validationTypes(vs []types.ValidationResult) []string {
	var out []string
	for _, v := range vs {
		if !v.Passed {
			out = append(out, v.ValidatorType)
		}
	}
	return out
}

// Two guardrails that fire on one chunk are both recorded: the truncation the
// later one replaced keeps its own validation.
func TestStream_EveryGuardrailFiringOnAChunkIsRecorded(t *testing.T) {
	got := streamChunks(t, []string{"Pineapple pizza is great."}, truncateThenBan())

	assert.Equal(t, noPineappleMessage, got.resp.Text())
	assert.Equal(t, []string{"max_length", "content_excludes"}, validationTypes(got.resp.Validations()))
}

// A session with no guardrail streams the model's deltas exactly as sent.
func TestStream_UnguardedDeltasAreTheModelsOwn(t *testing.T) {
	chunks := []string{"P", "ine", "apple is", " the best", " topping."}
	got := streamChunks(t, chunks)

	assert.Equal(t, chunks, got.deltas)
	assert.Equal(t, "Pineapple is the best topping.", got.resp.Text())
}

// --- Realtime ---

// realtimeGuardSession opens a realtime session on a mock provider with
// opts, and returns its response channel, its mock session and its store.
func realtimeGuardSession(
	t *testing.T, pack, promptName string, opts ...Option,
) (*Conversation, <-chan providers.StreamChunk, *mock.MockStreamSession, statestore.Store) {
	t.Helper()
	store := statestore.NewMemoryStore()
	prov := mock.NewStreamingProvider("rt", "rt-model", false)
	conv, err := OpenDuplex(pack, promptName, append([]Option{
		WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithStateStore(store), WithConversationID("guarded"),
	}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	ch, err := conv.Response()
	require.NoError(t, err)
	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: make([]byte, 320)},
	}))
	require.Eventually(t, func() bool { return prov.GetSession() != nil }, 5*time.Second, time.Millisecond)
	return conv, ch, prov.GetSession(), store
}

// spoken is a chunk of a realtime reply's transcript.
func spoken(text string) *providers.StreamChunk {
	return &providers.StreamChunk{Delta: text, Metadata: map[string]interface{}{"type": "output_transcription"}}
}

// storedReply is the stored assistant message of the session's first turn.
func storedReply(t *testing.T, store statestore.Store) types.Message {
	t.Helper()
	var reply types.Message
	require.Eventually(t, func() bool {
		state, err := store.Load(context.Background(), "guarded")
		if err != nil || state == nil {
			return false
		}
		for _, m := range state.Messages {
			if m.Role == roleAssistantForTest {
				reply = m
				return true
			}
		}
		return false
	}, 5*time.Second, 5*time.Millisecond)
	return reply
}

const roleAssistantForTest = "assistant"

// A realtime reply a chunk guardrail replaces does not run the tool calls
// that came with its text.
func TestDuplex_ReplacedReplyRunsNoToolCalls(t *testing.T) {
	conv, ch, sess, store := realtimeGuardSession(t, createTestPackFile(t, callerToolsPack), "chat", noPineapple())
	lookups := lookupCounter(conv)

	toolUse := types.FinishReasonToolUse
	c := spoken("Let me check pineapple prices.")
	c.ToolCalls = lookupCall()
	c.FinishReason = &toolUse
	sess.EmitChunk(c)

	got := realtimeGuardCut(t, ch, 1)
	assert.Equal(t, []string{"interrupted", "replace:" + noPineappleMessage, "end:safety"}, got)
	reply := storedReply(t, store)
	assert.Equal(t, noPineappleMessage, reply.Content)
	assert.Empty(t, reply.ToolCalls)
	assert.Equal(t, int32(0), lookups.Load(), "a blocked reply's tool calls must not run")
}

// A realtime reply's failed streaming check gives the guardrail's message,
// as on the text paths, and reports its failure.
func TestDuplex_FailedChunkCheckGivesTheGuardrailMessage(t *testing.T) {
	rec, bus := newValidationEvents(t)
	_, ch, sess, store := realtimeGuardSession(t, writeIngestionTestPack(t), "main",
		append(failingGuardrail(), WithEventBus(bus))...)

	stop := types.FinishReasonStop
	sess.EmitChunk(spoken("Hello"))
	sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})

	got := realtimeGuardCut(t, ch, 1)
	assert.Equal(t, []string{"interrupted", "replace:" + failingCheckMessage, "end:safety"}, got)
	assert.Equal(t, failingCheckMessage, storedReply(t, store).Content)
	// A chunk's check reports only its firing: no started or passed per
	// chunk, and the replaced reply is not judged again.
	assert.Equal(t, []string{"validation.failed/failing_check"}, rec.settled(t, 1))
}

// Two guardrails firing on one realtime chunk are both recorded.
func TestDuplex_EveryGuardrailFiringOnAChunkIsRecorded(t *testing.T) {
	_, ch, sess, store := realtimeGuardSession(t, writeIngestionTestPack(t), "main", truncateThenBan())

	stop := types.FinishReasonStop
	sess.EmitChunk(spoken("Pineapple pizza is great."))
	sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})

	realtimeGuardCut(t, ch, 1)
	reply := storedReply(t, store)
	assert.Equal(t, noPineappleMessage, reply.Content)
	assert.Equal(t, []string{"max_length", "content_excludes"}, validationTypes(reply.Validations))
}

// A banned word split across realtime transcript chunks never reaches the
// caller as text: its possible start is held until the word is complete.
func TestDuplex_BlockedWordSplitAcrossChunksNeverReachesCaller(t *testing.T) {
	_, ch, sess, _ := realtimeGuardSession(t, writeIngestionTestPack(t), "main", noPineapple())

	stop := types.FinishReasonStop
	for _, c := range []*providers.StreamChunk{
		spoken("Say P"), spoken("ine"), spoken("apple, please."), {FinishReason: &stop},
	} {
		sess.EmitChunk(c)
	}

	got := realtimeGuardCut(t, ch, 1)
	assert.Equal(t, []string{"text:Say ", "interrupted", "replace:" + noPineappleMessage, "end:safety"}, got)
	for _, g := range got {
		assert.NotContains(t, strings.ToLower(g), "p"+"ine", "no part of the banned word reaches the caller")
	}
}

// A realtime session with no guardrail forwards the model's text exactly as
// sent.
func TestDuplex_UnguardedDeltasAreTheModelsOwn(t *testing.T) {
	_, ch, sess, _ := realtimeGuardSession(t, writeIngestionTestPack(t), "main")

	stop := types.FinishReasonStop
	for _, c := range []*providers.StreamChunk{
		spoken("Say P"), spoken("ine"), spoken("apple, please."), {FinishReason: &stop},
	} {
		sess.EmitChunk(c)
	}

	assert.Equal(t, []string{"text:Say P", "text:ine", "text:apple, please.", "end:stop"},
		realtimeGuardCut(t, ch, 1))
}
