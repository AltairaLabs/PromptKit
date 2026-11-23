package duplex

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/history"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A turn is cut once and its guardrail message spoken once, however many
// guardrails fire on it. On OpenAI Realtime an output guardrail usually cuts
// the reply on its streamed text before the caller's transcript arrives; an
// input guardrail that then blocks the same turn only records its firing and
// has the turn forgotten: it neither cuts the turn again (that would flush the
// message's clip as it plays) nor shows and speaks its message a second time.

// onceStage is a forgetting duplex stage, wired to a cue stage on a spoken
// session, with an output and an input guardrail banning "Bruno" that both
// say brunoText.
func onceStage(t *testing.T) (*DuplexProviderStage, *forgettingSession) {
	t.Helper()
	s, a, _ := cueWiredStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(brunoGuard(t, "output")), hooks.WithProviderHook(brunoGuard(t, "input")),
	), true)
	f := &forgettingSession{interruptingSession: a}
	s.session = f
	return s, f
}

// The caller's transcript arrives while the reply an output guardrail cut is
// still ending: one cut, one message shown and spoken, both firings on the
// turn, and the caller's turn forgotten.
func TestDuplexGuardrailOnce_InputBlockWhileTheCutReplyEnds(t *testing.T) {
	s, sess := onceStage(t)

	elems, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("Sure! ", pcm(10))),
		genChunk(1, transcriptChunk("Bruno is", pcm(20))),
		spoken("Tell me about Bruno.", "item_A"),
		genChunk(1, stopChunk()),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sure! ", "audio:10",
		"interrupt", "text:" + brunoText, "cue:guardrail_message:" + brunoText,
		"user:Tell me about Bruno.",
		"end:" + brunoText + "|safety",
	}, cueTrace(elems))
	assert.Equal(t, []int64{1}, sess.interruptedGens(), "the reply is stopped once")
	assert.Equal(t, []string{"item_A"}, sess.forgotten(), "the blocked input is still forgotten")
	assert.Equal(t, 2, failedValidations(t, elems), "both guardrails' firings are on the turn")
	msg, _ := turnMessage(t, elems)
	assert.True(t, history.IsMarkedBlockedInput(msg), "the turn is still marked as a blocked input's")
}

// The caller's transcript arrives after the reply an output guardrail cut has
// ended (OpenAI's transcript of a short utterance comes after the reply): the
// turn is not cut again, so the message's clip plays on; the input is
// forgotten.
func TestDuplexGuardrailOnce_LateInputBlockDoesNotCutTheMessage(t *testing.T) {
	s, sess := onceStage(t)

	elems, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("Bruno", pcm(10))),
		genChunk(1, stopChunk()),
		spoken("Tell me about Bruno.", "item_A"),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"interrupt", "text:" + brunoText, "cue:guardrail_message:" + brunoText,
		"end:" + brunoText + "|safety",
		"user:Tell me about Bruno.",
	}, cueTrace(elems))
	assert.Equal(t, []int64{1}, sess.interruptedGens(), "the reply is stopped once")
	assert.Equal(t, []string{"item_A"}, sess.forgotten(), "the blocked input is still forgotten")
}

// Two of the caller's utterances blocked while one reply streams: the reply is
// cut once and the message shown once; both inputs are forgotten.
func TestDuplexGuardrailOnce_TwoInputBlocksOnOneReply(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))

	elems, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("Sure", pcm(10))),
		spoken("a wire transfer", "item_A"),
		spoken("I said a wire transfer", "item_B"),
		genChunk(1, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sure", "audio:10",
		"user:a wire transfer",
		"interrupt", "text:I can't help with transfers.",
		"user:I said a wire transfer",
		"end:I can't help with transfers.|safety",
	}, trace(elems))
	assert.Equal(t, []int64{1}, sess.interruptedGens())
	assert.Equal(t, []string{"item_A", "item_B"}, sess.forgotten())
}

