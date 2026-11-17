package stage

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A guardrail that blocks a realtime reply ends the turn, not the session. It
// cuts the reply where it can: the provider is asked to stop it and cut its own
// copy to what the caller heard (providers.ResponseInterrupter), and the
// caller is told to stop playing what it buffered, with the same Interrupt
// signal a barge-in gives. The next turn runs normally.

// interruptingSession is a recordingSession that also stops replies on
// request, recording each call.
type interruptingSession struct {
	*recordingSession
	gens []int64
}

func (s *interruptingSession) InterruptResponse(_ context.Context, generation int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gens = append(s.gens, generation)
	return nil
}

func (s *interruptingSession) interruptCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.gens)
}

func (s *interruptingSession) interruptedGens() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.gens...)
}

// cutStage is a duplex stage on an interruptingSession with reg's hooks, on
// the streaming path (no scenario turn ids).
func cutStage(t *testing.T, reg *hooks.Registry) (*DuplexProviderStage, *interruptingSession) {
	t.Helper()
	s, rec, _ := duplexStageWithHooks(t, reg)
	sess := &interruptingSession{recordingSession: rec}
	s.session = sess
	return s, sess
}

// trace renders what the caller receives, in order: "text:<t>", "audio:<n>",
// "interrupt", "user:<t>", "end:<content>|<finish reason>", "error".
func trace(elems []StreamElement) []string {
	var out []string
	for _, e := range elems {
		switch {
		case e.Error != nil:
			out = append(out, "error")
		case e.Interrupt:
			out = append(out, "interrupt")
		case e.Message != nil && e.Message.Role == roleUser:
			out = append(out, "user:"+e.Message.Content)
		case e.EndOfStream && e.Message != nil:
			out = append(out, "end:"+e.Message.Content+"|"+e.Message.FinishReason)
		}
		if e.Text != nil {
			out = append(out, "text:"+*e.Text)
		}
		if e.Audio != nil && len(e.Audio.Samples) > 0 {
			out = append(out, "audio:"+strconv.Itoa(len(e.Audio.Samples)))
		}
	}
	return out
}

func bannedWordsHook(t *testing.T, word string) hooks.ProviderHook {
	t.Helper()
	guard, err := guardrails.NewGuardrailHook("banned_words", map[string]any{"words": []any{word}},
		guardrails.WithMessage("I can't say that."))
	require.NoError(t, err)
	return guard
}

// finalTranscript is a provider's final transcript of the caller's speech.
func finalTranscript(text string) providers.StreamChunk {
	return providers.StreamChunk{Metadata: map[string]interface{}{
		"type": "input_transcription", "transcription": text, "transcription_final": true,
	}}
}

// A streaming guardrail block cuts the reply: the provider is asked once to
// stop it, the caller gets the interrupt before anything else of the turn,
// and the second turn streams normally.
func TestDuplexGuardrailCut_StreamingBlockInterruptsAndSessionContinues(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))

	elems, err := feedChunks(t, s,
		transcriptChunk("this is ", pcm(10)),
		transcriptChunk("forbidden ", pcm(20)),
		transcriptChunk("and more", pcm(30)),
		stopChunk(),
		transcriptChunk("all fine", pcm(12)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:this is ", "audio:10",
		"interrupt", "text:I can't say that.",
		"end:I can't say that.|safety",
		"text:all fine", "audio:12",
		"end:all fine|stop",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount(), "the provider is asked once to stop the blocked reply")
}

// A streaming interceptor that denies without enforcing ends the turn the
// same way, with the default blocked message, not the session.
func TestDuplexGuardrailCut_StreamingDenyEndsTheTurnOnly(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&maxCharsChunkInterceptor{maxChars: 3})))

	elems, err := feedChunks(t, s,
		transcriptChunk("too long", pcm(10)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"interrupt", "text:" + prompt.DefaultBlockedMessage,
		"end:" + prompt.DefaultBlockedMessage + "|safety",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
	assert.False(t, s.turnGuard.blocked, "the next turn starts unblocked")
}

// A completed-turn guardrail that enforces stops what the caller still has
// buffered and cuts the provider's copy, before the turn's end is emitted.
func TestDuplexGuardrailCut_AfterCallEnforceInterrupts(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&enforceAfterCallHook{replacement: "No."})))

	elems, err := feedChunks(t, s, transcriptChunk("the plan", pcm(10)), stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{"text:the plan", "audio:10", "interrupt", "text:No.", "end:No.|safety"}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
}

