package voice

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Unit tests of replySpeech, the streamed reply TTSStageWithInterruption
// speaks (#2876): what each released text, completed Message and cut leaves
// to speak, and what the caller heard.

func TestReplySpeech_ReleasedTextIsSpokenOnceItsMessageCompletes(t *testing.T) {
	var r replySpeech
	assert.Equal(t, []string{"Hello there."}, r.release("Hello there. How", false, false))
	assert.Empty(t, r.release(" are you?", false, false), "a sentence end with nothing after it waits")

	assert.Equal(t, []string{"How are you?"}, r.complete("Hello there. How are you?"))
	assert.Equal(t, "Hello there. How are you?", r.rt().sent)
	assert.Equal(t, "", r.rt().unmatched)
	assert.True(t, r.active)
}

func TestReplySpeech_FinalTextIsSpokenWhole(t *testing.T) {
	var r replySpeech
	assert.Equal(t, []string{"All done"}, r.release("All done", false, true))
}

func TestReplySpeech_AMessageThatGoesOnPastTheReleasedTextSpeaksTheRest(t *testing.T) {
	var r replySpeech
	assert.Equal(t, []string{"First part."}, r.release("First part. Sec", false, false))
	assert.Equal(t, []string{"Second part."}, r.complete("First part. Second part."))
	assert.Equal(t, "", r.rt().unmatched)
}

func TestReplySpeech_TextReleasedPastTheMessageWaitsForTheNextMessage(t *testing.T) {
	var r replySpeech
	assert.Equal(t, []string{"Round one."}, r.release("Round one. Round two", false, false))
	assert.Empty(t, r.complete("Round one."), "what follows belongs to a later Message")
	assert.Equal(t, " Round two", r.rt().unmatched)
	assert.Equal(t, []string{"Round two"}, r.complete("Round two"))
}

func TestReplySpeech_AReplacementThatBeginsWithWhatWasSpokenSpeaksOnlyTheRest(t *testing.T) {
	var r replySpeech
	assert.Equal(t, []string{"Sure thing."}, r.release("Sure thing. Pine", false, false))

	segs := r.release("Sure thing. Let's talk about something else.", true, false)
	assert.Equal(t, []string{"Let's talk about something else."}, segs, "the held \"Pine\" is dropped")
	assert.Equal(t, "Sure thing. Let's talk about something else.", r.rt().unmatched)
	assert.Equal(t, "Let's talk about something else.", r.rt().sent)
}

func TestReplySpeech_AReplacementThatEndsInsideWhatWasSpokenSpeaksNothingMore(t *testing.T) {
	var r replySpeech
	assert.Equal(t, []string{"Sure thing.", "Here it is."}, r.release("Sure thing. Here it is. And", false, false))

	assert.Empty(t, r.release("Sure thing.", true, false))
	assert.Equal(t, "Sure thing.", r.rt().unmatched)
	assert.Equal(t, "", r.rt().sent)
	assert.Equal(t, "", r.seg.Flush(), "the held \"And\" is dropped")
}

func TestReplySpeech_AReplacementOfWhatWasHeardIsHeardAfterIt(t *testing.T) {
	var r replySpeech
	assert.Equal(t, []string{"Pineapple is great."}, r.release("Pineapple is great. Also", false, false))
	r.rt().heard = []string{"Pineapple is great."}

	assert.Equal(t, []string{"Let's talk."}, r.release("Let's talk.", true, false))

	msg := &types.Message{Role: "assistant", Content: "  Let's talk.",
		Parts: []types.ContentPart{types.NewTextPart("Let's talk.")}}
	heard := r.heardWith(msg)
	require.NotSame(t, msg, heard, "the Message is copied")
	assert.Equal(t, "Pineapple is great. Let's talk.", heard.Content)
	assert.Nil(t, heard.Parts)
	assert.Equal(t, "  Let's talk.", msg.Content, "the original is unchanged")
	assert.Same(t, msg, r.heardWith(msg), "what was heard is recorded once")
}

