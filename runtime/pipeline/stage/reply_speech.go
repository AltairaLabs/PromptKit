package stage

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// replySpeech is the state of the reply TTSStageWithInterruption is speaking
// as it streams: the text the provider stage released to the caller (its
// streaming deltas) is split into segments (speechSegmenter), and each is
// synthesized as soon as it is complete. The provider stage releases only
// text its output guardrails have cleared, so held text is never spoken.
//
// When the reply's completed Message arrives, its text is matched against the
// released text: what the deltas already spoke is not spoken again, and a
// Message whose text is not what was released (a guardrail replaced the reply
// after it streamed) has what was not yet spoken dropped and is spoken in its
// place. A replacement or a rewritten Message that begins with what was
// already spoken speaks only the rest, and one that ends inside it (a
// truncation) speaks nothing more. The synthesis cost of the segments is
// stamped on the Message.
//
// The Message records what the caller heard. A reply a guardrail replaced
// after some of it was spoken is the spoken segments, then the guardrail's
// message (those segments had passed the guardrails before they were
// released). A reply a barge-in cut is what its audio had played when the
// caller spoke (playout).
type replySpeech struct {
	seg speechSegmenter
	// active is set once the turn has released text: its Messages are then
	// matched against it rather than spoken whole.
	active bool
	// unmatched is the released text no completed Message has accounted for.
	unmatched string
	// sent is the text of the segments handed to synthesis that no completed
	// Message has accounted for: what a replacement is compared with.
	sent string
	// last is the text of the last segment handed to synthesis, the text
	// before the next one (tts.SynthesisConfig.PreviousText).
	last string
	// stopped is set once the reply was cut short (a barge-in, or a Message
	// the provider stage kept as interrupted): nothing more of it is spoken.
	stopped bool
	// billed is the text of the segments synthesized and not yet priced on a
	// Message, and latency the time their synthesis took.
	billed  strings.Builder
	latency time.Duration
	// err is the first synthesis failure; the reply speaks nothing after it,
	// and its next Message carries it.
	err error
	// heard is the text of the segments whose audio reached the caller.
	heard []string
	// heardBeforeCut is what the caller had heard when a guardrail replaced
	// the reply, for the replaced Message to record.
	heardBeforeCut string
	// playout is when each spoken segment's audio plays.
	playout replyPlayout
	// cutAt reports when a barge-in canceled the turn (zero when none has).
	cutAt func() time.Time
	// ended is closed once the stage's input has ended.
	ended <-chan struct{}
}

// release takes text the provider stage released and returns the segments it
// completes. A replacement (ReplacesText) discards what was released but not
// yet spoken, unless it begins with what was spoken (it speaks only the rest)
// or ends inside it (it speaks nothing more); it and the text that ends a
// reply (final) are spoken whole.
func (r *replySpeech) release(text string, replaces, final bool) []string {
	r.active = true
	if r.stopped {
		return nil
	}
	if replaces {
		if segs, ok := r.overlap(text, true); ok {
			return segs
		}
		r.cut()
	}
	r.unmatched += text
	segs := r.seg.add(text)
	if replaces || final {
		segs = appendNonEmpty(segs, r.seg.flush())
	}
	return r.sending(segs)
}

// overlap handles text that replaces the reply (a replacement delta, or a
// completed Message that is not what streamed) when it overlaps what was
// already spoken: text that begins with it speaks only what follows, and text
// that ends inside it speaks nothing more. Either way text is now the reply.
// ok is false when text does not overlap what was spoken.
func (r *replySpeech) overlap(text string, released bool) ([]string, bool) {
	if strings.TrimSpace(r.sent) == "" {
		return nil, false
	}
	if rest, ok := cutPrefix(text, r.sent); ok {
		r.seg.reset()
		r.unmatched = ""
		if released {
			r.unmatched = text
		}
		r.sent = ""
		return r.sending(appendNonEmpty(r.seg.add(rest), r.seg.flush())), true
	}
	if _, ok := cutPrefix(r.sent, text); ok {
		r.seg.reset()
		r.unmatched = ""
		if released {
			r.unmatched = text
		}
		r.sent = ""
		return nil, true
	}
	return nil, false
}

// sending records segs as handed to synthesis and returns them.
func (r *replySpeech) sending(segs []string) []string {
	for _, s := range segs {
		r.sent = joinText(r.sent, s)
	}
	return segs
}