// A completed-turn hook that denies without enforcing ends the turn with the
// blocked message and its firing; the session goes on to the next turn.
func TestDuplexGuardrailCut_AfterCallDenyEndsTheTurnOnly(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&allowBeforeDenyAfterHook{
		reason: "nope", metadata: map[string]any{"validator_type": "test_output"},
	}))
	s, sess := cutStage(t, reg)

	elems, err := feedChunks(t, s,
		transcriptChunk("hello", pcm(10)), stopChunk(),
		transcriptChunk("again", nil), stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:hello", "audio:10", "interrupt", "text:" + prompt.DefaultBlockedMessage,
		"end:" + prompt.DefaultBlockedMessage + "|safety",
		"text:again", "interrupt", "text:" + prompt.DefaultBlockedMessage,
		"end:" + prompt.DefaultBlockedMessage + "|safety",
	}, trace(elems))
	assert.Equal(t, 2, sess.interruptCount())
	msg, _ := turnMessage(t, elems)
	assert.Equal(t, []string{"test_output"}, validationTypes(msg.Validations))
	assert.Equal(t, map[string]interface{}{"validator_type": "test_output", finishReasonMetaKey: types.FinishReasonSafety},
		msg.Meta, "the blocked turn carries the decision's metadata, as a blocked typed turn does")
}

// wordInputHook blocks, before the call, a last user turn containing word.
type wordInputHook struct{ word string }

func (h *wordInputHook) Name() string { return "word_input" }

func (h *wordInputHook) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	var last string
	for _, m := range req.Messages {
		if m.Role == roleUser {
			last = m.GetContent()
		}
	}
	if !strings.Contains(last, h.word) {
		return hooks.Allow
	}
	req.Replacement = "I can't help with transfers."
	return hooks.Enforced("banned", map[string]any{"validator_type": "test_input"})
}

func (h *wordInputHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

// A blocked reply's text reaches the caller as live output, shaped as a
// blocked typed turn's: a text element carrying finish reason safety, after
// the interruption.
func TestDuplexGuardrailCut_BlockedTextIsLiveOutput(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))

	elems, err := feedChunks(t, s, transcriptChunk("forbidden", pcm(10)), stopChunk())
	require.NoError(t, err)

	var live, cuts []StreamElement
	for _, e := range elems {
		if e.Text != nil && e.Message == nil {
			live = append(live, e)
		}
		if e.Interrupt {
			cuts = append(cuts, e)
		}
	}
	require.Len(t, cuts, 1)
	require.NotNil(t, cuts[0].Meta.FinishReason, "the cut says it was a guardrail's")
	assert.Equal(t, types.FinishReasonSafety, *cuts[0].Meta.FinishReason)
	require.Len(t, live, 1)
	assert.Equal(t, "I can't say that.", *live[0].Text)
	require.NotNil(t, live[0].Meta.FinishReason)
	assert.Equal(t, types.FinishReasonSafety, *live[0].Meta.FinishReason)
}

// A typed input a hook denies without enforcing ends the turn, as a spoken
// one does: the blocked message answers it and the session stays open.
func TestDuplexGuardrailCut_TypedInputDenyEndsTheTurnOnly(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&denyAllProviderHook{reason: "no",
		metadata: map[string]any{"validator_type": "test_input"}}))
	s, sess := cutStage(t, reg)

	text := "hello"
	out := make(chan StreamElement, 8)
	done := make(chan error, 1)
	require.False(t, s.handleInputElement(context.Background(), &StreamElement{Text: &text}, out, done))
	assert.Empty(t, done, "the input goroutine goes on")
	close(sess.respCh)
	require.NoError(t, s.forwardResponseElements(context.Background(), out))
	close(out)
	var elems []StreamElement
	for e := range out {
		elems = append(elems, e)
	}

	assert.Equal(t, []string{"user:hello", "text:" + prompt.DefaultBlockedMessage,
		"end:" + prompt.DefaultBlockedMessage + "|safety"}, trace(elems))
	msg, _ := turnMessage(t, elems)
	assert.Equal(t, []string{"test_input"}, validationTypes(msg.Validations))
	sess.mu.Lock()
	defer sess.mu.Unlock()
	assert.Empty(t, sess.texts, "the denied text never reaches the session")
	assert.Zero(t, sess.closeCount, "the session stays open")
}