func TestReplySpeech_ACompletedMessageThatReplacedTheStreamIsSpokenInItsPlace(t *testing.T) {
	var r replySpeech
	assert.Equal(t, []string{"Pineapple is great."}, r.release("Pineapple is great. Also", false, false))

	assert.Equal(t, []string{"Let's talk."}, r.complete("Let's talk."))
	assert.Equal(t, "", r.rt().heardBeforeCut, "nothing was heard before the cut")
	assert.Equal(t, "Let's talk.", r.rt().sent)
}

func TestReplySpeech_ACompletedMessageThatTruncatesWhatWasSpokenSpeaksNothingMore(t *testing.T) {
	var r replySpeech
	assert.Equal(t, []string{"One.", "Two."}, r.release("One. Two. Three", false, false))
	// Not a prefix of the released text (its spacing was rewritten), and it
	// ends inside what was spoken.
	assert.Empty(t, r.complete("One.\nTw"))
	assert.Equal(t, "", r.rt().unmatched, "a completed Message's text is not left to match")
	assert.Equal(t, "", r.rt().sent)
	assert.Equal(t, "", r.seg.Flush(), "the held \"Three\" is dropped")
}

func TestReplySpeech_AStoppedReplySpeaksNothingMore(t *testing.T) {
	var r replySpeech
	assert.Equal(t, []string{"One."}, r.release("One. Two", false, false))
	r.stop()
	assert.True(t, r.stopped)
	assert.Equal(t, "", r.rt().unmatched)
	assert.Empty(t, r.release(" more. Text. ", false, false))
	assert.Empty(t, r.complete("One. Two more."))
	assert.Empty(t, r.end())
	assert.Empty(t, r.peekRoundEnd("r2"))
}

func TestReplySpeech_ARoundsLastSegmentIsSpokenWhenTheNextRoundBegins(t *testing.T) {
	r := replySpeech{round: "r1"}
	assert.Equal(t, []string{"One."}, r.release("One. Two", false, false))

	assert.Empty(t, r.peekRoundEnd("r1"), "the same round")
	assert.Empty(t, r.peekRoundEnd(""), "an unnamed round")
	assert.Equal(t, []string{"Two"}, r.peekRoundEnd("r2"))
	assert.Equal(t, "One. Two", r.rounds["r1"].sent)

	unnamed := replySpeech{}
	unnamed.release("Held", false, false)
	assert.Empty(t, unnamed.peekRoundEnd("r2"), "no round is being spoken")
}

func TestReplySpeech_ARoundsTextEndsWithItsStream(t *testing.T) {
	r := replySpeech{round: "r1"}
	r.release("Partial", false, false)
	assert.Empty(t, r.roundTextEnd("r2"), "the segmenter holds r1's text")
	assert.Equal(t, []string{"Partial"}, r.roundTextEnd("r1"))
}

func TestReplySpeech_EachRoundSegmentsItsOwnText(t *testing.T) {
	r := replySpeech{round: "r1"}
	r.release("Alpha", false, false)
	r.round = "r2"
	assert.Equal(t, []string{"Beta."}, r.release("Beta. ", false, false), "r1's \"Alpha\" does not join r2's text")
	assert.Equal(t, "r1", r.segRound)

	// The turn ends with r1's text still held: it is r1's to speak.
	assert.Equal(t, []string{"Alpha"}, r.end())
	assert.Equal(t, "r1", r.round)
	assert.Equal(t, "Alpha", r.rounds["r1"].sent)
	assert.Equal(t, "Beta.", r.rounds["r2"].sent)
}

func TestReplySpeech_MessageDoneForgetsWhatTheMessageAccountedFor(t *testing.T) {
	var r replySpeech
	r.rt().sent = "One. Two."
	r.rt().heard = []string{"One."}
	r.playout.add("One.", "", true, time.Unix(0, 0), time.Second)
	r.playout.add("Two.", "", true, time.Unix(0, 0), time.Second)

	r.messageDone("One.", "")
	assert.Equal(t, "Two.", r.rt().sent)
	assert.Nil(t, r.rt().heard)
	require.Len(t, r.playout.segs, 1)
	assert.Equal(t, "Two.", r.playout.segs[0].text)

	r.messageDone("Something else", "")
	assert.Equal(t, "", r.rt().sent, "text that does not begin the sent text accounts for all of it")
}

