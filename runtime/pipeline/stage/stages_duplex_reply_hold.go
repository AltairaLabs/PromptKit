package stage

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// This file holds the realtime stream hold (StreamHoldConfig.Realtime). A
// realtime model speaks as it writes, so holding a reply's text keeps nothing
// from the caller's ears: the stage holds the reply's audio, with its
// transcript, until the output guardrails that hold it (hooks.StreamHolder,
// stream_hold segment or turn) have cleared the transcript that covers it.
//
//   - Turn (RealtimeHoldTurn, or any guardrail holding the whole turn): the
//     reply is held until the completed reply passes, then released at once,
//     in the order it arrived. The completed reply is judged when its content
//     is complete: the turn's completion, or, for a session whose turn
//     completes only once the reply would have finished playing, the chunk
//     marking its generation complete (providers.StreamChunk
//     GenerationComplete).
//   - Segment (RealtimeHoldSegment): the transcript is cut into segments and
//     judged as it streams (the text path's streamHold and segmenter). When
//     every guardrail has passed the transcript up to an offset, the audio
//     that arrived before the transcript delta completing that offset is
//     released, minus a holdback of the most recent audio for a session whose
//     transcript can trail its audio (providers.TranscriptLagger). What is
//     still held when the reply completes goes with the completed reply's
//     verdict.
//
// A failing verdict drops what is held and cuts the reply as any guardrail
// cut does (cutReply): the caller hears what was released, then the
// guardrail's message, and the turn's transcript is what was heard, the
// released prefix followed by the message. A barge-in drops what is held: it
// was never heard. A session that estimates what the caller heard from the
// audio it delivers (providers.PlayoutHolder) is told to count audio as it is
// released, so dropped audio never counts as heard.

// duplexReplyHold is the hold on the reply streaming now.
type duplexReplyHold struct {
	// gen is the reply's generation, 0 when the provider does not number them.
	gen int64
	// judge runs the segment guardrails' judgments of the transcript; its
	// judging is false when the whole reply is held.
	judge *streamHold
	// cleared is each holding guardrail's cleared offset into the transcript.
	cleared []int
	// held is the reply's output still held, in arrival order.
	held []heldOutput
	// text and audio are how much transcript (bytes) and audio (playing time)
	// the reply has delivered so far.
	text  int
	audio time.Duration
	// holdback is the most recent audio a segment release keeps back.
	holdback time.Duration
	// heard is the transcript released to the caller, and heardAudio the
	// bytes of audio.
	heard      strings.Builder
	heardAudio int
	// passed is set once the completed reply passed before its turn
	// completed (GenerationComplete): what still arrives of it is not held.
	passed bool
	// ending is set once the reply's content is complete: the completed
	// reply's check judges what no segment judgment has, so none starts.
	ending bool
}

// heldOutput is a piece of the reply held back from the caller.
type heldOutput struct {
	elem StreamElement
	// src is the chunk that carried the audio, for the session's playout
	// (providers.PlayoutHolder).
	src  providers.StreamChunk
	text string
	// textEnd and audioEnd are the transcript and audio delivered up to and
	// including this piece.
	textEnd  int
	audioEnd time.Duration
}

// replyHolders returns the guardrails a realtime hold holds replies for:
// none when the session does not hold (RealtimeHoldOff).
func (s *DuplexProviderStage) replyHolders() []hooks.StreamHolder {
	switch s.streamHold.Realtime {
	case RealtimeHoldTurn, RealtimeHoldSegment:
		return s.hookRegistry.StreamHolders()
	case RealtimeHoldOff:
	}
	return nil
}

// holdPlayout has a session that estimates playout count reply audio only as
// it is released, when the stage holds replies.
func (s *DuplexProviderStage) holdPlayout() {
	if len(s.replyHolders()) == 0 {
		return
	}
	if ph, ok := s.session.(providers.PlayoutHolder); ok {
		ph.HoldPlayout()
		s.playoutHeld = ph
	}
}