// A reply the caller barged in on stopped playing: a late input block on its
// turn does not cut again (what plays now is the caller's next reply, or
// nothing), and the input is forgotten.
func TestDuplexGuardrailOnce_LateInputBlockAfterABargeIn(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))
	canceled := finishReasonInterrupted

	elems, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("Sure", pcm(10))),
		providers.StreamChunk{Generation: 1, Interrupted: true},
		providers.StreamChunk{Generation: 1, FinishReason: &canceled},
		spoken("a wire transfer", "item_A"))
	require.NoError(t, err)

	tr := trace(elems)
	require.NotEmpty(t, tr)
	assert.Equal(t, "user:a wire transfer", tr[len(tr)-1], "nothing is cut after the caller's turn: %v", tr)
	assert.Empty(t, sess.interruptedGens())
	assert.Equal(t, []string{"item_A"}, sess.forgotten())
}

// A reply an output guardrail blocked only once it completed is cut then,
// with its message; a late input block on its turn does not cut again.
func TestDuplexGuardrailOnce_LateInputBlockAfterACompletedTurnBlock(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(&enforceAfterCallHook{replacement: "I can't say that."}),
		hooks.WithProviderHook(inputBannedWords(t, "wire"))))

	elems, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("the secret plan", pcm(10))),
		genChunk(1, stopChunk()),
		spoken("a wire transfer", "item_A"))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:the secret plan", "audio:10",
		"interrupt", "text:I can't say that.",
		"end:I can't say that.|safety",
		"user:a wire transfer",
	}, trace(elems))
	assert.Equal(t, []int64{1}, sess.interruptedGens())
	assert.Equal(t, []string{"item_A"}, sess.forgotten())
}

// Each call's cost is reported once, on its completion or, for a call that
// completed before its cost arrived, as late usage, through every way a
// guardrail cuts a turn (OpenAI Realtime's order: cut on the streamed text,
// the canceled reply's end with its usage, then the caller's transcript).
func TestDuplexGuardrailOnce_EachCallIsCostedOnce(t *testing.T) {
	emitter, ev := newCallEvents()
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	reg := hooks.NewRegistry(hooks.WithProviderHook(brunoGuard(t, "output")), hooks.WithProviderHook(brunoGuard(t, "input")))
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec), emitter: emitter, hookReg: reg})
	canceled, stop := finishReasonInterrupted, "stop"
	seq := []providers.StreamChunk{
		// Cut on its text; the caller's transcript, blocked too, comes after
		// the canceled reply's end.
		genChunk(1, transcriptChunk("Sure ", pcm(10))), genChunk(1, transcriptChunk("Bruno", pcm(10))),
		{Generation: 1, FinishReason: &canceled, CostInfo: usd(100, 10)},
		spoken("Tell me about Bruno.", "item_A"),
		// Cut on its text; the transcript, blocked too, comes before its end.
		genChunk(2, transcriptChunk("Bruno", pcm(10))),
		spoken("Tell me about Bruno.", "item_B"),
		{Generation: 2, FinishReason: &canceled, CostInfo: usd(200, 20)},
		// Blocked before its reply, which ends canceled with nothing said.
		spoken("Bruno again", "item_C"),
		{Generation: 3, FinishReason: &canceled, CostInfo: usd(300, 30)},
		// An allowed turn whose transcript comes late.
		genChunk(4, transcriptChunk("Hello there", pcm(10))),
		{Generation: 4, FinishReason: &stop, CostInfo: usd(400, 40)},
		spoken("Twice? Why not?", "item_D"),
	}
	for i := range seq {
		h.sess().EmitChunk(&seq[i])
		h.settle()
	}
	done, _ := ev.await(t, 4, 0)
	h.finish()

	require.Len(t, done, 4, "one completion per call")
	for i, want := range [][2]int{{100, 10}, {200, 20}, {300, 30}, {400, 40}} {
		assertCall(t, done[i], want[0], want[1])
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	assert.Empty(t, ev.usage, "no call's cost is reported again")
}

// A late input block on a reply no guardrail cut still cuts what plays of it:
// only a turn already cut is not cut again.
func TestDuplexGuardrailOnce_LateInputBlockStillCutsAnUncutReply(t *testing.T) {
	s, sess := onceStage(t)

	elems, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("Sure, here goes", pcm(10))),
		genChunk(1, stopChunk()),
		spoken("Tell me about Bruno.", "item_A"),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sure, here goes", "audio:10",
		"end:Sure, here goes|stop",
		"user:Tell me about Bruno.",
		"interrupt",
	}, cueTrace(elems))
	assert.Equal(t, []int64{1}, sess.interruptedGens())
	assert.Equal(t, []string{"item_A"}, sess.forgotten())
}
