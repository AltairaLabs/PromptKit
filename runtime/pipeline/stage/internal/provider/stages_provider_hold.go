package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// newStreamHold returns the round's stream_hold, or nil when no guardrail
// holds a streamed reply past its chunk check's tail, or the round's text is
// suppressed: the round then streams exactly as without one.
func (s *ProviderStage) newStreamHold(
	ctx context.Context, req *hooks.ProviderRequest, rr RoundRef, text *HoldController, suppressText bool,
) *StreamHold {
	if suppressText || s.hookRegistry == nil {
		return nil
	}
	holders := s.hookRegistry.StreamHolders()
	if len(holders) == 0 {
		return nil
	}
	h := NewHeldGuardrails(ctx, holders, req, s.config.StreamHold)
	h.rr = rr
	text.holdFor(len(holders))
	return h
}

// readHeldStream is processStreamChunks for a round under stream_hold
// guardrails (rs.hold): it reads the stream and the segment judgments
// together, so text a judgment clears goes out even while the model is
// between chunks. After the stream ends it waits for the judgments still
// running, then returns with what no judgment cleared still held: the
// completed reply's check decides it (finishStreamedText).
func (s *ProviderStage) readHeldStream(ctx context.Context, rs *roundStream) (streamedRound, error) {
	h := rs.hold
	defer h.Stop()
	for open := true; open; {
		select {
		case chunk, ok := <-rs.streamChan:
			if !ok {
				rs.drained, open = true, false
				continue
			}
			if got, done, err := s.takeHeldChunk(ctx, rs, &chunk); done {
				return got, err
			}
		case v := <-h.Results:
			if got, done, err := s.applyPartialVerdict(ctx, rs, v); done {
				return got, err
			}
			h.Judge(v.Guardrail)
		case <-ctx.Done():
			return rs.text.withoutHeld(&rs.acc), fmt.Errorf("stream interrupted: %w", ctx.Err())
		}
	}
	if got, done, err := s.awaitPartialVerdicts(ctx, rs); done {
		return got, err
	}
	// Nothing can complete what the chunk checks held; what the stream_hold
	// guardrails hold waits for the completed reply's check.
	rs.text.ClearAll()
	if err := s.releaseHeld(ctx, rs, nil, false); err != nil {
		return rs.text.withoutHeld(&rs.acc), err
	}
	got := rs.acc.collected()
	got.hold = rs
	return got, canceledBeforeFinish(ctx, rs.acc.finishReason)
}

// takeHeldChunk takes one chunk of a held round (handleStreamChunk), then
// starts the judgments of the segments it completed.
func (s *ProviderStage) takeHeldChunk(
	ctx context.Context, rs *roundStream, chunk *providers.StreamChunk,
) (got streamedRound, done bool, err error) {
	if chunk.Reset {
		rs.hold.restart()
	}
	if got, done, err = s.handleStreamChunk(ctx, rs, chunk); done {
		return got, done, err
	}
	rs.hold.received(rs.text.received())
	rs.hold.SegmentAndJudge(chunk.Delta)
	return streamedRound{}, false, nil
}

// awaitPartialVerdicts waits, once the stream has ended, for the judgments
// still running, applying each; it starts no new one. done reports the round
// ends here.
func (s *ProviderStage) awaitPartialVerdicts(
	ctx context.Context, rs *roundStream,
) (got streamedRound, done bool, err error) {
	// Waiting on a judge is activity, not idle silence.
	defer core.KeepIdleAlive(ctx)()
	for rs.hold.InFlight() {
		select {
		case v := <-rs.hold.Results:
			if got, done, err = s.applyPartialVerdict(ctx, rs, v); done {
				return got, done, err
			}
		case <-ctx.Done():
			return rs.text.withoutHeld(&rs.acc), true, fmt.Errorf("stream interrupted: %w", ctx.Err())
		}
	}
	return streamedRound{}, false, nil
}

// applyPartialVerdict applies a segment guardrail's judgment: a pass clears
// its hold up to the segment's end and releases what every guardrail has now
// cleared; a firing cuts the reply (cutStream) with the verdict
// GuardrailOutcome makes of it. done reports the round ends here.
func (s *ProviderStage) applyPartialVerdict(
	ctx context.Context, rs *roundStream, v PartialVerdict,
) (got streamedRound, done bool, err error) {
	h := rs.hold
	v.Guardrail.Busy = false
	if ctx.Err() != nil {
		// The round is ending: nothing was judged, and nothing fired.
		return streamedRound{}, false, nil
	}
	if v.Decision.Allow {
		rs.text.clearHeld(v.Guardrail.Index, len(v.Text))
		if relErr := s.releaseHeld(ctx, rs, nil, false); relErr != nil {
			return rs.text.withoutHeld(&rs.acc), true, relErr
		}
		return streamedRound{}, false, nil
	}
	canceled := h.Stop()
	ov := s.guardrailOutcome().Verdict(ctx, h.Req, v.Text, []hooks.Decision{v.Decision}, v.Took, false)
	acc := &rs.acc
	acc.pendingValidations = append(acc.pendingValidations, ov.Validations...)
	acc.content = ov.Text
	acc.toolCalls = nil
	acc.finishReason = types.FinishReasonSafety
	acc.replaced = ov.Replaces()
	acc.cut = &ov
	if s.emitter != nil {
		name, _ := v.Decision.Metadata[MetadataKeyValidatorType].(string)
		s.emitter.StreamHoldCut(ctx, &events.StreamHoldCutData{
			Round: h.rr.Round, ProviderCallID: h.rr.ProviderCallID, Guardrail: name,
			SegmentIndex: v.Segment, ReleasedBytes: rs.text.sent.Len(), CanceledJudges: canceled,
		})
	}
	got, err = s.cutStream(ctx, rs)
	return got, true, err
}