// pricedService is a TTS service billed per character.
type pricedService struct{ *fakeTTS }

func (pricedService) Pricing() *base.PricingDescriptor {
	return &base.PricingDescriptor{Items: []base.PriceItem{{Unit: "character", Rate: 0.5}}}
}
func (pricedService) ImplName() string  { return "priced" }
func (pricedService) ModelName() string { return "" }

func TestReplySpeech_TakeCostPricesTheBilledTextOnce(t *testing.T) {
	var r replySpeech
	svc := pricedService{newFakeTTS(nil)}
	assert.Nil(t, r.takeCost(svc), "nothing billed")

	r.bill("ab", 100*time.Millisecond)
	r.bill("cd", 50*time.Millisecond)
	cost := r.takeCost(svc)
	require.NotNil(t, cost)
	assert.InDelta(t, 2.0, cost.TotalCost, 1e-9, "4 characters at 0.5")
	assert.Equal(t, 0, r.billed.Len())
	assert.Equal(t, time.Duration(0), r.latency)
	assert.Nil(t, r.takeCost(svc), "the billed text is priced once")
}

func TestReplySpeech_ResetKeepsWhenTheAudioEndsAndInterruptedDoesNot(t *testing.T) {
	until := time.Unix(100, 0)
	r := replySpeech{round: "r1", segRound: "r1", active: true, stopped: true, last: "x",
		err: assert.AnError, playout: replyPlayout{until: until, segs: []playedSegment{{text: "a"}}}}
	r.release("held", false, false)
	r.bill("abc", time.Second)

	r.reset()
	assert.Equal(t, replySpeech{playout: replyPlayout{until: until}}, r)

	r.playout.segs = []playedSegment{{text: "a"}}
	r.interrupted()
	assert.Equal(t, replyPlayout{}, r.playout)
}

func TestReplySpeech_InterruptedAtIsTheBargeInsTimeElseNow(t *testing.T) {
	now := time.Unix(50, 0)
	var r replySpeech
	assert.Equal(t, now, r.interruptedAt(now), "no barge-in clock")

	r.cutAt = func() time.Time { return time.Time{} }
	assert.Equal(t, now, r.interruptedAt(now), "no barge-in yet")

	at := time.Unix(40, 0)
	r.cutAt = func() time.Time { return at }
	assert.Equal(t, at, r.interruptedAt(now))
}

func TestReplySpeech_CutMessageRecordsWhatWasHeard(t *testing.T) {
	t0 := time.Unix(0, 0)
	r := replySpeech{round: "m1"}
	r.playout.add("One two.", "m1", true, t0, time.Second)
	r.playout.add("Three four.", "m1", true, t0, time.Second)
	msg := &types.Message{Role: "assistant", Content: "One two. Three four.", FinishReason: "stop",
		Parts: []types.ContentPart{types.NewTextPart("One two. Three four.")}}
	msg.Meta = map[string]interface{}{types.MetaMessageID: "m1"}

	cut := r.cutMessage(msg, t0.Add(1500*time.Millisecond), types.PlayoutCutBargeIn)
	assert.Equal(t, "One two. Three", cut.Content)
	assert.Nil(t, cut.Parts)
	assert.Equal(t, types.FinishReasonInterrupted, cut.FinishReason)
	assert.Equal(t, true, cut.Meta[types.MetaHeardPrefix])
	assert.Equal(t, int64(1500), cut.Meta[types.MetaHeardMs])
	assert.Equal(t, int64(2000), cut.Meta[types.MetaAudioMs])
	assert.Equal(t, 0.75, cut.Meta[types.MetaPlayedFraction])
	assert.Equal(t, "stop", msg.FinishReason, "the original is unchanged")
}