// beginReplyHold starts holding the reply whose first content chunk is
// chunk, when guardrails hold replies and the reply is not blocked already.
func (s *DuplexProviderStage) beginReplyHold(
	ctx context.Context, chunk *providers.StreamChunk, isOutputTranscription bool,
) {
	if h := s.replyHold; h != nil && h.gen != 0 && chunk.Generation != 0 && chunk.Generation != h.gen {
		// The provider moved on to another reply: what the superseded one
		// still held is never heard, and its message is dropped with it.
		h.judge.stop()
		s.replyHold = nil
	}
	if s.replyHold != nil || s.turnGuard.blocked || !isAssistantContentChunk(chunk, isOutputTranscription) {
		return
	}
	holders := s.replyHolders()
	if len(holders) == 0 {
		return
	}
	h := &duplexReplyHold{cleared: make([]int, len(holders)), holdback: s.streamHold.Holdback, gen: chunk.Generation}
	h.judge = newHeldGuardrails(ctx, holders, s.hookRequest(s.history.snapshot()), s.streamHold)
	if s.streamHold.Realtime != RealtimeHoldSegment {
		h.judge.judging = false
	}
	if h.holdback <= 0 {
		if tl, ok := s.session.(providers.TranscriptLagger); ok {
			h.holdback = tl.OutputTranscriptLag()
		}
	}
	s.replyHold = h
}

// holdReplyOutput takes the reply's text and audio off elem into the hold,
// starting the segment judgments the transcript completes, and reports
// whether it took any: an element it took from that is left with nothing else
// to send is not sent. Audio of a reply not held (or already passed) is
// reported to a session that counts released audio.
func (s *DuplexProviderStage) holdReplyOutput(chunk *providers.StreamChunk, elem *StreamElement) bool {
	if elem.Text == nil && elem.Audio == nil {
		return false
	}
	h := s.replyHold
	if h == nil || h.passed {
		if elem.Audio != nil && s.playoutHeld != nil {
			s.playoutHeld.AudioReleased(chunk)
		}
		return false
	}
	out := heldOutput{elem: StreamElement{Text: elem.Text, Audio: elem.Audio, Timestamp: elem.Timestamp}}
	if elem.Text != nil {
		out.text = *elem.Text
		h.text += len(out.text)
	}
	if elem.Audio != nil {
		out.src = providers.StreamChunk{MediaData: chunk.MediaData, Metadata: chunk.Metadata}
		h.audio += audio.PCM16Duration(len(elem.Audio.Samples), elem.Audio.SampleRate, elem.Audio.Channels)
	}
	out.textEnd, out.audioEnd = h.text, h.audio
	h.held = append(h.held, out)
	elem.Text, elem.Audio = nil, nil
	if out.text != "" {
		h.judge.segmentAndJudge(out.text)
	}
	return true
}

// replyHoldVerdicts is the channel the held reply's segment judgments
// arrive on, or nil (never ready) when none can.
func (s *DuplexProviderStage) replyHoldVerdicts() <-chan partialVerdict {
	if s.replyHold == nil || !s.replyHold.judge.judging {
		return nil
	}
	return s.replyHold.judge.results
}

// onReplyVerdict applies a segment guardrail's judgment of the held reply: a
// pass releases what every guardrail has now cleared; a firing cuts the
// reply with the guardrail's message.
func (s *DuplexProviderStage) onReplyVerdict(ctx context.Context, v partialVerdict, output chan<- StreamElement) error {
	h := s.replyHold
	v.g.busy = false
	if ctx.Err() != nil {
		return nil
	}
	if v.d.Allow {
		h.cleared[v.g.index] = max(h.cleared[v.g.index], len(v.text))
		if err := s.releaseCleared(ctx, output); err != nil {
			return err
		}
		if !h.ending {
			h.judge.judge(v.g)
		}
		return nil
	}
	canceled := h.judge.stop()
	g := s.guardrailOutcomeAt(h.judge.req.TurnIndex)
	ov := g.verdict(ctx, h.judge.req, v.text, []hooks.Decision{v.d}, v.took, false)
	s.noteBlock(ov.decision, hooks.DirectionOutput, s.replyGeneration())
	s.turnGuard.validations = append(s.turnGuard.validations, ov.validations...)
	if s.emitter != nil {
		name, _ := v.d.Metadata[metadataKeyValidatorType].(string)
		s.emitter.StreamHoldCut(ctx, &events.StreamHoldCutData{
			Guardrail: name, SegmentIndex: v.segment, ReleasedBytes: h.heard.Len(), CanceledJudges: canceled,
		})
	}
	s.blockTurnWithMessage(types.Message{Content: ov.text, Meta: decisionMessageMeta(ov.decision)})
	return s.cutReply(ctx, output)
}

