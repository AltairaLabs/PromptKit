package stage

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// segmentAudioChunk is how much synthesized audio TTSStageWithInterruption
// reads before it hands a piece of a segment on, so a segment starts playing
// while the service is still synthesizing the rest of it.
const segmentAudioChunk = 100 * time.Millisecond

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
// place. The synthesis cost of the segments is stamped on the Message.
//
// A reply a guardrail replaces after some of it was spoken is stored as what
// the caller heard: the spoken segments, then the guardrail's message. Those
// segments had passed the guardrails before they were released.
type replySpeech struct {
	seg speechSegmenter
	// active is set once the turn has released text: its Messages are then
	// matched against it rather than spoken whole.
	active bool
	// unmatched is the released text no completed Message has accounted for.
	unmatched string
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
}

// release takes text the provider stage released and returns the segments it
// completes. A replacement (ReplacesText) discards what was released but not
// yet spoken; it and the text that ends a reply (final) are spoken whole.
func (r *replySpeech) release(text string, replaces, final bool) []string {
	r.active = true
	if replaces {
		r.cut()
	}
	r.unmatched += text
	segs := r.seg.add(text)
	if replaces || final {
		segs = appendNonEmpty(segs, r.seg.flush())
	}
	return segs
}

// complete accounts for a completed assistant Message whose text is text and
// returns the segments still to speak for it.
func (r *replySpeech) complete(text string) []string {
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
		return appendNonEmpty(nil, r.seg.flush())
	case strings.HasPrefix(text, released):
		// The Message goes on past what was released: speak the rest.
		r.unmatched = ""
		return appendNonEmpty(r.seg.add(text[len(released):]), r.seg.flush())
	default:
		// The completed reply is not the text released: a guardrail replaced
		// it after it streamed. What was not yet spoken is dropped.
		r.cut()
		return appendNonEmpty(r.seg.add(text), r.seg.flush())
	}
}

// cut drops what was released and not yet spoken, as a guardrail replaced
// the reply, and records what the caller had heard of it.
func (r *replySpeech) cut() {
	r.seg.reset()
	r.unmatched = ""
	if len(r.heard) > 0 {
		r.heardBeforeCut = strings.Join(r.heard, " ")
	}
}

// heardWith returns msg as the caller heard it: after a cut, the segments
// spoken before it and then msg's text, on a copy; else msg unchanged.
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

// end returns what is left to speak when the turn ends.
func (r *replySpeech) end() []string {
	return appendNonEmpty(nil, r.seg.flush())
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
	r.billed.Reset()
	r.latency = 0
	r.err = nil
	r.heard = nil
	r.heardBeforeCut = ""
}

func appendNonEmpty(segs []string, s string) []string {
	if s == "" {
		return segs
	}
	return append(segs, s)
}

// speakDelta forwards a streaming delta, the reply's text as the caller sees
// it, then speaks the segments it completes.
func (s *TTSStageWithInterruption) speakDelta(
	forwardCtx, synthCtx context.Context, reply *replySpeech, elem *StreamElement, output chan<- StreamElement,
) error {
	if err := s.forwardElement(forwardCtx, *elem, output); err != nil {
		return err
	}
	if elem.Text == nil || *elem.Text == "" {
		return nil
	}
	// A delta that ends a released segment (stream_hold), or the reply, ends
	// a segment to speak now: no later text can extend it.
	final := elem.Meta.FinishReason != nil || elem.Meta.SegmentEnd
	segs := reply.release(*elem.Text, elem.Meta.ReplacesText, final)
	return s.speakSegments(forwardCtx, synthCtx, reply, segs, output)
}

