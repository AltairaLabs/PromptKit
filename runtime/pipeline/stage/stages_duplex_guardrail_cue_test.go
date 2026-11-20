package stage

import (
	"context"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// completionRecorder is an output hook that judges only completed turns,
// recording the text of each it is given.
type completionRecorder struct {
	mu   sync.Mutex
	seen []string
}

func (*completionRecorder) Name() string { return "completion_recorder" }

func (*completionRecorder) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (r *completionRecorder) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, resp.Message.Content)
	return hooks.Allow
}

func (r *completionRecorder) judged() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

// A guardrail that truncates the model's very first chunk leaves the model's
// own text, not a guardrail's: the completed turn is still judged by the
// output guardrails, and the kept text is shown, not spoken.
func TestDuplexGuardrailCue_TruncatedFirstChunkIsJudgedAndNotSpoken(t *testing.T) {
	limit, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 10})
	require.NoError(t, err)
	judge := &completionRecorder{}
	s, _, _ := cueWiredStage(t, hooks.NewRegistry(hooks.WithProviderHook(limit), hooks.WithProviderHook(judge)), true)

	elems, err := feedChunks(t, s, transcriptChunk("secret plans are here", pcm(20)), stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{"secret pla"}, judge.judged(), "the completed turn is judged")
	for _, e := range elems {
		assert.Nil(t, e.Cue, "the model's own text is not spoken as a guardrail message")
	}
	assert.Contains(t, cueTrace(elems), "text:secret pla", "the kept text is shown")
}

// A guardrail's message is spoken by the cue stage, not by the model: the
// provider stage puts a cue on the element that carries the message's text,
// and the cue stage after it plays the clip. The model is asked for nothing.

// cueWiredStage is cutStage with a cue stage wired after it (NewDuplexCueStage),
// told the session's output: spoken in voice at 24 kHz when spoken is set,
// else not spoken.
func cueWiredStage(t *testing.T, reg *hooks.Registry, spoken bool,
) (*DuplexProviderStage, *interruptingSession, *cueStage) {
	t.Helper()
	s, sess := cutStage(t, reg)
	cue := newCueStageFor(s, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)})
	out := providersmock.NewMockStreamSession()
	if spoken {
		out = spokenSession("alloy", cueTestRate)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.cues.sessionStarted(ctx, out, nil, nil)
	return s, sess, cue
}

// sentTexts is the text the session was sent.
func sentTexts(r *recordingSession) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.texts...)
}

// cueTrace is trace with each cue shown after its element as
// "cue:<kind>:<text>" and each Interrupted end shown as "interrupted".
func cueTrace(elems []StreamElement) []string {
	var out []string
	for _, e := range elems {
		out = append(out, trace([]StreamElement{e})...)
		if e.Cue != nil {
			out = append(out, "cue:"+string(e.Cue.Kind)+":"+e.Cue.Text)
		}
		if e.Meta.Interrupted {
			out = append(out, "interrupted")
		}
	}
	return out
}

// A guardrail that cuts a spoken reply has its message spoken by the cue
// stage: after the Interrupt, the message's text element carries the cue, and
// the blocked reply's end follows, not marked interrupted (a second cut would
// drop the cue's audio). The session is only asked to stop the reply.
func TestDuplexGuardrailCue_CutReplyCarriesTheCue(t *testing.T) {
	s, sess, _ := cueWiredStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))), true)

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
		"interrupt", "text:I can't say that.", "cue:guardrail_message:I can't say that.",
		"end:I can't say that.|safety",
		"text:all fine", "audio:12",
		"end:all fine|stop",
	}, cueTrace(elems))
	assert.Equal(t, 1, sess.interruptCount())
	assert.Empty(t, sentTexts(sess.recordingSession), "the model is sent nothing to say")
}

// Without a cue stage after the provider stage (a hand-built chain that has
// not added one) the message is shown, with no cue: nothing would play it.
func TestDuplexGuardrailCue_NoCueStageShowsTheTextOnly(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))

	elems, err := feedChunks(t, s, transcriptChunk("forbidden", pcm(20)), stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{"interrupt", "text:I can't say that.", "end:I can't say that.|safety"},
		cueTrace(elems))
}