// complete accounts for a completed assistant Message whose text is text and
// returns the segments still to speak for it.
func (r *replySpeech) complete(text string) []string {
	if r.stopped {
		return nil
	}
	released := strings.TrimLeftFunc(r.unmatched, unicode.IsSpace)
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	switch {
	case strings.HasPrefix(released, text):
		// Already released. Text after it belongs to a later Message (a
		// later round of the tool loop), so the segmenter keeps any partial
		// segment for it.
		r.unmatched = released[len(text):]
		if strings.TrimSpace(r.unmatched) != "" {
			return nil
		}
		r.unmatched = ""
		return r.sending(appendNonEmpty(nil, r.seg.flush()))
	case strings.HasPrefix(text, released):
		// The Message goes on past what was released: speak the rest.
		r.unmatched = ""
		return r.sending(appendNonEmpty(r.seg.add(text[len(released):]), r.seg.flush()))
	default:
		// The completed reply is not the text released: a guardrail replaced
		// it after it streamed.
		if segs, ok := r.overlap(text, false); ok {
			return segs
		}
		// What was not yet spoken is dropped, and the Message is spoken.
		r.cut()
		return r.sending(appendNonEmpty(r.seg.add(text), r.seg.flush()))
	}
}

// cut drops what was released and not yet spoken, as a guardrail replaced
// the reply, and records what the caller had heard of it.
func (r *replySpeech) cut() {
	r.seg.reset()
	r.unmatched = ""
	r.sent = ""
	if len(r.heard) > 0 {
		r.heardBeforeCut = strings.Join(r.heard, " ")
	}
}

// heardWith returns msg as the caller heard it: after a guardrail cut, the
// segments spoken before it and then msg's text, on a copy; else msg
// unchanged.
func (r *replySpeech) heardWith(msg *types.Message) *types.Message {
	if r.heardBeforeCut == "" || msg == nil {
		return msg
	}
	heard := *msg
	heard.Content = r.heardBeforeCut + " " + strings.TrimLeftFunc(msg.Content, unicode.IsSpace)
	heard.Parts = nil
	r.heardBeforeCut = ""
	return &heard
}

// stop cuts the reply short: what is not yet spoken never is.
func (r *replySpeech) stop() {
	r.stopped = true
	r.seg.reset()
	r.unmatched = ""
}

// cutMessage returns msg as the caller heard it when its playback was cut at
// t (cause, types.PlayoutCut*): the text its audio had played (playout), with
// heard_ms, audio_ms, played_fraction and playout_cut as the realtime playout
// hold records them. A barge-in marks it interrupted; a session that closed
// leaves the reply its own finish reason, as the realtime hold does.
func (r *replySpeech) cutMessage(msg *types.Message, t time.Time, cause string) *types.Message {
	// The playout holds every segment spoken for this Message, those before
	// a guardrail's cut among them.
	text, heard, total := r.playout.heardAt(t)
	r.heardBeforeCut = ""
	cut := heardReply(msg, heard, total, cause)
	if rest, ok := cutPrefix(msg.Content, text); !ok || strings.TrimSpace(rest) != "" {
		// Not all of it was heard: the text is what was. (All of it heard,
		// the reply keeps its own spacing.)
		cut.Content = text
		cut.Parts = nil
	}
	if cause == types.PlayoutCutBargeIn {
		cut.FinishReason = types.FinishReasonInterrupted
	}
	return cut
}

// messageDone forgets what the Message just handed on accounted for: the
// text it sent, and the segments whose audio carried it.
func (r *replySpeech) messageDone(text string) {
	if rest, ok := cutPrefix(r.sent, text); ok {
		r.sent = strings.TrimSpace(rest)
	} else {
		r.sent = ""
	}
	r.playout.forget(text)
	r.heard = nil
}

// end returns what is left to speak when the turn ends.
func (r *replySpeech) end() []string {
	if r.stopped {
		return nil
	}
	return r.sending(appendNonEmpty(nil, r.seg.flush()))
}

// bill records a segment the service synthesized in full: it is priced.
func (r *replySpeech) bill(text string, took time.Duration) {
	r.billed.WriteString(text)
	r.latency += took
}

// takeCost returns the cost of the segments billed since the last call, the
// cost of their text as one request (pricing is per character, so this is
// the sum of theirs), or nil when none was billed or the service is unpriced.
func (r *replySpeech) takeCost(service tts.Service) *types.CostInfo {
	if r.billed.Len() == 0 {
		return nil
	}
	cost := tts.ComputeTTSCost(service, r.billed.String(), r.latency)
	r.billed.Reset()
	r.latency = 0
	return cost
}

