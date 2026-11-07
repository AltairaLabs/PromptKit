package stage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// duplexStageWithHooks builds a duplex stage wired to reg, with an emitter on
// a fresh bus. The returned getter flushes the bus and returns the
// validation.failed events.
func duplexStageWithHooks(
	t *testing.T, reg *hooks.Registry,
) (*DuplexProviderStage, *recordingSession, func() []*events.ValidationEventData) {
	t.Helper()
	bus := events.NewEventBus()
	getEvents := collectValidationFailures(t, bus)
	emitter := events.NewEmitter(bus, "run1", "sess1", "conv1")
	s := NewDuplexProviderStageWithEmitter(providersmock.NewStreamingProvider("t", "m", false), baseConfig(), emitter)
	sess := newRecordingSession()
	s.session = sess
	s.systemPromptSent = true
	s.SetHookRegistry(reg)
	return s, sess, getEvents
}

// feedChunks runs chunks through handleResponseChunk and returns the emitted
// elements and the first error.
func feedChunks(t *testing.T, s *DuplexProviderStage, chunks ...providers.StreamChunk) ([]StreamElement, error) {
	t.Helper()
	out := make(chan StreamElement, 64)
	var err error
	for i := range chunks {
		if err = s.handleResponseChunk(context.Background(), &chunks[i], out); err != nil {
			break
		}
	}
	close(out)
	var elems []StreamElement
	for e := range out {
		elems = append(elems, e)
	}
	return elems, err
}

func stopChunk() providers.StreamChunk {
	stop := "stop"
	return providers.StreamChunk{FinishReason: &stop}
}

func transcriptChunk(text string, audio []byte) providers.StreamChunk {
	c := providers.StreamChunk{Delta: text, Metadata: map[string]interface{}{"type": "output_transcription"}}
	if audio != nil {
		c.MediaData = &providers.StreamMediaData{Data: audio}
	}
	return c
}

// turnMessage returns the assistant Message on the turn-completing element.
func turnMessage(t *testing.T, elems []StreamElement) (*types.Message, StreamElement) {
	t.Helper()
	for _, e := range elems {
		if e.EndOfStream && e.Message != nil && e.Message.Role == roleAssistant {
			return e.Message, e
		}
	}
	require.FailNow(t, "no assistant turn element")
	return nil, StreamElement{}
}

func validationTypes(vs []types.ValidationResult) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.ValidatorType)
	}
	return out
}

// An enforcing AfterCall hook rewrites the completed realtime turn as it
// rewrites a text turn: content replaced, tool calls dropped, finish reason
// safety. The duplex message's text part and Meta finish reason follow, and
// the audio the session played is kept (#1682).
func TestDuplexHooks_AfterCallEnforcedRewritesTurn(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceAfterCallHook{replacement: "I can't say that."}))
	s, _, getEvents := duplexStageWithHooks(t, reg)

	final := stopChunk()
	final.ToolCalls = []types.MessageToolCall{{ID: "c1", Name: "transfer_funds"}}
	final.MediaData = &providers.StreamMediaData{Data: pcm(64)}
	elems, err := feedChunks(t, s, providers.StreamChunk{Content: "the secret plan"}, final)
	require.NoError(t, err)

	msg, elem := turnMessage(t, elems)
	assert.Equal(t, "I can't say that.", msg.Content)
	assert.Empty(t, msg.ToolCalls, "an enforced turn's tool calls must not reach the executor")
	assert.Equal(t, types.FinishReasonSafety, msg.FinishReason)
	assert.Equal(t, types.FinishReasonSafety, msg.Meta[finishReasonMetaKey])
	require.NotNil(t, elem.Meta.FinishReason)
	assert.Equal(t, types.FinishReasonSafety, *elem.Meta.FinishReason)
	require.Len(t, msg.Parts, 2)
	assert.Equal(t, "I can't say that.", *msg.Parts[0].Text)
	assert.Equal(t, types.ContentTypeAudio, msg.Parts[1].Type)
	assert.Equal(t, []string{"test_output"}, validationTypes(msg.Validations))

	got := getEvents()
	require.Len(t, got, 1)
	assert.Equal(t, hooks.DirectionOutput, got[0].Direction)
}

