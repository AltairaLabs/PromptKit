package duplex

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/history"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emittedMessages returns the messages of the emitted elements.
func emittedMessages(elems []core.StreamElement) []types.Message {
	var msgs []types.Message
	for _, e := range elems {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	return msgs
}

// A spoken turn an input hook blocks is replaced by a canned reply marked as a
// blocked input turn, so a later text turn on the stored conversation leaves
// both out.
func TestDuplexHooks_SpokenBlockedInputIsMarked(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	elems, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "a wire transfer please",
		}},
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
	)
	require.NoError(t, err)

	msgs := emittedMessages(elems)
	require.Equal(t, []string{"user:a wire transfer please", "assistant:I can't help with transfers."},
		roleLines(msgs))
	assert.Equal(t, true, msgs[1].Meta[types.MetaBlockedInput])
	assert.Empty(t, history.ExcludeBlockedInputTurns(msgs))
}

// A typed duplex turn an input hook blocks, without naming a validator, is
// marked; the reply streaming ahead of it is not.
func TestDuplexHooks_TypedBlockedInputIsMarked(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{replacement: "Not here."}))
	s, sess, _ := duplexStageWithHooks(t, reg)
	s.turnIDQueue = []string{"t1"}
	s.accumulatedText.WriteString("half a reply")

	out := make(chan core.StreamElement, 16)
	loopDone := make(chan error, 1)
	go func() { loopDone <- s.forwardResponseElements(context.Background(), out) }()
	text := "blocked input"
	require.False(t, s.handleInputElement(context.Background(), &core.StreamElement{Text: &text}, out, make(chan error, 1)))
	sess.respCh <- stopChunk()
	close(sess.respCh)
	require.NoError(t, <-loopDone)
	close(out)

	var elems []core.StreamElement
	for e := range out {
		elems = append(elems, e)
	}
	msgs := emittedMessages(elems)
	require.Equal(t, []string{"assistant:half a reply", "user:blocked input", "assistant:Not here."},
		roleLines(msgs))
	assert.Nil(t, msgs[0].Meta[types.MetaBlockedInput])
	assert.Equal(t, true, msgs[2].Meta[types.MetaBlockedInput])
	assert.Equal(t, []string{"assistant:half a reply"}, roleLines(history.ExcludeBlockedInputTurns(msgs)))
}

// A spoken input blocked after its reply completed cannot replace the reply,
// which the caller already has. The blocked request is marked as answered by
// the reply before it, so model context leaves out both; the transcript keeps
// both.
func TestDuplexHooks_LateSpokenBlockedInputIsMarked(t *testing.T) {
	s, rec, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	s.session = &interruptingSession{recordingSession: rec}
	elems, err := feedChunks(t, s,
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
		finalTranscript("a wire transfer please"),
	)
	require.NoError(t, err)

	msgs := emittedMessages(elems)
	require.Equal(t, []string{"assistant:Sure", "user:a wire transfer please"}, roleLines(msgs))
	assert.Equal(t, true, msgs[1].Meta[types.MetaBlockedInput])
	assert.Equal(t, true, msgs[1].Meta[history.BlockedReplyBeforeMetaKey])
	assert.Nil(t, msgs[0].Meta[types.MetaBlockedInput], "the emitted reply is not written after the fact")
	assert.Empty(t, history.ExcludeBlockedInputTurns(msgs))
}

// With a TranscriptReorderStage the late blocked request is placed ahead of
// the reply it held; the reply is marked instead, and model context still
// leaves out both.
func TestTranscriptReorderStage_LateBlockedRequestMarksTheHeldReply(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan core.StreamElement, 8)
	out := make(chan core.StreamElement, 16)

	prevUser := userElem("hello")
	prevReply := asstEndElem("Hi!")
	reply := asstEndElem("Sure")
	late := lateUserElem("a wire transfer please")
	late.Message.Meta = map[string]interface{}{types.MetaBlockedInput: true, history.BlockedReplyBeforeMetaKey: true}
	original := reply.Message
	in <- prevUser
	in <- prevReply
	in <- reply
	in <- late
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))

	var msgs []types.Message
	for e := range out {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	require.Equal(t, []string{"user:hello", "assistant:Hi!", "user:a wire transfer please", "assistant:Sure"},
		roleLines(msgs))
	assert.Nil(t, msgs[2].Meta[history.BlockedReplyBeforeMetaKey], "the reply is no longer before it")
	assert.Equal(t, true, msgs[3].Meta[types.MetaBlockedInput])
	assert.Nil(t, msgs[1].Meta[types.MetaBlockedInput])
	assert.Nil(t, original.Meta[types.MetaBlockedInput], "the duplex stage's message is not written")
	assert.Equal(t, []string{"user:hello", "assistant:Hi!"}, roleLines(history.ExcludeBlockedInputTurns(msgs)))
}