// holdTruncation ends a held round a chunk check truncated: the truncated
// reply is the round's text, the chunk checks hold none of it, and what the
// stream_hold guardrails have cleared is released. The rest stays held for
// the completed reply's check (finishStreamedText), which judges the
// truncated reply. The partial-reply judgments still running are canceled.
func (s *ProviderStage) holdTruncation(ctx context.Context, rs *roundStream) (streamedRound, error) {
	rs.hold.Stop()
	rest, ok := strings.CutPrefix(rs.acc.content, rs.text.sent.String())
	if !ok {
		rest = ""
	}
	rs.text.Pending = rest
	rs.text.ClearAll()
	if err := s.releaseHeld(ctx, rs, nil, false); err != nil {
		return rs.text.withoutHeld(&rs.acc), err
	}
	var late <-chan *types.CostInfo
	rs.drained, late = s.awaitCutUsage(ctx, rs.streamChan, &rs.acc)
	got := rs.acc.collected()
	got.lateUsage = late
	got.hold = rs
	return got, nil
}

// releaseHeld emits the text every guardrail has cleared and not yet
// released, ending the reply with finish when it is set, then the media that
// waited behind it, and reports the release (stream.hold.released). The
// delta is marked SegmentEnd when it ends a segment, or ends the reply
// (last).
func (s *ProviderStage) releaseHeld(ctx context.Context, rs *roundStream, finish *string, last bool) error {
	from := rs.text.sent.Len()
	text := rs.text.Release()
	if text != "" && !rs.suppressText {
		elem := textDeltaElement(text, finish)
		elem.Meta.SegmentEnd = last || slices.Contains(rs.hold.seg.Segments(), rs.text.sent.Len())
		elem.Meta.MessageID = rs.rr.messageID
		if err := core.SendStreamElement(ctx, elem, rs.output); err != nil {
			return err
		}
		if s.emitter != nil {
			s.emitter.StreamHoldReleased(ctx, &events.StreamHoldReleasedData{
				Round: rs.rr.Round, ProviderCallID: rs.rr.ProviderCallID,
				From: from, To: rs.text.sent.Len(), Held: time.Since(rs.hold.heldSince(from)),
			})
		}
	}
	if rs.text.Pending != "" {
		return nil
	}
	return rs.text.sendMedia(ctx, rs.output)
}

// finishStreamedText gives the caller the end of a streamed round's text once
// the completed reply's checks (AfterCall) have run on msg. Text the
// stream_hold guardrails still held is released when they passed it. When a
// check changed the reply, the caller gets the change: what a truncation left
// of the model's text after what was already released, or the replacing
// message, marked as replacing the turn's streamed text. A reply cut while it
// streamed already ended with its message.
func (s *ProviderStage) finishStreamedText(
	ctx context.Context, got *streamedRound, msg *types.Message, output chan<- core.StreamElement, suppressText bool,
) error {
	if suppressText {
		return nil
	}
	rs := got.hold
	if msg.Content == got.content {
		// The checks left the reply as it streamed, or cut it while it
		// streamed and its message already went out.
		if rs == nil {
			return nil
		}
		rs.text.releaseHeld()
		var finish *string
		if msg.FinishReason != "" {
			finish = &msg.FinishReason
		}
		return s.releaseHeld(ctx, rs, finish, true)
	}
	released := got.released
	if rs != nil {
		released = rs.text.sent.String()
	}
	fr := types.FinishReasonSafety
	if rest, ok := strings.CutPrefix(msg.Content, released); ok && strings.HasPrefix(got.content, msg.Content) {
		// A truncation keeps the model's own text.
		if rest == "" {
			return nil
		}
		elem := textDeltaElement(rest, &fr)
		elem.Meta.MessageID = msg.ID()
		return core.SendStreamElement(ctx, elem, output)
	}
	if msg.Content == "" {
		return nil
	}
	elem := textDeltaElement(msg.Content, &fr)
	elem.Meta.ReplacesText = true
	elem.Meta.MessageID = msg.ID()
	return core.SendStreamElement(ctx, elem, output)
}

// streamHoldRequest is the request a round's stream_hold guardrails judge
// its reply against: the one its AfterCall hooks are given.
func (s *ProviderStage) streamHoldRequest(params *streamingRoundParams) *hooks.ProviderRequest {
	return s.providerHookRequest(params.messages, params.systemPrompt, params.round, params.metadata)
}