// A reply the provider canceled on barge-in (OpenAI Realtime's cancelled
// response.done) tells the caller to stop playing it, before its end.
func TestDuplexGuardrailCut_ProviderCanceledReplyInterruptsTheCaller(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry())
	interrupted := types.FinishReasonInterrupted

	elems, err := feedChunks(t, s,
		transcriptChunk("a long", pcm(10)),
		providers.StreamChunk{FinishReason: &interrupted},
		transcriptChunk("next", nil),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"text:a long", "audio:10", "interrupt", "end:a long|interrupted",
		"text:next", "end:next|stop"}, trace(elems))
	for _, e := range elems {
		if e.Interrupt {
			assert.Equal(t, types.FinishReasonInterrupted, *e.Meta.FinishReason, "a barge-in, not a guardrail's cut")
		}
	}
	assert.Zero(t, sess.interruptCount(), "the provider stopped the reply itself")
}

// The caller's spoken turn is checked once its transcript is final. A block
// after the reply began cuts the reply: the rest is dropped, the caller is
// interrupted, and the turn is the blocked message, marked with the input
// firing. The next spoken turn is answered normally.
func TestDuplexGuardrailCut_SpokenInputBlockCutsTheReply(t *testing.T) {
	s, rec, getEvents := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	sess := &interruptingSession{recordingSession: rec}
	s.session = sess

	elems, err := feedChunks(t, s,
		transcriptChunk("Sure, ", pcm(10)),
		finalTranscript("please arrange a wire transfer"),
		transcriptChunk("sending it", pcm(20)),
		stopChunk(),
		finalTranscript("what time is it"),
		transcriptChunk("noon", pcm(12)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sure, ", "audio:10",
		"user:please arrange a wire transfer",
		"interrupt", "text:I can't help with transfers.",
		"end:I can't help with transfers.|safety",
		"user:what time is it",
		"text:noon", "audio:12",
		"end:noon|stop",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())

	var blocked *types.Message
	for _, e := range elems {
		if e.EndOfStream && e.Message != nil && e.Message.FinishReason == types.FinishReasonSafety {
			blocked = e.Message
		}
	}
	require.NotNil(t, blocked)
	require.Len(t, blocked.Validations, 1)
	assert.Equal(t, "test_input", blocked.Validations[0].ValidatorType)
	assert.Equal(t, hooks.DirectionInput, blocked.Validations[0].Details["direction"])
	got := getEvents()
	require.Len(t, got, 1)
	assert.Equal(t, hooks.DirectionInput, got[0].Direction)
}

// When the caller's transcript is materialized as the reply begins (a provider
// with no final marker, as Gemini Live), the reply is blocked from its first
// chunk: none of it reaches the caller.
func TestDuplexGuardrailCut_SpokenInputBlockedAtReplyStart(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))

	elems, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "a wire transfer please",
		}},
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"user:a wire transfer please",
		"interrupt", "text:I can't help with transfers.",
		"end:I can't help with transfers.|safety",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
}

// A transcript that arrives after the reply to it completed can only stop
// what still plays and cut the provider's copy; the completed reply stays as
// it was emitted, and the firing is reported.
func TestDuplexGuardrailCut_LateSpokenInputBlockInterruptsOnly(t *testing.T) {
	s, rec, getEvents := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	sess := &interruptingSession{recordingSession: rec}
	s.session = sess

	elems, err := feedChunks(t, s,
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
		finalTranscript("a wire transfer please"),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sure", "audio:10", "end:Sure|stop",
		"user:a wire transfer please",
		"interrupt",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
	got := getEvents()
	require.Len(t, got, 1)
	assert.Equal(t, hooks.DirectionInput, got[0].Direction)
}

// On the scenario path the caller's transcript arrives with the reply's end:
// it is checked there, and a block turns the reply into the blocked message.
func TestDuplexGuardrailCut_ScenarioTranscriptBlockedAtTurnEnd(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	turnID := "t1"
	user := StreamElement{Message: &types.Message{Role: roleUser}}
	user.Meta.TurnID = &turnID
	require.False(t, s.handleInputElement(context.Background(), &user, make(chan StreamElement, 4), make(chan error, 1)))

	elems, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "a wire transfer",
		}},
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"text:Sure", "audio:10", "interrupt", "text:I can't help with transfers.",
		"end:I can't help with transfers.|safety"},
		trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
}