// A late blocked request of a tool turn moves ahead of the held tool round;
// only replies are marked, never the tool call or its result.
func TestTranscriptReorderStage_LateBlockedRequestNeverMarksAToolRound(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan core.StreamElement, 8)
	out := make(chan core.StreamElement, 16)
	late := lateUserElem("a wire transfer please")
	late.Message.Meta = map[string]interface{}{types.MetaBlockedInput: true, history.BlockedReplyBeforeMetaKey: true}
	in <- toolCallEndElem("c1")
	in <- toolResultElem("c1")
	in <- late
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))

	var msgs []types.Message
	for e := range out {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	require.Len(t, msgs, 3)
	assert.Equal(t, core.RoleUser, msgs[0].Role)
	assert.Nil(t, msgs[1].Meta[types.MetaBlockedInput], "the tool call is not a reply")
	assert.Nil(t, msgs[2].Meta[types.MetaBlockedInput], "the tool result is not a reply")
}

// blockedLateUserElem is a late transcript an input guardrail blocked after
// its reply completed, as DuplexProviderStage marks it.
func blockedLateUserElem(content string) core.StreamElement {
	e := lateUserElem(content)
	e.Message.Meta = map[string]interface{}{types.MetaBlockedInput: true, history.BlockedReplyBeforeMetaKey: true}
	return e
}

// reorderMessages drives a reorder stage and returns the messages it emits.
func reorderMessages(r *reorderRun) []types.Message {
	close(r.in)
	var msgs []types.Message
	for e := range r.out {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	return msgs
}

// A blocked late transcript for a turn whose hold already timed out is sent
// right after that turn's reply, not dropped, so model context leaves out
// both. An allowed one is still dropped.
func TestTranscriptReorderStage_BlockedLateTranscriptAfterTimeoutFollowsItsReply(t *testing.T) {
	r := startReorder(NewTranscriptReorderStageWithTimeout("[none]", 20*time.Millisecond))
	r.in <- asstEndElem("A1")
	time.Sleep(80 * time.Millisecond) // the hold times out
	r.in <- blockedLateUserElem("a wire transfer please")
	r.in <- txtElem("A2")
	r.in <- userElem("q2")
	r.in <- asstEndElem("A2")
	msgs := reorderMessages(r)

	assert.Equal(t, []string{"user:[none]", "assistant:A1", "user:a wire transfer please", "user:q2", "assistant:A2"},
		roleLines(msgs), "the transcript keeps the request")
	assert.Equal(t, []string{"user:[none]", "user:q2", "assistant:A2"}, roleLines(history.ExcludeBlockedInputTurns(msgs)))
}

// Once a later turn's user message is out, a blocked late transcript can no
// longer be placed right after its reply, and is dropped.
func TestTranscriptReorderStage_BlockedLateTranscriptAfterNextTurnIsDropped(t *testing.T) {
	r := startReorder(NewTranscriptReorderStageWithTimeout("[none]", 20*time.Millisecond))
	r.in <- asstEndElem("A1")
	time.Sleep(80 * time.Millisecond) // the hold times out
	r.in <- userElem("q2")
	r.in <- blockedLateUserElem("a wire transfer please")
	r.in <- asstEndElem("A2")
	assert.Equal(t, []string{"user:[none]", "assistant:A1", "user:q2", "assistant:A2"}, roleLines(reorderMessages(r)))
}

// A late transcript that is not blocked moves ahead of its reply unmarked.
func TestTranscriptReorderStage_LateAllowedRequestLeavesReplyUnmarked(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan core.StreamElement, 4)
	out := make(chan core.StreamElement, 8)
	in <- asstEndElem("Sure")
	in <- lateUserElem("what time is it")
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))

	var msgs []types.Message
	for e := range out {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	require.Equal(t, []string{"user:what time is it", "assistant:Sure"}, roleLines(msgs))
	assert.Nil(t, msgs[1].Meta[types.MetaBlockedInput])
}