// reset forgets the reply: the turn ended, or a barge-in cut it.
func (r *replySpeech) reset() {
	r.seg.reset()
	r.active = false
	r.unmatched = ""
	r.sent = ""
	r.last = ""
	r.stopped = false
	r.billed.Reset()
	r.latency = 0
	r.err = nil
	r.heard = nil
	r.heardBeforeCut = ""
	r.playout = replyPlayout{until: r.playout.until}
}

// interrupted forgets the reply a barge-in cut: the caller stopped playing
// its audio, so the next reply's plays from when it arrives.
func (r *replySpeech) interrupted() {
	r.reset()
	r.playout = replyPlayout{}
}

// interruptedAt is when the reply was cut: the barge-in's time, else now.
func (r *replySpeech) interruptedAt(now time.Time) time.Time {
	if r.cutAt != nil {
		if t := r.cutAt(); !t.IsZero() {
			return t
		}
	}
	return now
}

func appendNonEmpty(segs []string, s string) []string {
	if s == "" {
		return segs
	}
	return append(segs, s)
}

// joinText joins two pieces of a reply's text with a space.
func joinText(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}

// cutPrefix reports whether s begins with prefix, whitespace aside (the
// segments of a reply are trimmed, so their spacing differs from the reply's),
// and returns what of s follows it.
func cutPrefix(s, prefix string) (string, bool) {
	i := 0
	for _, want := range prefix {
		if unicode.IsSpace(want) {
			continue
		}
		for i < len(s) {
			r, size := utf8.DecodeRuneInString(s[i:])
			if !unicode.IsSpace(r) {
				break
			}
			i += size
		}
		if i >= len(s) {
			return "", false
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != want {
			return "", false
		}
		i += size
	}
	return s[i:], true
}

// replyPlayout estimates when each spoken segment of a reply plays, assuming
// the caller plays its audio in real time as it arrives, or as soon as the
// audio ahead of it finishes (as audio.Playout and the interruption handler's
// ExtendPlayback do).
type replyPlayout struct {
	// until is when the audio handed on so far finishes playing.
	until time.Time
	segs  []playedSegment
}

// playedSegment is a spoken segment's text and the stretches its audio plays
// in (more than one when its audio stalled).
type playedSegment struct {
	text      string
	stretches []playStretch
}

type playStretch struct {
	start time.Time
	dur   time.Duration
}

// add records d of a segment's audio handed on at now; first starts the
// segment.
func (p *replyPlayout) add(text string, first bool, now time.Time, d time.Duration) {
	if first || len(p.segs) == 0 {
		p.segs = append(p.segs, playedSegment{text: text})
	}
	start := p.until
	if now.After(start) {
		start = now
	}
	seg := &p.segs[len(p.segs)-1]
	if n := len(seg.stretches); n > 0 && seg.stretches[n-1].start.Add(seg.stretches[n-1].dur).Equal(start) {
		seg.stretches[n-1].dur += d
	} else {
		seg.stretches = append(seg.stretches, playStretch{start: start, dur: d})
	}
	p.until = start.Add(d)
}

// lastStart is when the last spoken segment starts to play: the zero time
// when none was spoken.
func (p *replyPlayout) lastStart() time.Time {
	if len(p.segs) == 0 || len(p.segs[len(p.segs)-1].stretches) == 0 {
		return time.Time{}
	}
	return p.segs[len(p.segs)-1].stretches[0].start
}

// heardAt returns what the caller had heard by t: the text of the segments
// that had played, then, of the one playing, its share of words its audio
// had played (rounded down), with the audio heard and the audio handed on.
func (p *replyPlayout) heardAt(t time.Time) (text string, heard, total time.Duration) {
	cut := false
	for _, seg := range p.segs {
		var played, dur time.Duration
		for _, st := range seg.stretches {
			dur += st.dur
			played += min(max(t.Sub(st.start), 0), st.dur)
		}
		total += dur
		heard += played
		if cut || played <= 0 {
			cut = true
			continue
		}
		if played >= dur {
			text = joinText(text, seg.text)
			continue
		}
		text = joinText(text, leadingWords(seg.text, float64(played)/float64(dur)))
		cut = true
	}
	return text, heard, total
}

// forget drops the segments whose text the handed-on Message's text holds,
// from the first: their audio belongs to that Message, not the next.
func (p *replyPlayout) forget(text string) {
	i := 0
	for i < len(p.segs) {
		rest, ok := cutPrefix(text, p.segs[i].text)
		if !ok {
			break
		}
		text = rest
		i++
	}
	p.segs = p.segs[i:]
}

// leadingWords is the first share (0 to 1) of text's words, rounded down.
func leadingWords(text string, share float64) string {
	words := strings.Fields(text)
	return strings.Join(words[:int(float64(len(words))*share)], " ")
}