func TestReplySpeech_CutMessageHeardInFullKeepsItsTextAndATurnWithToolsItsFinishReason(t *testing.T) {
	t0 := time.Unix(0, 0)
	var r replySpeech
	r.playout.add("Let me look.", "", true, t0, time.Second)
	parts := []types.ContentPart{types.NewTextPart("Let me look.")}
	msg := &types.Message{Role: "assistant", Content: "Let me look. ", FinishReason: "tool_calls", Parts: parts,
		ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "get_weather"}}}

	cut := r.cutMessage(msg, t0.Add(2*time.Second), types.PlayoutCutBargeIn)
	assert.Equal(t, "Let me look. ", cut.Content, "all of it was heard: its own spacing is kept")
	assert.Equal(t, parts, cut.Parts)
	assert.Equal(t, "tool_calls", cut.FinishReason)
	assert.Equal(t, true, cut.Meta[types.MetaHeardPrefix])
}

func TestReplySpeech_CutMessageHeardNoneOfIsNotAHeardPrefix(t *testing.T) {
	t0 := time.Unix(0, 0)
	var r replySpeech
	r.playout.add("Not yet.", "", true, t0.Add(time.Second), time.Second)
	msg := &types.Message{Role: "assistant", Content: "Not yet.", FinishReason: "stop"}

	cut := r.cutMessage(msg, t0, types.PlayoutCutSessionClosed)
	assert.Equal(t, "", cut.Content)
	assert.NotContains(t, cut.Meta, types.MetaHeardPrefix)
	assert.Equal(t, "stop", cut.FinishReason, "a closed session leaves the reply its own finish reason")
}

func TestCutPrefix(t *testing.T) {
	for _, tc := range []struct {
		s, prefix, rest string
		ok              bool
	}{
		{"Hello world. More", "Hello world.", " More", true},
		{"Hello\n world.  More", "Hello world.", "  More", true},
		{"  Hi there", "Hi", " there", true},
		{"Hi", "", "Hi", true},
		{"Hi", "   ", "Hi", true},
		{"Hi", "Hi there", "", false},
		{"Hi   ", "Hi there", "", false},
		{"Hello", "Help", "", false},
		{"héllo wörld", "héllo", " wörld", true},
		// A prefix longer than s fails even when its next rune is the one
		// decoding past the end of s would yield.
		{"a", "a\uFFFD", "", false},
	} {
		rest, ok := cutPrefix(tc.s, tc.prefix)
		assert.Equal(t, tc.ok, ok, "cutPrefix(%q, %q)", tc.s, tc.prefix)
		assert.Equal(t, tc.rest, rest, "cutPrefix(%q, %q)", tc.s, tc.prefix)
	}
}

func TestJoinTextAndAppendNonEmpty(t *testing.T) {
	assert.Equal(t, "b", joinText("", "b"))
	assert.Equal(t, "a", joinText("a", ""))
	assert.Equal(t, "a b", joinText("a", "b"))
	assert.Equal(t, []string{"a"}, appendNonEmpty([]string{"a"}, ""))
	assert.Equal(t, []string{"a", "b"}, appendNonEmpty([]string{"a"}, "b"))
}

func TestLeadingWords(t *testing.T) {
	assert.Equal(t, "", leadingWords("one two three four", 0))
	assert.Equal(t, "one", leadingWords("one two three four", 0.49))
	assert.Equal(t, "one two", leadingWords("one two three four", 0.5))
	assert.Equal(t, "one two", leadingWords("one two three four", 0.74))
	assert.Equal(t, "one two three four", leadingWords("one  two three four", 1))
}

func TestReplyPlayout_QueuesSegmentsBehindTheAudioAhead(t *testing.T) {
	t0 := time.Unix(0, 0)
	var p replyPlayout
	assert.Equal(t, time.Time{}, p.lastStart(), "nothing spoken")

	p.add("One two.", "r1", true, t0, time.Second)
	p.add("Three four.", "r2", true, t0.Add(200*time.Millisecond), time.Second) // behind the first

	assert.Equal(t, t0.Add(2*time.Second), p.until)
	assert.Equal(t, t0.Add(time.Second), p.lastStart())
	assert.Equal(t, t0.Add(time.Second), p.endOf("r1"))
	assert.Equal(t, t0.Add(2*time.Second), p.endOf("r2"))
	assert.Equal(t, t0.Add(2*time.Second), p.endOf(""))
	assert.Equal(t, t0.Add(2*time.Second), p.endOf("r3"), "a round with no audio: the end of all of it")

	text, heard, total := p.heardAt(t0.Add(1500*time.Millisecond), "")
	assert.Equal(t, "One two. Three", text)
	assert.Equal(t, 1500*time.Millisecond, heard)
	assert.Equal(t, 2*time.Second, total)

	text, heard, total = p.heardAt(t0.Add(1500*time.Millisecond), "r2")
	assert.Equal(t, "Three", text, "only r2's segments count")
	assert.Equal(t, 500*time.Millisecond, heard)
	assert.Equal(t, time.Second, total)
}