// A hook that denies without enforcing fails the realtime turn as it fails a
// text turn: no assistant message, an error element, and the error returned.
func TestDuplexHooks_AfterCallDenyFailsTurn(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&allowBeforeDenyAfterHook{reason: "nope"}))
	s, _, _ := duplexStageWithHooks(t, reg)

	elems, err := feedChunks(t, s, providers.StreamChunk{Content: "hello"}, stopChunk())
	var denied *hooks.HookDeniedError
	require.ErrorAs(t, err, &denied)
	assert.Equal(t, providerHookTypeAfter, denied.HookType)

	for _, e := range elems {
		if e.Message != nil {
			assert.NotEqual(t, roleAssistant, e.Message.Role, "a denied turn must not be emitted")
		}
	}
	last := elems[len(elems)-1]
	require.ErrorAs(t, last.Error, &denied)
}

// A streaming guardrail checks each transcript delta before it is forwarded.
// Once it enforces, the turn's text is the enforced text and the rest of the
// turn's text and audio is dropped, the realtime equivalent of the text path
// no longer reading the stream.
func TestDuplexHooks_ChunkEnforcedDropsRestOfTurn(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("banned_words", map[string]any{"words": []any{"forbidden"}})
	require.NoError(t, err)
	s, _, getEvents := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(guard)))
	s.turnIDQueue = []string{"t1"} // scenario path: no user-turn materialization

	elems, err := feedChunks(t, s,
		transcriptChunk("this is ", pcm(10)),
		transcriptChunk("forbidden ", pcm(20)),
		transcriptChunk("and more", pcm(30)),
		stopChunk(),
	)
	require.NoError(t, err)

	var texts []string
	audioBytes := 0
	for _, e := range elems {
		if e.Text != nil {
			texts = append(texts, *e.Text)
		}
		if e.Audio != nil {
			audioBytes += len(e.Audio.Samples)
		}
	}
	assert.Equal(t, []string{"this is "}, texts, "nothing from the blocked chunk on is forwarded")
	assert.Equal(t, 10, audioBytes, "the blocked turn's later audio is dropped")

	msg, _ := turnMessage(t, elems)
	assert.NotContains(t, msg.Content, "forbidden")
	assert.NotContains(t, msg.Content, "and more")
	assert.NotEmpty(t, msg.Content)
	assert.Equal(t, types.FinishReasonSafety, msg.FinishReason)
	assert.Equal(t, []string{"banned_words"}, validationTypes(msg.Validations),
		"the streaming firing is recorded once on the turn")
	assert.Len(t, getEvents(), 1)
}

// A turn cut short after a streaming firing keeps the firing on its partial
// reply, and the next turn starts unblocked.
func TestDuplexHooks_InterruptedTurnKeepsChunkFiring(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("banned_words", map[string]any{"words": []any{"forbidden"}})
	require.NoError(t, err)
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(guard)))
	s.turnIDQueue = []string{"t1", "t2"}

	elems, err := feedChunks(t, s,
		transcriptChunk("forbidden", nil),
		providers.StreamChunk{Interrupted: true},
		transcriptChunk("fine", nil),
		stopChunk(),
	)
	require.NoError(t, err)

	var partial *types.Message
	for _, e := range elems {
		if e.Meta.Interrupted && e.Message != nil {
			partial = e.Message
		}
	}
	require.NotNil(t, partial)
	assert.Equal(t, []string{"banned_words"}, validationTypes(partial.Validations))

	msg, _ := turnMessage(t, elems)
	assert.Equal(t, "fine", msg.Content)
	assert.Empty(t, msg.Validations)
	assert.NotEqual(t, types.FinishReasonSafety, msg.FinishReason)
}