// A blocked spoken input is both forgotten by the session and kept out of
// model context: late (after its reply completed) and mid-reply.
func TestDuplexForget_BlockedSpokenInputIsForgottenAndMarked(t *testing.T) {
	t.Run("late", func(t *testing.T) {
		s, sess := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))
		elems, err := feedChunks(t, s,
			genChunk(3, transcriptChunk("Sure", pcm(10))), genChunk(3, stopChunk()),
			spoken("a wire transfer please", "item_A"))
		require.NoError(t, err)

		assert.Equal(t, []string{"item_A"}, sess.forgotten())
		msgs := emittedMessages(elems)
		require.Equal(t, []string{"assistant:Sure", "user:a wire transfer please"}, roleLines(msgs))
		assert.Equal(t, true, msgs[1].Meta[types.MetaBlockedInput])
		assert.Equal(t, true, msgs[1].Meta[history.BlockedReplyBeforeMetaKey])
		assert.Empty(t, history.ExcludeBlockedInputTurns(msgs))
	})
	t.Run("mid-reply", func(t *testing.T) {
		s, sess := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))
		elems, err := feedChunks(t, s,
			genChunk(5, transcriptChunk("Sure, ", pcm(10))),
			spoken("a wire transfer please", "item_A"),
			genChunk(5, transcriptChunk("sending it", pcm(20))),
			genChunk(5, stopChunk()))
		require.NoError(t, err)

		assert.Equal(t, []string{"item_A"}, sess.forgotten())
		msgs := emittedMessages(elems)
		require.NotEmpty(t, msgs)
		last := msgs[len(msgs)-1]
		assert.Equal(t, core.RoleAssistant, last.Role)
		assert.Equal(t, true, last.Meta[types.MetaBlockedInput])
		assert.Empty(t, history.ExcludeBlockedInputTurns(msgs))
	})
}

// A late transcript that is not blocked is not marked.
func TestDuplexHooks_LateSpokenAllowedInputIsNotMarked(t *testing.T) {
	s, rec, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	s.session = &interruptingSession{recordingSession: rec}
	elems, err := feedChunks(t, s,
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
		finalTranscript("what time is it"),
	)
	require.NoError(t, err)

	msgs := emittedMessages(elems)
	require.Equal(t, []string{"assistant:Sure", "user:what time is it"}, roleLines(msgs))
	assert.Nil(t, msgs[1].Meta[types.MetaBlockedInput])
	assert.Len(t, history.ExcludeBlockedInputTurns(msgs), 2)
}

// A spoken input block and an output block on the same turn: the reply's
// mark survives the second block.
func TestDuplexHooks_SpokenInputMarkSurvivesALaterOutputBlock(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(&wordInputHook{word: "wire"}),
		hooks.WithProviderHook(denyAllRepliesHook{}),
	))
	elems, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "a wire transfer please",
		}},
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
	)
	require.NoError(t, err)

	msgs := emittedMessages(elems)
	require.Len(t, msgs, 2)
	assert.Equal(t, types.FinishReasonSafety, msgs[1].FinishReason)
	assert.Equal(t, true, msgs[1].Meta[types.MetaBlockedInput])
	assert.Empty(t, history.ExcludeBlockedInputTurns(msgs))
}

// A second block on a turn adds its metadata to the first's rather than
// replacing it, so an input block's mark is kept.
func TestDuplexHooks_BlockTurnWithMergesMeta(t *testing.T) {
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry())
	first := hooks.DenyWithMetadata("no", map[string]any{"only": "c"})
	s.blockTurnWith(context.Background(), nil, &first, hooks.DirectionOutput)
	assert.Equal(t, map[string]interface{}{"only": "c"}, s.turnGuard.blockedMeta,
		"a first block's metadata is taken as is")

	s.turnGuard.blockedMeta = history.WithBlockedInputMark(map[string]interface{}{"first": "a"})
	second := hooks.DenyWithMetadata("no", map[string]any{"second": "b"})
	s.blockTurnWith(context.Background(), nil, &second, hooks.DirectionOutput)
	assert.Equal(t, map[string]interface{}{
		types.MetaBlockedInput: true, "first": "a", "second": "b",
	}, s.turnGuard.blockedMeta)
}

// denyAllRepliesHook is an output guardrail that denies every completed reply.
type denyAllRepliesHook struct{}

func (denyAllRepliesHook) Name() string { return "deny_all_replies" }

func (denyAllRepliesHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (denyAllRepliesHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.DenyWithMetadata("no replies", map[string]any{"rule": "replies"})
}

// A duplex reply an output hook denies is replaced, not marked: it stays in
// model context.
func TestDuplexHooks_OutputDenyIsNotMarked(t *testing.T) {
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(denyReplyHook{})))
	elems, err := feedChunks(t, s, transcriptChunk("deny-me", pcm(10)), stopChunk())
	require.NoError(t, err)

	msgs := emittedMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, types.FinishReasonSafety, msgs[0].FinishReason)
	assert.Nil(t, msgs[0].Meta[types.MetaBlockedInput])
	assert.Len(t, history.ExcludeBlockedInputTurns(msgs), 1)
}
