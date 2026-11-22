package stage_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// lastMessage is the content of the last Message in elems.
func lastMessage(t *testing.T, elems []stage.StreamElement) *types.Message {
	t.Helper()
	for i := len(elems) - 1; i >= 0; i-- {
		if elems[i].Message != nil {
			return elems[i].Message
		}
	}
	t.Fatalf("no Message in %v", kinds(elems))
	return nil
}

// speakFirst sends deltas and reads on until n segments have been spoken.
func speakFirst(t *testing.T, r *replyRun, n int, deltas ...string) {
	t.Helper()
	for _, d := range deltas {
		r.send(t, delta(d))
	}
	for len(audioElements(r.got)) < n {
		r.next(t)
	}
}

// A guardrail that truncates the reply to text already spoken speaks nothing
// more, and the reply is stored as the truncation, once.
func TestTTSStageWithInterruption_TruncationToSpokenTextSpeaksNothingMore(t *testing.T) {
	svc := newSegmentRecordingTTS()
	r := startReplyRun(t, newStreamedReplyStage(svc))
	speakFirst(t, r, 2, "Alpha one. ", "Beta two. ", "Gamma three.")

	r.send(t, replacement("Alpha one. Beta two."), assistant("Alpha one. Beta two."), stage.NewEndOfTurnElement())
	got := r.finish(t)

	assert.Equal(t, []string{"Alpha one.", "Beta two."}, svc.spoken(), "nothing already spoken is spoken again")
	assert.Equal(t, "Alpha one. Beta two.", lastMessage(t, got).Content)
}

// A truncation that ends inside text already spoken stores the truncation and
// speaks nothing more.
func TestTTSStageWithInterruption_TruncationInsideSpokenTextStoresTheTruncation(t *testing.T) {
	svc := newSegmentRecordingTTS()
	r := startReplyRun(t, newStreamedReplyStage(svc))
	speakFirst(t, r, 2, "Alpha one. ", "Beta two. ", "Gamma three.")

	r.send(t, replacement("Alpha one."), assistant("Alpha one."), stage.NewEndOfTurnElement())
	got := r.finish(t)

	assert.Equal(t, []string{"Alpha one.", "Beta two."}, svc.spoken())
	assert.Equal(t, "Alpha one.", lastMessage(t, got).Content)
}

// A replacement that begins with what was spoken (a redactor rewriting the
// rest) speaks only what follows it, and stores the replacement once.
func TestTTSStageWithInterruption_ReplacementStartingWithSpokenTextSpeaksTheRest(t *testing.T) {
	svc := newSegmentRecordingTTS()
	r := startReplyRun(t, newStreamedReplyStage(svc))
	speakFirst(t, r, 1, "Hi there. ", "Call 555")

	r.send(t, replacement("Hi there.  Call [REDACTED]."), assistant("Hi there.  Call [REDACTED]."),
		stage.NewEndOfTurnElement())
	got := r.finish(t)

	assert.Equal(t, []string{"Hi there.", "Call [REDACTED]."}, svc.spoken())
	assert.Equal(t, "Hi there.  Call [REDACTED].", lastMessage(t, got).Content)
}

// A completed reply rewritten after it streamed (an AfterCall redactor, no
// replacement delta) speaks only what follows the text already spoken.
func TestTTSStageWithInterruption_RedactedMessageSpeaksOnlyWhatWasNotSpoken(t *testing.T) {
	svc := newSegmentRecordingTTS()
	r := startReplyRun(t, newStreamedReplyStage(svc))
	speakFirst(t, r, 1, "Hi there. ", "Call 555-1234 now.")

	r.send(t, assistant("Hi there. Call [REDACTED] now."), stage.NewEndOfTurnElement())
	got := r.finish(t)

	assert.Equal(t, []string{"Hi there.", "Call [REDACTED] now."}, svc.spoken())
	assert.Equal(t, "Hi there. Call [REDACTED] now.", lastMessage(t, got).Content)
}

// A reply the provider stage kept as interrupted (the caller barged in while
// it streamed) speaks nothing more, even text the turn's end would otherwise
// flush, and records none of what was not spoken.
func TestTTSStageWithInterruption_InterruptedMessageFlushesNothing(t *testing.T) {
	svc := newSegmentRecordingTTS()
	r := startReplyRun(t, newStreamedReplyStage(svc))
	speakFirst(t, r, 1, "One. ", "It ran to")

	m := assistant("One. It ran to")
	m.Message.FinishReason = types.FinishReasonInterrupted
	r.send(t, m, stage.NewEndOfTurnElement(), stage.NewInterruptElement())
	got := r.finish(t)

	assert.Equal(t, []string{"One."}, svc.spoken(), "the pending fragment is never spoken")
	msg := lastMessage(t, got)
	assert.Equal(t, types.FinishReasonInterrupted, msg.FinishReason)
	assert.True(t, strings.HasPrefix("One.", msg.Content), "at most what was spoken is recorded: %q", msg.Content)
	assert.Equal(t, types.PlayoutCutBargeIn, msg.Meta[types.MetaPlayoutCut])
}

// The cap holds for a long sentence that arrives in one delta: it is spoken
// in pieces of at most 250 characters, every character once, in order.
func TestTTSStageWithInterruption_CapsALongSentenceInOneDelta(t *testing.T) {
	svc := newSegmentRecordingTTS()
	long := "See " + strings.Repeat("é", 300) + " and " + strings.Repeat("word ", 60) + "ok. Next one."
	r := startReplyRun(t, newStreamedReplyStage(svc))
	r.send(t, delta(long), assistant(long), stage.NewEndOfTurnElement())
	r.finish(t)

	spoken := svc.spoken()
	require.Greater(t, len(spoken), 2)
	for _, seg := range spoken {
		assert.LessOrEqual(t, utf8.RuneCountInString(seg), 250, "segment %q exceeds the cap", seg)
	}
	assert.Equal(t, strings.Join(strings.Fields(long), ""), strings.Join(strings.Fields(strings.Join(spoken, "")), ""),
		"every character is spoken once, in order")
}