// speakStreamedMessage handles a completed assistant Message of a reply that
// streamed: it speaks what of it was not yet spoken, then forwards it with the
// synthesis cost of the reply's segments, and a synthesis failure, if any.
func (s *TTSStageWithInterruption) speakStreamedMessage(
	forwardCtx, synthCtx context.Context, reply *replySpeech, elem *StreamElement, output chan<- StreamElement,
) error {
	text := s.extractText(elem)
	if text == "" {
		// A tool-call-only round's Message: it has nothing to speak or to
		// price, and the text released so far belongs to a later Message.
		return s.forwardElement(forwardCtx, *elem, output)
	}
	segs := reply.complete(text)
	if err := s.speakSegments(forwardCtx, synthCtx, reply, segs, output); err != nil {
		return err
	}
	out := *elem
	out.Message = withTTSCost(reply.heardWith(elem.Message), reply.takeCost(s.service))
	if reply.err != nil {
		out.Error = reply.err
		reply.err = nil
	}
	return s.forwardElement(forwardCtx, out, output)
}

// endReply speaks what is left of a streamed reply when its turn ends, then
// forgets it. A synthesis failure no Message carried is reported on its own.
func (s *TTSStageWithInterruption) endReply(
	forwardCtx, synthCtx context.Context, reply *replySpeech, output chan<- StreamElement,
) error {
	if !reply.active {
		return nil
	}
	defer reply.reset()
	if err := s.speakSegments(forwardCtx, synthCtx, reply, reply.end(), output); err != nil {
		return err
	}
	if reply.err != nil {
		return s.forwardElement(forwardCtx, NewErrorElement(reply.err), output)
	}
	return nil
}

// speakSegments synthesizes segs in order. A reply a barge-in canceled, or
// whose synthesis failed, speaks nothing more.
func (s *TTSStageWithInterruption) speakSegments(
	forwardCtx, synthCtx context.Context, reply *replySpeech, segs []string, output chan<- StreamElement,
) error {
	for _, seg := range segs {
		if reply.err != nil || synthCtx.Err() != nil {
			return nil
		}
		if s.shouldSkipText(seg) {
			continue
		}
		if err := s.speakSegment(forwardCtx, synthCtx, reply, seg, output); err != nil {
			return err
		}
	}
	return nil
}

// speakSegment synthesizes one segment and emits its audio as the service
// delivers it. A segment read to its end is billed, even when a barge-in then
// drops its audio; one a barge-in canceled earlier is not. The returned error
// is only the pipeline's: a failed synthesis is recorded on reply.
func (s *TTSStageWithInterruption) speakSegment(
	forwardCtx, synthCtx context.Context, reply *replySpeech, text string, output chan<- StreamElement,
) error {
	s.setBotSpeaking(true)
	defer s.setBotSpeaking(false)

	start := time.Now()
	reader, err := tts.SynthesizeWithRetry(synthCtx, s.service, text, s.synthesisConfig(), s.config.Retry)
	if err != nil {
		if errors.Is(err, context.Canceled) || synthCtx.Err() != nil {
			logger.Debug("TTSStageWithInterruption: segment synthesis canceled (barge-in/shutdown)")
			return nil
		}
		logger.Error("TTSStageWithInterruption: synthesis failed", "error", err)
		reply.err = err
		return nil
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			logger.Warn("TTSStageWithInterruption: failed to close reader", "error", closeErr)
		}
	}()

	heard, complete, err := s.streamSegmentAudio(forwardCtx, synthCtx, text, reader, output)
	if heard {
		reply.heard = append(reply.heard, text)
	}
	if complete {
		reply.bill(text, time.Since(start))
	}
	return err
}

// streamSegmentAudio reads a segment's audio in segmentAudioChunk pieces and
// emits each as it arrives, until the audio ends (complete) or synthCtx is
// canceled. The first piece carries the segment's text. heard reports that
// some of the segment's audio was handed on.
func (s *TTSStageWithInterruption) streamSegmentAudio(
	forwardCtx, synthCtx context.Context, text string, reader io.Reader, output chan<- StreamElement,
) (heard, complete bool, err error) {
	buf := make([]byte, s.segmentChunkBytes())
	for {
		n, readErr := io.ReadFull(reader, buf)
		n -= n % bytesPerSample16Bit
		if n > 0 {
			if synthCtx.Err() != nil {
				return heard, false, nil
			}
			pcm := append([]byte(nil), buf[:n]...)
			if err := s.emitSegmentAudio(forwardCtx, text, pcm, !heard, output); err != nil {
				return heard, false, err
			}
			heard = true
		}
		switch {
		case readErr == nil:
		case errors.Is(readErr, io.EOF), errors.Is(readErr, io.ErrUnexpectedEOF):
			return heard, true, nil
		default:
			if !errors.Is(readErr, context.Canceled) && synthCtx.Err() == nil {
				logger.Error("TTSStageWithInterruption: failed to read audio", "error", readErr)
			}
			return heard, false, nil
		}
	}
}