// A reply already cut for its output is cut once: the caller's transcript
// blocking it too gives the turn the input block's message, with no second
// interruption.
func TestDuplexGuardrailCut_BlockedReplyIsCutOnce(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(bannedWordsHook(t, "forbidden")),
		hooks.WithProviderHook(&wordInputHook{word: "wire"}),
	))

	elems, err := feedChunks(t, s,
		transcriptChunk("forbidden", pcm(10)),
		finalTranscript("a wire transfer"),
		transcriptChunk("more", pcm(20)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"interrupt", "text:I can't say that.",
		"user:a wire transfer",
		"text:I can't help with transfers.",
		"end:I can't help with transfers.|safety",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
}

// genChunk is c tagged with the provider generation it belongs to.
func genChunk(g int64, c providers.StreamChunk) providers.StreamChunk {
	c.Generation = g
	return c
}

// inputBannedWords is the built-in banned_words guardrail on the input side.
func inputBannedWords(t *testing.T, word string) hooks.ProviderHook {
	t.Helper()
	guard, err := guardrails.NewGuardrailHook("banned_words",
		map[string]any{"words": []any{word}, "direction": "input"},
		guardrails.WithMessage("I can't help with transfers."))
	require.NoError(t, err)
	return guard
}

// The provider is asked to stop the blocked reply by its generation: the
// reply streaming when an output guardrail fires, and, for a spoken turn
// blocked before its reply began, the reply after the last one seen. A later
// reply is never named.
func TestDuplexGuardrailCut_InterruptNamesTheBlockedGeneration(t *testing.T) {
	t.Run("streaming block", func(t *testing.T) {
		s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
		_, err := feedChunks(t, s,
			genChunk(1, transcriptChunk("fine", nil)), genChunk(1, stopChunk()),
			genChunk(2, transcriptChunk("forbidden", nil)), genChunk(2, stopChunk()))
		require.NoError(t, err)
		assert.Equal(t, []int64{2}, sess.interruptedGens())
	})
	t.Run("spoken turn blocked before its reply", func(t *testing.T) {
		s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))
		_, err := feedChunks(t, s,
			finalTranscript("hello"), genChunk(1, transcriptChunk("hi", nil)), genChunk(1, stopChunk()),
			finalTranscript("a wire transfer"),
			genChunk(2, transcriptChunk("Sure", nil)), genChunk(2, stopChunk()))
		require.NoError(t, err)
		assert.Equal(t, []int64{2}, sess.interruptedGens())
	})
	t.Run("spoken turn blocked mid-reply", func(t *testing.T) {
		s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))
		_, err := feedChunks(t, s,
			genChunk(5, transcriptChunk("Sure, ", nil)),
			genChunk(4, finalTranscript("a wire transfer")), // tagged with an older response's number
			genChunk(5, stopChunk()))
		require.NoError(t, err)
		assert.Equal(t, []int64{5}, sess.interruptedGens())
	})
	t.Run("completed-turn block", func(t *testing.T) {
		s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&enforceAfterCallHook{replacement: "No."})))
		_, err := feedChunks(t, s, genChunk(4, transcriptChunk("plan", nil)), genChunk(4, stopChunk()))
		require.NoError(t, err)
		assert.Equal(t, []int64{4}, sess.interruptedGens())
	})
}