// A session whose replies are not spoken gets the text with no cue.
func TestDuplexGuardrailCue_UnspokenSessionShowsTheTextOnly(t *testing.T) {
	s, _, _ := cueWiredStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))), false)

	elems, err := feedChunks(t, s, transcriptChunk("forbidden", nil), stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{"interrupt", "text:I can't say that.", "end:I can't say that.|safety"},
		cueTrace(elems))
}

// A guardrail that truncates a reply part-way leaves the caller with what they
// heard: the tail it keeps is shown, not spoken.
func TestDuplexGuardrailCue_TruncationIsNotSpoken(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 16})
	require.NoError(t, err)
	s, _, _ := cueWiredStage(t, hooks.NewRegistry(hooks.WithProviderHook(guard)), true)

	elems, err := feedChunks(t, s,
		transcriptChunk("short start ", pcm(20)), transcriptChunk("then far too long", pcm(30)), stopChunk(),
	)
	require.NoError(t, err)

	for _, e := range elems {
		assert.Nil(t, e.Cue, "a truncation's tail is not spoken")
	}
	assert.Contains(t, cueTrace(elems), "interrupt", "the reply is still cut")
}

// The provider that cannot stop a blocked reply (Gemini Live) keeps
// generating it; its chunks are dropped, and its end is not marked
// interrupted, so it does not cut the cue's clip.
func TestDuplexGuardrailCue_UnstoppableReplyEndsWithoutAnotherCut(t *testing.T) {
	s, rec, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
	newCueStageFor(s, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.cues.sessionStarted(ctx, spokenSession("Puck", cueTestRate), nil, nil)
	s.session = rec // no ResponseInterrupter

	elems, err := feedChunks(t, s,
		transcriptChunk("forbidden ", pcm(20)),
		transcriptChunk("still going", pcm(30)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"interrupt", "text:I can't say that.", "cue:guardrail_message:I can't say that.",
		"end:I can't say that.|safety",
	}, cueTrace(elems))
}

// blockedTypedTurn is a typed user turn an input guardrail blocked with text.
func blockedTypedTurn(text string) *duplexBlockedTurn {
	msg := types.Message{Role: roleAssistant, Content: text, FinishReason: types.FinishReasonSafety}
	return &duplexBlockedTurn{
		msg:      msg,
		decision: hooks.Decision{Allow: false, Reason: "banned", Enforced: true},
		user:     &types.Message{Role: roleUser, Content: "tell me about Bruno"},
	}
}

// emitBlocked emits turn through s and returns what came out.
func emitBlocked(t *testing.T, s *DuplexProviderStage, turn *duplexBlockedTurn) []StreamElement {
	t.Helper()
	out := make(chan StreamElement, 16)
	require.NoError(t, s.emitBlockedTurn(context.Background(), turn, out))
	close(out)
	var elems []StreamElement
	for e := range out {
		elems = append(elems, e)
	}
	return elems
}

// A typed turn an input guardrail blocks is spoken in a spoken session: the
// caller's turn, then the message's text carrying the cue, then its end. No
// reply was playing, so there is no Interrupt.
func TestDuplexGuardrailCue_BlockedTypedTurnCarriesTheCue(t *testing.T) {
	s, sess, _ := cueWiredStage(t, hooks.NewRegistry(), true)

	elems := emitBlocked(t, s, blockedTypedTurn(brunoText))

	assert.Equal(t, []string{
		"user:tell me about Bruno",
		"text:" + brunoText, "cue:guardrail_message:" + brunoText,
		"end:" + brunoText + "|safety",
	}, cueTrace(elems))
	assert.Equal(t, 0, sess.interruptCount())
	assert.Empty(t, sentTexts(sess.recordingSession), "the model is sent nothing")
}

// A blocked typed turn in a session whose replies are not spoken, or with no
// cue stage, is shown only.
func TestDuplexGuardrailCue_BlockedTypedTurnTextOnly(t *testing.T) {
	silent, _, _ := cueWiredStage(t, hooks.NewRegistry(), false)
	plain, _ := cutStage(t, hooks.NewRegistry())

	for name, s := range map[string]*DuplexProviderStage{"unspoken": silent, "no cue stage": plain} {
		elems := emitBlocked(t, s, blockedTypedTurn(brunoText))
		assert.Equal(t, []string{
			"user:tell me about Bruno", "text:" + brunoText, "end:" + brunoText + "|safety",
		}, cueTrace(elems), name)
	}
}

// The cut's elements through the cue stage: the clip, rendered at the
// session's start, plays right after the message's text and before the
// blocked reply's end, and is the earcon when no TTS service is configured.
func TestDuplexGuardrailCue_CueStagePlaysTheEarconAfterTheCut(t *testing.T) {
	s, _, cue := cueWiredStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))), true)
	<-cue.bank.render(context.Background(), "I can't say that.", nil).done // rendered at the session's start
	elems, err := feedChunks(t, s, transcriptChunk("forbidden", pcm(20)), stopChunk())
	require.NoError(t, err)

	c := startChain(t, cue)
	for _, e := range elems {
		c.in <- e
	}
	got := c.finish(t)

	var order []string
	for _, e := range got {
		switch {
		case e.Interrupt:
			order = append(order, "interrupt")
		case e.Meta.CueAudio != nil:
			if len(order) == 0 || order[len(order)-1] != "cue-audio" {
				order = append(order, "cue-audio")
			}
		case e.Text != nil:
			order = append(order, "text")
		case e.EndOfStream:
			order = append(order, "end")
		}
	}
	assert.Equal(t, []string{"interrupt", "text", "cue-audio", "end"}, order)
	assert.Equal(t, earconPCM(cueTestRate), cueAudioOf(got))
}