// A streaming interceptor that denies without enforcing fails the turn: as in
// text, none of the blocked output is kept.
func TestDuplexHooks_ChunkDenyAbortsTurn(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&maxCharsChunkInterceptor{maxChars: 3}))
	s, _, _ := duplexStageWithHooks(t, reg)
	s.turnIDQueue = []string{"t1"}

	elems, err := feedChunks(t, s, transcriptChunk("too long", nil), stopChunk())
	var abort *providers.ValidationAbortError
	require.ErrorAs(t, err, &abort)
	for _, e := range elems {
		assert.Nil(t, e.Message, "a blocked turn keeps no message")
		assert.Nil(t, e.Text, "the blocked text is not forwarded")
	}
	require.NotEmpty(t, elems)
	require.ErrorAs(t, elems[len(elems)-1].Error, &abort)
	assert.Zero(t, s.accumulatedText.Len())
}

// Text bound for the session is checked before it is sent. An enforcing input
// guardrail keeps it from the session and emits the blocked turn in its place.
func TestDuplexHooks_InputEnforcedBlocksText(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{
		reason: "off topic", replacement: "Not here.",
		metadata: map[string]any{"validator_type": "test_input"},
	}))
	s, sess, getEvents := duplexStageWithHooks(t, reg)
	turnID := "t1"
	s.turnIDQueue = []string{turnID}

	text := "something off topic"
	elem := StreamElement{Text: &text, EndOfStream: true}
	elem.Meta.TurnID = &turnID
	out := make(chan StreamElement, 8)
	done := make(chan error, 1)
	require.False(t, s.handleInputElement(context.Background(), &elem, out, done))
	assert.Empty(t, out, "the response loop, not the input goroutine, emits the blocked turn")
	close(sess.respCh)
	require.NoError(t, s.forwardResponseElements(context.Background(), out))
	close(out)

	sess.mu.Lock()
	assert.Empty(t, sess.texts, "blocked text must not reach the session")
	assert.Zero(t, sess.endInputCount, "a blocked turn must not ask the provider for a response")
	sess.mu.Unlock()
	assert.Empty(t, s.turnIDQueue, "the answered turn's id is not left queued")

	var elems []StreamElement
	for e := range out {
		elems = append(elems, e)
	}
	require.Len(t, elems, 2)
	require.NotNil(t, elems[0].Text)
	assert.Equal(t, "Not here.", *elems[0].Text)
	msg, _ := turnMessage(t, elems)
	assert.Equal(t, "Not here.", msg.Content)
	assert.Equal(t, types.FinishReasonSafety, msg.FinishReason)
	assert.Equal(t, []string{"test_input"}, validationTypes(msg.Validations))

	got := getEvents()
	require.Len(t, got, 1)
	assert.Equal(t, hooks.DirectionInput, got[0].Direction)
}

// An input hook that denies without enforcing ends the session with the
// error, as it fails a text turn.
func TestDuplexHooks_InputDenyEndsSession(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&denyAllProviderHook{reason: "no"}))
	s, sess, _ := duplexStageWithHooks(t, reg)

	text := "hello"
	out := make(chan StreamElement, 4)
	done := make(chan error, 1)
	require.True(t, s.handleInputElement(context.Background(), &StreamElement{Text: &text}, out, done))

	var denied *hooks.HookDeniedError
	require.ErrorAs(t, <-done, &denied)
	require.Len(t, out, 1)
	require.ErrorAs(t, (<-out).Error, &denied)
	sess.mu.Lock()
	assert.Empty(t, sess.texts)
	assert.Equal(t, 1, sess.closeCount, "the session is closed so the response loop ends")
	sess.mu.Unlock()
}

// capturingHook records what each AfterCall saw.
type capturingHook struct {
	mu    sync.Mutex
	calls []hooks.ProviderRequest
}

func (h *capturingHook) Name() string { return "capture" }

func (h *capturingHook) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *capturingHook) AfterCall(
	_ context.Context, req *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, *req)
	return hooks.Allow
}