// segmentChunkBytes is the size of segmentAudioChunk of the service's PCM.
func (s *TTSStageWithInterruption) segmentChunkBytes() int {
	samples := int(int64(s.serviceRate) * int64(segmentAudioChunk) / int64(time.Second))
	return max(samples, 1) * bytesPerSample16Bit
}

// emitSegmentAudio emits one piece of a segment's audio. The first piece of a
// segment carries the segment's text and the text the service reports it
// spoke; the text already reached the caller as deltas (SynthesizedSpeech).
func (s *TTSStageWithInterruption) emitSegmentAudio(
	ctx context.Context, text string, pcm []byte, first bool, output chan<- StreamElement,
) error {
	samples, sampleRate := s.toOutputRate(pcm)
	out := StreamElement{
		Audio: &AudioData{
			Samples:    samples,
			SampleRate: sampleRate,
			Channels:   1,
			Format:     AudioFormatPCM16,
		},
		Timestamp: time.Now(),
	}
	out.Meta.SynthesizedSpeech = true
	if first {
		out.Text = &text
		if reporter, ok := s.service.(tts.SpokenTextReporter); ok {
			out.Meta.SpokenText = reporter.SpokenText(text, s.synthesisConfig())
		}
	}
	// The caller plays this after it is handed over, so the bot is still
	// speaking, and interruptible, until it would have finished.
	if s.interruption != nil {
		s.interruption.ExtendPlayback(audio.PCM16Duration(len(samples), sampleRate, 1))
	}
	return s.forwardElement(ctx, out, output)
}

// queueCancelingOnInterrupt is drainCancelingOnInterrupt with an unbounded
// queue: input is always read, so a reply that streams faster than it is
// spoken never holds up the provider stage, and an Interrupt behind it is seen
// (and cancels the turn) at once. Order is preserved.
func queueCancelingOnInterrupt(
	ctx context.Context, input <-chan StreamElement, work chan<- StreamElement, c *turnCanceller,
) {
	defer close(work)
	q := elementQueue{in: input}
	for q.open() {
		select {
		case elem, ok := <-q.in:
			q.receive(elem, ok, c)
		case q.out(work) <- q.next():
			q.pop()
		case <-ctx.Done():
			// The stage has stopped; keep reading so upstream is not blocked.
			q.drainInput()
			return
		}
	}
}

// elementQueue is queueCancelingOnInterrupt's queue: the input, nil once it
// has closed, and the elements read from it and not yet handed on.
type elementQueue struct {
	in    <-chan StreamElement
	items []StreamElement
}

// open reports whether there is more to hand on.
func (q *elementQueue) open() bool { return q.in != nil || len(q.items) > 0 }

// receive takes what a read of the input returned. An Interrupt cancels the
// turn at once, ahead of the elements queued before it.
func (q *elementQueue) receive(elem StreamElement, ok bool, c *turnCanceller) {
	if !ok {
		q.in = nil
		return
	}
	if elem.Interrupt {
		c.interrupt()
	}
	q.items = append(q.items, elem)
}

// out is work while there is an element to hand on, else nil, which blocks.
func (q *elementQueue) out(work chan<- StreamElement) chan<- StreamElement {
	if len(q.items) == 0 {
		return nil
	}
	return work
}

// next is the element to hand on, or the zero element when there is none.
func (q *elementQueue) next() StreamElement {
	if len(q.items) == 0 {
		return StreamElement{}
	}
	return q.items[0]
}

// pop drops the element just handed on.
func (q *elementQueue) pop() {
	q.items[0] = StreamElement{}
	q.items = q.items[1:]
}

// drainInput reads the input to its end, discarding it.
func (q *elementQueue) drainInput() {
	if q.in == nil {
		return
	}
	for range q.in {
	}
}