// brunoGuard is banned_words on "Bruno" in direction, whose message names
// what it bans.
func brunoGuard(t *testing.T, direction string) hooks.ProviderHook {
	t.Helper()
	guard, err := guardrails.NewGuardrailHook("banned_words",
		map[string]any{"words": []any{"Bruno"}, "direction": direction}, guardrails.WithMessage(brunoText))
	require.NoError(t, err)
	return guard
}

// failedValidations counts the failed validations on the turn's Message.
func failedValidations(t *testing.T, elems []StreamElement) int {
	t.Helper()
	msg, _ := turnMessage(t, elems)
	n := 0
	for _, v := range msg.Validations {
		if !v.Passed {
			n++
		}
	}
	return n
}

// A message that names what its guardrail bans is the guardrail's text, not
// the model's: the completed turn is not judged again, so the message is shown
// and spoken once and the guardrail fires once.
func TestDuplexGuardrailCue_MessageNamingTheBannedWordIsNotJudgedAgain(t *testing.T) {
	s, sess, _ := cueWiredStage(t, hooks.NewRegistry(hooks.WithProviderHook(brunoGuard(t, "output"))), true)

	elems, err := feedChunks(t, s,
		transcriptChunk("Let me tell you about ", pcm(20)), transcriptChunk("Bruno", pcm(30)), stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Let me tell you about ", "audio:20",
		"interrupt", "text:" + brunoText, "cue:guardrail_message:" + brunoText,
		"end:" + brunoText + "|safety",
	}, cueTrace(elems))
	assert.Equal(t, 1, failedValidations(t, elems))
	assert.Equal(t, 1, sess.interruptCount())
}

// The same holds for spoken input an input guardrail blocks while an output
// guardrail bans the same word: the blocked turn's text is the input
// guardrail's message, which the output guardrail does not judge.
func TestDuplexGuardrailCue_SpokenInputBlockMessageIsNotJudgedAgain(t *testing.T) {
	s, sess, _ := cueWiredStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(brunoGuard(t, "input")), hooks.WithProviderHook(brunoGuard(t, "output")),
	), true)

	elems, err := feedChunks(t, s,
		spoken("tell me about Bruno", "item_A"),
		genChunk(1, transcriptChunk("Sure", pcm(10))), genChunk(1, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"user:tell me about Bruno",
		"interrupt", "text:" + brunoText, "cue:guardrail_message:" + brunoText,
		"end:" + brunoText + "|safety",
	}, cueTrace(elems))
	assert.Equal(t, 1, failedValidations(t, elems))
	assert.Equal(t, 1, sess.interruptCount())
}