// releaseCleared releases the held output every guardrail has cleared: the
// pieces up to the one whose transcript completed the cleared offset, whose
// own transcript does not run past it, and whose audio is not within the
// holdback of the audio delivered when that transcript arrived.
func (s *DuplexProviderStage) releaseCleared(ctx context.Context, output chan<- StreamElement) error {
	h := s.replyHold
	upto := h.cleared[0]
	for _, c := range h.cleared[1:] {
		upto = min(upto, c)
	}
	// The piece whose transcript completed the cleared offset; with nothing
	// cleared, none did.
	last := -1
	for i := range h.held {
		if upto > 0 && h.held[i].text != "" && h.held[i].textEnd >= upto {
			last = i
			break
		}
	}
	if last < 0 {
		return nil
	}
	audioCut := h.held[last].audioEnd - h.holdback
	n := 0
	for i := 0; i <= last; i++ {
		o := &h.held[i]
		if o.textEnd > upto || (o.elem.Audio != nil && o.audioEnd > audioCut) {
			break
		}
		n++
	}
	return s.releaseHeldOutput(ctx, n, output)
}

// releaseHeldOutput sends the first n pieces of held output, in order, and
// reports their audio to a session that counts released audio.
func (s *DuplexProviderStage) releaseHeldOutput(ctx context.Context, n int, output chan<- StreamElement) error {
	h := s.replyHold
	for range n {
		o := h.held[0]
		h.held = h.held[1:]
		if err := s.emit(ctx, o.elem, output); err != nil {
			return err
		}
		h.heard.WriteString(o.text)
		if o.elem.Audio != nil {
			h.heardAudio += len(o.elem.Audio.Samples)
			if s.playoutHeld != nil {
				s.playoutHeld.AudioReleased(&o.src)
			}
		}
	}
	return nil
}

// onGenerationComplete judges a held reply whose content is complete before
// its turn completes (GenerationComplete): it passes and is released now, or
// is cut now. A reply making tool calls waits for its turn's completion,
// whose check judges the calls too.
func (s *DuplexProviderStage) onGenerationComplete(ctx context.Context, output chan<- StreamElement) error {
	if err := s.awaitReplyVerdicts(ctx, output); err != nil {
		return err
	}
	h := s.replyHold
	if h == nil || h.passed || s.turnGuard.blocked || len(s.accumulatedToolCalls) > 0 {
		return nil
	}
	h.judge.stop()
	msg := &types.Message{Role: roleAssistant, Content: s.accumulatedText.String()}
	if s.runTurnAfterCallHooks(ctx, msg, &StreamElement{}) {
		s.turnGuard.validations = append(s.turnGuard.validations, msg.Validations...)
		s.blockTurnWithMessage(types.Message{Content: msg.Content, Meta: msg.Meta})
		return s.cutReply(ctx, output)
	}
	passed := msg.Content
	s.turnGuard.passed = &passed
	h.passed = true
	return s.releaseHeldOutput(ctx, len(h.held), output)
}

// settleReplyHold ends the hold when the reply's turn completes (turnDone),
// elem being the element that completes it, after its guardrails ran: what is
// held is released when the turn passed, and dropped when it was blocked or
// cut short, its Message then keeping only what the caller heard.
func (s *DuplexProviderStage) settleReplyHold(
	ctx context.Context, elem *StreamElement, turnDone bool, output chan<- StreamElement,
) error {
	h := s.replyHold
	if h == nil || !turnDone {
		return nil
	}
	s.replyHold = nil
	h.judge.stop()
	msg := elem.Message
	fr := ""
	if elem.Meta.FinishReason != nil {
		fr = *elem.Meta.FinishReason
	}
	switch fr {
	case types.FinishReasonSafety:
		keepHeardAudio(msg, h.heardAudio)
	case finishReasonInterrupted:
		if msg != nil {
			msg.Content = h.heard.String()
			msg.Parts = replaceTextParts(msg.Parts, msg.Content)
			keepHeardAudio(msg, h.heardAudio)
		}
	default:
		s.replyHold = h
		err := s.releaseHeldOutput(ctx, len(h.held), output)
		s.replyHold = nil
		return err
	}
	return nil
}

