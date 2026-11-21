package stage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Where a hold could let unjudged audio or transcript through: a transcript
// that trails its audio past the reply's generationComplete, a chunk check
// that truncates a held reply, the caller speaking over a held reply, a
// reply with no transcript, and tool calls in a held reply.

// A session whose transcript trails its audio may deliver the end of a
// reply's transcript after generationComplete: the hold waits out that lag
// before judging, so the late words are judged, and a failure in them keeps
// the reply from the caller.
func TestReplyHold_GenerationCompleteWaitsOutTheTranscriptLag(t *testing.T) {
	msg := prompt.DefaultBlockedMessage
	for name, tc := range map[string]struct {
		failOn string
		want   []string
		heard  int
	}{
		"late words pass": {"", []string{"text:Sunny skies.", "audio:10", "text: Mild."}, 10},
		"late words fail": {"Bruno", []string{"interrupt", "text:" + msg}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			hook := &replyHoldHook{mode: hooks.StreamHoldTurn, failOn: tc.failOn}
			r, sess := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldTurn}, 20*time.Millisecond, hook)
			assert.Empty(t, r.feed(transcriptChunk("Sunny skies.", pcm(10))))
			r.handle(providers.StreamChunk{GenerationComplete: true})
			assert.Empty(t, r.drain(), "nothing goes before the transcript's lag has passed")
			late := " Mild."
			if tc.failOn != "" {
				late = " Bruno sings."
			}
			r.handle(saidChunk(late))
			r.settle()
			assert.Equal(t, tc.want, r.drain())
			assert.Equal(t, []string{"Sunny skies." + late}, hook.calls(), "the late words are judged")
			assert.Equal(t, tc.heard, sess.released)
		})
	}
}

// A truncating chunk check on a held reply cuts it where the caller stopped
// hearing it: the stored turn is the released transcript, with its audio,
// not the truncated text the caller never heard.
func TestReplyHold_TruncationKeepsWhatWasHeard(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldSegment}
	length, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 30})
	require.NoError(t, err)
	r, sess := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, hook, length)

	r.feed(spokenChunk(10))
	r.feed(saidChunk("Hello there."))
	assert.Equal(t, []string{"audio:10", "text:Hello there."}, r.feed(saidChunk(" My friend is")))
	r.feed(spokenChunk(20))
	assert.Equal(t, []string{"interrupt"}, r.feed(saidChunk(" called Bob.")),
		"the reply is cut, and nothing the caller did not hear is shown")
	trace := r.feed(stopChunk())
	assert.Equal(t, []string{"end:Hello there.|safety"}, trace)
	assert.Equal(t, 10, sess.released)
}

// A caller who starts speaking over a held reply's silence, after the reply
// finished generating and while its judge runs, starts a turn of their own:
// the session reports it (HeldAudioInterrupted), and the reply is not
// released. Its turn keeps only what was heard.
func TestReplyHold_CallerSpeakingOverTheHoldDropsTheReply(t *testing.T) {
	t.Run("turn", func(t *testing.T) {
		hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
		r, sess := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldTurn}, 0, hook)
		r.feed(transcriptChunk("Sunny skies.", pcm(10)))
		sess.interrupted = true
		// The caller is told to stop playing the reply, none of which plays,
		// and no turn of it is left.
		elems := r.feedElems(stopChunk())
		assert.Equal(t, []string{"interrupt"}, trace(elems))
		for _, e := range elems {
			assert.Nil(t, e.Message, "nothing was heard")
		}
		assert.Zero(t, sess.released)
		assert.False(t, sess.interrupted, "the session is told the held audio was dropped")
	})
	t.Run("segment", func(t *testing.T) {
		hook := &replyHoldHook{mode: hooks.StreamHoldSegment}
		r, sess := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, hook)
		r.feed(spokenChunk(10))
		r.feed(saidChunk("Sunny skies."))
		assert.Equal(t, []string{"audio:10", "text:Sunny skies."}, r.feed(saidChunk(" Rain later.")))
		r.feed(spokenChunk(20))
		sess.interrupted = true
		assert.Empty(t, r.feed(saidChunk(" Snow soon.")), "nothing more is released")
		assert.Equal(t, []string{"interrupt", "end:Sunny skies.|" + types.FinishReasonInterrupted}, r.feed(stopChunk()))
		assert.Equal(t, 10, sess.released)
	})
}

