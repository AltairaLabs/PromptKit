package stage

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// segmentAudioChunk is how much synthesized audio TTSStageWithInterruption
// reads before it hands a piece of a segment on, so a segment starts playing
// while the service is still synthesizing the rest of it.
const segmentAudioChunk = 100 * time.Millisecond

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
// streamed: it speaks what of it was not yet spoken, waits for its audio to
// play, then forwards it with the synthesis cost of the reply's segments, and
// a synthesis failure, if any. A Message the caller did not hear in full (a
// barge-in cut the reply, or the provider stage kept it as interrupted) is
// forwarded as heard, and nothing more of the reply is spoken.
func (s *TTSStageWithInterruption) speakStreamedMessage(
	forwardCtx, synthCtx context.Context, reply *replySpeech, elem *StreamElement, output chan<- StreamElement,
) error {
	text := s.extractText(elem)
	if text == "" {
		// A tool-call-only round's Message: it has nothing to speak or to
		// price, and the text released so far belongs to a later Message.
		return s.forwardElement(forwardCtx, *elem, output)
	}
	interrupted := elem.Message.FinishReason == types.FinishReasonInterrupted
	if !interrupted {
		if err := s.speakSegments(forwardCtx, synthCtx, reply, reply.complete(text), output); err != nil {
			return err
		}
		// Handed on before its audio has played, the Message would be stored
		// whole though a barge-in may yet cut it.
		s.waitUntil(synthCtx, reply.ended, reply.playout.until)
	}
	out := *elem
	if interrupted || reply.stopped || synthCtx.Err() != nil {
		out.Message = reply.cutMessage(elem.Message, reply.interruptedAt(s.clock.now()))
		reply.stop()
	} else {
		out.Message = reply.heardWith(elem.Message)
	}
	out.Message = withTTSCost(out.Message, reply.takeCost(s.service))
	if reply.err != nil {
		out.Error = reply.err
		reply.err = nil
	}
	reply.messageDone(text)
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

// speakSegments synthesizes segs in order, one segment ahead of the audio
// playing: a segment is synthesized once the one before it starts to play,
// so a long reply is not synthesized (and its unheard audio handed on) far
// ahead of the caller. A reply a barge-in canceled, or whose synthesis
// failed, speaks nothing more.
func (s *TTSStageWithInterruption) speakSegments(
	forwardCtx, synthCtx context.Context, reply *replySpeech, segs []string, output chan<- StreamElement,
) error {
	for i, seg := range segs {
		if reply.err != nil || reply.stopped || synthCtx.Err() != nil {
			return nil
		}
		if s.shouldSkipText(seg) {
			continue
		}
		if !s.waitUntil(synthCtx, reply.ended, reply.playout.lastStart()) {
			return nil
		}
		next := ""
		if i+1 < len(segs) {
			next = segs[i+1]
		}
		if err := s.speakSegment(forwardCtx, synthCtx, reply, seg, next, output); err != nil {
			return err
		}
	}
	return nil
}

// waitUntil waits for the stage's clock to reach t, and reports whether it
// did before ctx ended (a barge-in). A time already reached returns at once,
// and so does every wait once the input has ended (ended): the session is
// ending, so the reply is no longer paced to its playback.
func (s *TTSStageWithInterruption) waitUntil(ctx context.Context, ended <-chan struct{}, t time.Time) bool {
	if !t.After(s.clock.now()) {
		return ctx.Err() == nil
	}
	select {
	case <-s.clock.until(t):
	case <-ended:
	case <-ctx.Done():
	}
	return ctx.Err() == nil
}

// speakSegment synthesizes one segment, with the text spoken before it and
// next, and emits its audio as the service delivers it. A segment read to its
// end is billed, even when a barge-in then drops its audio; one a barge-in
// canceled earlier is not. The returned error is only the pipeline's: a
// failed synthesis is recorded on reply.
func (s *TTSStageWithInterruption) speakSegment(
	forwardCtx, synthCtx context.Context, reply *replySpeech, text, next string, output chan<- StreamElement,
) error {
	s.setBotSpeaking(true)
	defer s.setBotSpeaking(false)

	config := s.synthesisConfig()
	config.PreviousText, config.NextText = reply.last, next
	reply.last = text
	start := time.Now()
	reader, err := tts.SynthesizeWithRetry(synthCtx, s.service, text, config, s.config.Retry)
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

	heard, complete, err := s.streamSegmentAudio(forwardCtx, synthCtx, reply, text, reader, output)
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
	forwardCtx, synthCtx context.Context, reply *replySpeech, text string, reader io.Reader,
	output chan<- StreamElement,
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
			if err := s.emitSegmentAudio(forwardCtx, reply, text, pcm, !heard, output); err != nil {
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

// emitSegmentAudio emits one piece of a segment's audio and records when it
// plays. The first piece of a segment carries the segment's text and the text
// the service reports it spoke; the text already reached the caller as deltas
// (SynthesizedSpeech).
func (s *TTSStageWithInterruption) emitSegmentAudio(
	ctx context.Context, reply *replySpeech, text string, pcm []byte, first bool, output chan<- StreamElement,
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
	d := audio.PCM16Duration(len(samples), sampleRate, 1)
	reply.playout.add(text, first, s.clock.now(), d)
	if s.interruption != nil {
		s.interruption.ExtendPlayback(d)
	}
	return s.forwardElement(ctx, out, output)
}

// queueCancelingOnInterrupt is drainCancelingOnInterrupt with an unbounded
// queue: input is always read, so a reply that streams faster than it is
// spoken never holds up the provider stage, and an Interrupt behind it is seen
// (and cancels the turn) at once. Order is preserved.
func queueCancelingOnInterrupt(
	ctx context.Context, input <-chan StreamElement, work chan<- StreamElement, c *turnCanceller,
	now func() time.Time, ended chan<- struct{},
) {
	defer close(work)
	q := elementQueue{in: input, now: now, ended: ended}
	for q.open() {
		select {
		case elem, ok := <-q.in:
			q.receive(elem, ok, c)
		case q.out(work) <- q.next():
			q.pop()
		case <-ctx.Done():
			// The stage has stopped; keep reading so upstream is not blocked.
			q.endInput()
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
	// now is the clock an Interrupt's arrival is timed on.
	now func() time.Time
	// ended is closed once the input has ended, so a reply waiting for its
	// audio to play stops waiting: nothing more will be played.
	ended chan<- struct{}
}

// endInput closes ended, once.
func (q *elementQueue) endInput() {
	if q.ended != nil {
		close(q.ended)
		q.ended = nil
	}
}

// open reports whether there is more to hand on.
func (q *elementQueue) open() bool { return q.in != nil || len(q.items) > 0 }

// receive takes what a read of the input returned. An Interrupt cancels the
// turn at once, ahead of the elements queued before it.
func (q *elementQueue) receive(elem StreamElement, ok bool, c *turnCanceller) {
	if !ok {
		q.in = nil
		q.endInput()
		return
	}
	if elem.Interrupt {
		c.interruptAt(q.now())
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