// The hooks see the conversation so far and the turn being judged, as a text
// turn's do: each AfterCall gets the session's earlier messages and its own
// turn index.
func TestDuplexHooks_SeeHistoryAndTurnIndex(t *testing.T) {
	h := &capturingHook{}
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(h)))
	s.turnState = NewTurnState()
	s.turnState.SystemPrompt = "be brief"

	send := func(text string) {
		out := make(chan StreamElement, 4)
		require.False(t, s.handleInputElement(context.Background(), &StreamElement{Text: &text}, out, make(chan error, 1)))
	}
	send("hi")
	_, err := feedChunks(t, s, providers.StreamChunk{Content: "hello"}, stopChunk())
	require.NoError(t, err)
	send("again")
	_, err = feedChunks(t, s, providers.StreamChunk{Content: "ok"}, stopChunk())
	require.NoError(t, err)

	require.Len(t, h.calls, 2)
	contents := func(msgs []types.Message) []string {
		var out []string
		for _, m := range msgs {
			out = append(out, m.Role+":"+m.Content)
		}
		return out
	}
	assert.Equal(t, []string{"user:hi"}, contents(h.calls[0].Messages))
	assert.Equal(t, 0, h.calls[0].TurnIndex)
	assert.Equal(t, []string{"user:hi", "assistant:hello", "user:again"}, contents(h.calls[1].Messages))
	assert.Equal(t, 1, h.calls[1].TurnIndex)
	assert.Equal(t, "be brief", h.calls[1].SystemPrompt)
	assert.Equal(t, "t", h.calls[1].ProviderID)
}

// Without a registry nothing changes: the turn is emitted as the provider
// produced it.
func TestDuplexHooks_NilRegistryLeavesTurnAlone(t *testing.T) {
	s := newDuplexStageForUnit()
	s.SetHookRegistry(nil)
	final := stopChunk()
	final.ToolCalls = []types.MessageToolCall{{ID: "c1", Name: "lookup"}}
	elems, err := feedChunks(t, s, providers.StreamChunk{Content: "forbidden"}, final)
	require.NoError(t, err)
	msg, _ := turnMessage(t, elems)
	assert.Equal(t, "forbidden", msg.Content)
	assert.Len(t, msg.ToolCalls, 1)
	assert.Empty(t, s.history.snapshot(), "no history is kept when no hooks run")
}

func TestReplaceTextParts(t *testing.T) {
	audio := types.ContentPart{Type: types.ContentTypeAudio}
	a, b := "a", "b"
	got := replaceTextParts([]types.ContentPart{
		{Type: types.ContentTypeText, Text: &a}, audio, {Type: types.ContentTypeText, Text: &b},
	}, "new")
	require.Len(t, got, 2)
	assert.Equal(t, "new", *got[0].Text)
	assert.Equal(t, types.ContentTypeAudio, got[1].Type)

	got = replaceTextParts([]types.ContentPart{audio}, "new")
	require.Len(t, got, 2)
	assert.Equal(t, "new", *got[0].Text, "a text part is added ahead of the audio")

	got = replaceTextParts([]types.ContentPart{{Type: types.ContentTypeText, Text: &a}, audio}, "")
	assert.Equal(t, []types.ContentPart{audio}, got, "empty content leaves no text part")
}

// A spoken user turn reaches the hooks' history too: the forwarded user
// Message on the scenario path, and the turn materialized from the provider's
// input transcript on the streaming path.
func TestDuplexHooks_HistoryHoldsSpokenUserTurns(t *testing.T) {
	h := &capturingHook{}
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(h)))

	out := make(chan StreamElement, 4)
	require.False(t, s.handleInputElement(context.Background(),
		&StreamElement{Message: &types.Message{Role: roleUser, Content: "from scenario"}},
		out, make(chan error, 1)))
	_, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "spoken words",
		}},
		providers.StreamChunk{Content: "reply"},
		stopChunk(),
	)
	require.NoError(t, err)

	require.Len(t, h.calls, 1)
	var got []string
	for _, m := range h.calls[0].Messages {
		got = append(got, m.Role+":"+m.Content)
	}
	assert.Equal(t, []string{"user:from scenario", "user:spoken words"}, got)
}