// dropReplyHold drops what the hold still holds, when the reply is cut or
// cut short, leaving the turn's audio what the caller heard and, with
// heardText, its transcript too. It does nothing without a hold.
func (s *DuplexProviderStage) dropReplyHold(heardText bool) {
	h := s.replyHold
	if h == nil {
		return
	}
	s.replyHold = nil
	h.judge.stop()
	h.held = nil
	if heardText {
		s.accumulatedText.Reset()
		s.accumulatedText.WriteString(h.heard.String())
	}
	s.accumulatedMedia = s.accumulatedMedia[:min(len(s.accumulatedMedia), h.heardAudio)]
}

// heardReply is the transcript of the held reply streaming now that the
// caller has heard: what was released. "" without a hold.
func (s *DuplexProviderStage) heardReply() string {
	if s.replyHold == nil {
		return ""
	}
	return s.replyHold.heard.String()
}

// isGenerationCompleteOnly reports whether chunk only marks its reply's
// content complete (providers.StreamChunk GenerationComplete).
func isGenerationCompleteOnly(chunk *providers.StreamChunk) bool {
	return chunk.GenerationComplete && chunk.Content == "" && chunk.Delta == "" && chunk.MediaData == nil &&
		chunk.FinishReason == nil && len(chunk.ToolCalls) == 0 && chunk.CostInfo == nil && !chunk.Interrupted
}

// heardAndMessage is what a guardrail's message makes of a turn whose
// beginning the caller already heard under a hold: the turn's text is the
// heard prefix followed by the message, and the live text that follows what
// was shown. Without a heard prefix it is the message, replacing the turn's
// text.
func heardAndMessage(heard, message string) (content, live string, replaces bool) {
	if heard == "" || message == "" {
		return message, message, true
	}
	sep := " "
	if strings.HasSuffix(heard, " ") || strings.HasSuffix(heard, "\n") {
		sep = ""
	}
	return heard + sep + message, sep + message, false
}

// keepHeardAudio leaves msg's audio part only the first n bytes of its audio,
// what the caller heard, dropping the part when none was.
func keepHeardAudio(msg *types.Message, n int) {
	if msg == nil {
		return
	}
	parts := msg.Parts[:0:0]
	for _, p := range msg.Parts {
		if p.Type != types.ContentTypeAudio || p.Media == nil || p.Media.Data == nil {
			parts = append(parts, p)
			continue
		}
		if n == 0 {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(*p.Media.Data)
		if err == nil {
			data := base64.StdEncoding.EncodeToString(raw[:min(len(raw), n)])
			media := *p.Media
			media.Data = &data
			p.Media = &media
		}
		parts = append(parts, p)
	}
	msg.Parts = parts
}

// finishReplyVerdicts has the held reply's segment judgments still running
// decide, when chunk completes the turn, before the turn's message is built:
// a reply they cut loses the chunk's own output.
func (s *DuplexProviderStage) finishReplyVerdicts(
	ctx context.Context, chunk *providers.StreamChunk, isOutputTranscription bool, output chan<- StreamElement,
) error {
	if chunk.FinishReason == nil || *chunk.FinishReason == "" {
		return nil
	}
	if err := s.awaitReplyVerdicts(ctx, output); err != nil {
		return err
	}
	if s.turnGuard.blocked {
		dropAssistantOutput(chunk, isOutputTranscription)
	}
	return nil
}

// awaitReplyVerdicts waits, once the held reply's content is complete, for
// the segment judgments still running, applying each: a firing among them
// cuts the reply, whatever the completed reply's check would say. No new
// judgment starts: that check judges the rest.
func (s *DuplexProviderStage) awaitReplyVerdicts(ctx context.Context, output chan<- StreamElement) error {
	h := s.replyHold
	if h == nil || !h.judge.judging {
		return nil
	}
	h.ending = true
	// Waiting on a judge is activity, not idle silence.
	defer keepIdleAlive(ctx)()
	for s.replyHold == h && h.judge.inFlight() {
		select {
		case v := <-h.judge.results:
			if err := s.onReplyVerdict(ctx, v, output); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