// A caller who speaks over a held reply of which only audio was released
// (the transcript completing it was within the holdback) heard that audio:
// the turn keeps it, with the reply's other marks.
func TestReplyHold_CallerSpeakingKeepsHeardAudioWithoutText(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldSegment}
	r, sess := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment, Holdback: 10 * time.Millisecond}, 0, hook)
	r.feed(spokenChunk(480))
	r.feed(spokenChunk(482))
	r.feed(saidChunk("Sunny skies."))
	assert.Equal(t, []string{"audio:480"}, r.feed(saidChunk(" Rain later.")))
	sess.interrupted = true
	elems := r.feedElems(stopChunk())
	m, e := turnMessage(t, elems)
	assert.Empty(t, m.Content)
	assert.Equal(t, pcm(480), partAudio(t, m))
	assert.Equal(t, types.FinishReasonInterrupted, m.FinishReason)
	assert.Equal(t, types.FinishReasonInterrupted, *e.Meta.FinishReason)
	assert.Equal(t, types.FinishReasonInterrupted, m.Meta[finishReasonMetaKey])
	assert.Contains(t, m.Meta, types.MetaCallerTurn, "the reply's other marks stay")
}

// A held reply whose audio came with no transcript cannot be judged: it fails
// closed rather than releasing audio no guardrail heard.
func TestReplyHold_AudioWithoutTranscriptFailsClosed(t *testing.T) {
	msg := prompt.DefaultBlockedMessage
	for name, mode := range map[string]RealtimeHold{"turn": RealtimeHoldTurn, "segment": RealtimeHoldSegment} {
		t.Run(name, func(t *testing.T) {
			hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
			if mode == RealtimeHoldSegment {
				hook.mode = hooks.StreamHoldSegment
			}
			r, sess := newReleased(t, StreamHoldConfig{Realtime: mode}, 0, hook)
			r.feed(spokenChunk(10))
			r.feed(spokenChunk(20))
			assert.Equal(t, []string{"interrupt", "text:" + msg, "end:" + msg + "|safety"}, r.feed(stopChunk()))
			assert.Zero(t, sess.released)
		})
	}
}

// A reply that calls tools is held like any other: its text and audio go
// when the completed reply passes, and a reply that fails keeps none of its
// calls. Its generationComplete does not release it: the turn's completion
// judges the calls too.
func TestReplyHold_ToolCallsInAHeldReply(t *testing.T) {
	call := types.MessageToolCall{ID: "c1", Name: "lookup", Args: []byte(`{}`)}
	t.Run("passes", func(t *testing.T) {
		hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
		r, sess := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldTurn}, 0, hook)
		r.feed(transcriptChunk("Let me check.", pcm(10)))
		assert.Empty(t, r.feed(providers.StreamChunk{ToolCalls: []types.MessageToolCall{call}}))
		assert.Empty(t, r.feed(providers.StreamChunk{GenerationComplete: true}), "the calls wait for the turn")
		elems := r.feedElems(toolStopChunk())
		assert.Equal(t, []string{"text:Let me check.", "audio:10", "end:Let me check.|tool_calls"}, trace(elems))
		m, _ := turnMessage(t, elems)
		assert.Equal(t, []types.MessageToolCall{call}, m.ToolCalls)
		assert.Equal(t, 10, sess.released)
	})
	t.Run("fails", func(t *testing.T) {
		hook := &replyHoldHook{mode: hooks.StreamHoldTurn, failOn: "Bruno"}
		r, sess := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldTurn}, 0, hook)
		r.feed(transcriptChunk("Asking Bruno.", pcm(10)))
		r.feed(providers.StreamChunk{ToolCalls: []types.MessageToolCall{call}})
		elems := r.feedElems(toolStopChunk())
		msg := prompt.DefaultBlockedMessage
		assert.Equal(t, []string{"interrupt", "text:" + msg, "end:" + msg + "|safety"}, trace(elems))
		m, _ := turnMessage(t, elems)
		assert.Empty(t, m.ToolCalls)
		assert.Zero(t, sess.released)
	})
}

func toolStopChunk() providers.StreamChunk {
	fr := "tool_calls"
	return providers.StreamChunk{FinishReason: &fr}
}

// On the scenario path the caller's transcript reaches the history only with
// the turn's completion: a reply whose generation completes before that is
// judged then, with the caller's turn in the history it is judged against.
func TestReplyHold_ScenarioPathJudgesWithTheCallersTurn(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
	r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldTurn}, 0, hook)
	r.s.turnIDQueue = []string{"t1"}
	r.s.history.add(&types.Message{Role: roleUser})
	r.feed(providers.StreamChunk{Metadata: map[string]interface{}{
		"type": "input_transcription", "transcription": "Is it sunny?",
	}})
	r.feed(transcriptChunk("Sunny skies.", pcm(10)))
	assert.Empty(t, r.feed(providers.StreamChunk{GenerationComplete: true}), "judged with the turn")
	assert.Empty(t, hook.calls())
	assert.Equal(t, []string{"text:Sunny skies.", "audio:10", "end:Sunny skies.|stop"}, r.feed(stopChunk()))
	assert.Equal(t, []string{"Sunny skies."}, hook.calls())
	assert.Equal(t, "Is it sunny?", hook.lastCallerSaid())
}