// A transcript that arrives after its reply completed is judged as the
// input it is: the built-in input guardrail sees that user message, though
// the reply follows it in the history, and its block cuts what still plays
// of that reply (by its generation).
func TestDuplexGuardrailCut_LateTranscriptRunsBuiltInInputGuardrail(t *testing.T) {
	s, rec, getEvents := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))
	sess := &interruptingSession{recordingSession: rec}
	s.session = sess

	elems, err := feedChunks(t, s,
		genChunk(3, transcriptChunk("Sure", pcm(10))), genChunk(3, stopChunk()),
		finalTranscript("a wire transfer please"))
	require.NoError(t, err)

	assert.Equal(t, []string{"text:Sure", "audio:10", "end:Sure|stop", "user:a wire transfer please", "interrupt"},
		trace(elems))
	assert.Equal(t, []int64{3}, sess.interruptedGens())
	got := getEvents()
	require.Len(t, got, 1)
	assert.Equal(t, hooks.DirectionInput, got[0].Direction)
}

// A blocked reply that a barge-in then cuts short (Gemini's interrupted) is
// still the blocked turn: finish reason safety, its blocked text, no tool
// calls.
func TestDuplexGuardrailCut_BlockedReplyBargedInStaysSafety(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))

	elems, err := feedChunks(t, s,
		transcriptChunk("this is forbidden", pcm(10)),
		providers.StreamChunk{Interrupted: true},
	)
	require.NoError(t, err)

	var partial StreamElement
	for _, e := range elems {
		if e.Meta.Interrupted && e.Message != nil {
			partial = e
		}
	}
	require.NotNil(t, partial.Message, "the cut-short reply is kept")
	assert.Equal(t, "I can't say that.", partial.Message.Content)
	assert.Equal(t, types.FinishReasonSafety, partial.Message.FinishReason)
	assert.Equal(t, types.FinishReasonSafety, partial.Message.Meta[finishReasonMetaKey])
	require.NotNil(t, partial.Meta.FinishReason)
	assert.Equal(t, types.FinishReasonSafety, *partial.Meta.FinishReason)
	assert.Equal(t, []string{"banned_words"}, validationTypes(partial.Message.Validations))
}

// A transcript that arrives during a tool turn's round (after the reply that
// made the calls, before the reply to their results) blocks the rest of the
// turn: the follow-up reply is the blocked message, with one interruption,
// marked safety.
func TestDuplexGuardrailCut_LateBlockOnOwedToolTurnBlocksTheFollowUp(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))
	toolUse := "tool_use"
	calls := genChunk(1, providers.StreamChunk{FinishReason: &toolUse,
		ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "lookup"}}})
	interrupted := types.FinishReasonInterrupted

	elems, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("Let me check.", pcm(10))), calls,
		finalTranscript("a wire transfer"),
		genChunk(2, transcriptChunk("Done, sending", pcm(20))),
		genChunk(2, providers.StreamChunk{FinishReason: &interrupted}),
	)
	require.NoError(t, err)

	var interrupts []string
	for _, e := range elems {
		if e.Interrupt {
			interrupts = append(interrupts, *e.Meta.FinishReason)
		}
	}
	assert.Equal(t, []string{types.FinishReasonSafety}, interrupts, "one interruption, the guardrail's")
	var last *types.Message
	for _, e := range elems {
		if e.EndOfStream && e.Message != nil {
			last = e.Message
		}
	}
	require.NotNil(t, last)
	assert.Equal(t, "I can't help with transfers.", last.Content)
	assert.Equal(t, types.FinishReasonSafety, last.FinishReason)
	assert.Equal(t, []int64{2}, sess.interruptedGens(), "the follow-up reply is the one stopped")
}

// An enforcing interceptor that truncates text the caller already has sends
// only what the caller does not have: the caller's text adds up to the
// truncated turn.
func TestDuplexGuardrailCut_TruncationSendsOnlyTheRest(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 20})
	require.NoError(t, err)
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(guard)))

	elems, err := feedChunks(t, s,
		transcriptChunk("Hello there ", nil),
		transcriptChunk("my friend, how are you", nil),
		stopChunk(),
	)
	require.NoError(t, err)

	var caller strings.Builder
	for _, e := range elems {
		if e.Text != nil && e.Message == nil {
			caller.WriteString(*e.Text)
		}
	}
	msg, _ := turnMessage(t, elems)
	assert.Len(t, msg.Content, 20)
	assert.Equal(t, msg.Content, caller.String(), "the caller's text is the turn's, sent once")
}