// A blocked input turn waits for the reply in progress to complete, so its
// turn boundary cannot land inside that reply.
func TestDuplexHooks_BlockedInputWaitsForReplyInProgress(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{replacement: "Not here."}))
	s, sess, _ := duplexStageWithHooks(t, reg)
	s.turnIDQueue = []string{"t1"}
	s.accumulatedText.WriteString("half a reply") // a reply is streaming

	out := make(chan StreamElement, 16)
	loopDone := make(chan error, 1)
	go func() { loopDone <- s.forwardResponseElements(context.Background(), out) }()

	text := "blocked input"
	require.False(t, s.handleInputElement(context.Background(), &StreamElement{Text: &text}, out, make(chan error, 1)))
	require.Never(t, func() bool { return len(out) > 0 }, 50*time.Millisecond, 5*time.Millisecond,
		"nothing is emitted while the reply is in progress")

	sess.respCh <- stopChunk()
	close(sess.respCh)
	require.NoError(t, <-loopDone)
	close(out)

	var turns []string
	for e := range out {
		if e.EndOfStream && e.Message != nil {
			turns = append(turns, e.Message.Content)
		}
	}
	assert.Equal(t, []string{"half a reply", "Not here."}, turns)
}

// A blocked element's tool responses still reach the session.
func TestDuplexHooks_BlockedInputStillSendsToolResponses(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{replacement: "Not here."}))
	s, sess, _ := duplexStageWithHooks(t, reg)
	text := "blocked"
	elem := StreamElement{Text: &text}
	elem.Meta.ToolResponses = []providers.ToolResponse{{ToolCallID: "c1", Result: "{}"}}
	require.False(t, s.handleInputElement(context.Background(), &elem, make(chan StreamElement, 4), make(chan error, 1)))
	sess.mu.Lock()
	defer sess.mu.Unlock()
	require.Len(t, sess.toolResponses, 1)
}

// After an input hook denies, its error element is the session's last output:
// the response loop does not then emit (and possibly block on) a partial reply.
func TestDuplexHooks_InputDenyIsLastOutput(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&denyAllProviderHook{reason: "no"}))
	s, _, _ := duplexStageWithHooks(t, reg)
	s.accumulatedText.WriteString("partial")
	text := "hello"
	out := make(chan StreamElement, 4)
	require.True(t, s.handleInputElement(context.Background(), &StreamElement{Text: &text}, out, make(chan error, 1)))
	require.Len(t, out, 1)
	<-out

	require.NoError(t, s.finishOnResponseClosed(context.Background(), out, true))
	assert.Empty(t, out)
}

// The history keeps text only: a realtime turn's audio part would otherwise
// hold every turn's audio for the life of the session. A registry with no
// provider hooks keeps no history at all.
func TestDuplexHooks_HistoryIsTextOnly(t *testing.T) {
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(&capturingHook{})))
	final := stopChunk()
	final.MediaData = &providers.StreamMediaData{Data: pcm(64)}
	_, err := feedChunks(t, s, providers.StreamChunk{Content: "spoken"}, final)
	require.NoError(t, err)
	hist := s.history.snapshot()
	require.Len(t, hist, 1)
	require.Len(t, hist[0].Parts, 1)
	assert.Equal(t, types.ContentTypeText, hist[0].Parts[0].Type)

	toolOnly, _, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithToolHook(&denyToolHook{})))
	_, err = feedChunks(t, toolOnly, providers.StreamChunk{Content: "spoken"}, stopChunk())
	require.NoError(t, err)
	assert.Empty(t, toolOnly.history.snapshot())
}

// On the scenario path the user Message is forwarded before its audio is
// transcribed; the transcript that arrives with the turn fills it in, so the
// output hooks see what the user said.
func TestDuplexHooks_ScenarioUserTurnGetsItsTranscript(t *testing.T) {
	h := &capturingHook{}
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(h)))
	turnID := "t1"
	user := StreamElement{Message: &types.Message{Role: roleUser}}
	user.Meta.TurnID = &turnID
	require.False(t, s.handleInputElement(context.Background(), &user, make(chan StreamElement, 4), make(chan error, 1)))

	_, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "book a table",
		}},
		providers.StreamChunk{Content: "done"},
		stopChunk(),
	)
	require.NoError(t, err)
	require.Len(t, h.calls, 1)
	require.Len(t, h.calls[0].Messages, 1)
	assert.Equal(t, "book a table", h.calls[0].Messages[0].Content)
}