func TestReplyPlayout_AStalledSegmentPlaysInStretches(t *testing.T) {
	t0 := time.Unix(0, 0)
	var p replyPlayout
	p.add("a b c d", "", true, t0, time.Second)
	p.add("", "", false, t0.Add(time.Second), time.Second) // straight on: one stretch
	require.Len(t, p.segs, 1)
	assert.Equal(t, []playStretch{{start: t0, dur: 2 * time.Second}}, p.segs[0].stretches)

	p.add("", "", false, t0.Add(3*time.Second), time.Second) // after a stall: a new stretch
	assert.Equal(t, []playStretch{{start: t0, dur: 2 * time.Second}, {start: t0.Add(3 * time.Second), dur: time.Second}},
		p.segs[0].stretches)

	// Played: all of the first stretch and none of the second (the stall).
	text, heard, total := p.heardAt(t0.Add(2500*time.Millisecond), "")
	assert.Equal(t, "a b", text, "2 of its 3 seconds: two of its four words, rounded down")
	assert.Equal(t, 2*time.Second, heard)
	assert.Equal(t, 3*time.Second, total)
}

func TestReplyPlayout_NothingAfterASegmentStillPlayingIsHeard(t *testing.T) {
	t0 := time.Unix(0, 0)
	p := replyPlayout{segs: []playedSegment{
		{text: "One two.", stretches: []playStretch{{start: t0, dur: time.Second}}},
		{text: "Three.", stretches: []playStretch{{start: t0.Add(2 * time.Second), dur: time.Second}}},
		{text: "Four.", stretches: []playStretch{{start: t0.Add(500 * time.Millisecond), dur: time.Second}}},
	}}
	text, heard, total := p.heardAt(t0.Add(time.Second), "")
	assert.Equal(t, "One two.", text, "a segment after one not yet playing is not heard")
	assert.Equal(t, 1500*time.Millisecond, heard)
	assert.Equal(t, 3*time.Second, total)
}

func TestReplyPlayout_ForgetDropsTheHandedOnMessagesSegments(t *testing.T) {
	segs := func() []playedSegment {
		return []playedSegment{{text: "One.", round: "r1"}, {text: "Two.", round: "r2"}, {text: "Three.", round: "r1"}}
	}
	p := replyPlayout{segs: segs()}
	p.forget("ignored", "r1")
	assert.Equal(t, []playedSegment{{text: "Two.", round: "r2"}}, p.segs)

	p = replyPlayout{segs: segs()}
	p.forget("One. Two.", "")
	assert.Equal(t, []playedSegment{{text: "Three.", round: "r1"}}, p.segs, "from the first, those its text holds")

	p = replyPlayout{segs: segs()}
	p.forget("Two.", "")
	assert.Equal(t, segs(), p.segs, "a text that does not begin with the first segment drops none")
}

func TestReplyPlayout_ASegmentWithNoAudioIsNotHeardNorAnythingAfterIt(t *testing.T) {
	t0 := time.Unix(0, 0)
	p := replyPlayout{segs: []playedSegment{
		{text: "Silent.", stretches: []playStretch{{start: t0, dur: 0}}},
		{text: "Spoken.", stretches: []playStretch{{start: t0, dur: time.Second}}},
	}}
	text, heard, total := p.heardAt(t0.Add(2*time.Second), "")
	assert.Equal(t, "", text)
	assert.Equal(t, time.Second, heard)
	assert.Equal(t, time.Second, total)
}
