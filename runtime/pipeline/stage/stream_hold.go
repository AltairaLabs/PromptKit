package stage

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// StreamHoldConfig sizes the segments a stream_hold: segment output guardrail
// judges a streamed reply in, and bounds its judging. It is per conversation,
// not per guardrail: every segment guardrail judges the same segments. Zero
// fields take the defaults.
type StreamHoldConfig struct {
	// MinChars is the size, in characters, sentences merge up to before a
	// segment ends. Default 60. A round's first segment ends at 24 characters
	// (or MinChars, when smaller), because the first release is the latency a
	// caller notices most. A segment with no boundary (a long code block,
	// text without punctuation) is cut at the last whitespace before 600
	// characters, or 10 times MinChars when that is larger.
	MinChars int
	// MaxSegmentJudges caps the partial-reply judgments each guardrail makes
	// per round. Past it, the rest of the round is judged once, with the
	// completed reply. Default 8.
	MaxSegmentJudges int
	// SegmentTimeout bounds each partial-reply judgment, unless the guardrail
	// sets stream_hold_timeout. A judgment that runs past it fails closed.
	// Default 10s.
	SegmentTimeout time.Duration

	// Realtime is how a realtime (duplex) session holds a spoken reply for
	// the output guardrails whose stream_hold holds it (segment or turn):
	// RealtimeHoldOff (the default), RealtimeHoldTurn or RealtimeHoldSegment.
	// A realtime model speaks as it writes, so holding the text alone keeps
	// nothing from the caller's ears: the reply's audio is held with it.
	Realtime RealtimeHold
	// Holdback is how far a session's transcript can trail its audio: how
	// much of the reply audio received most recently a RealtimeHoldSegment
	// release keeps back (the audio of words not yet judged), and how long a
	// held reply whose generation completed waits for its last words before
	// it is judged. Zero takes the session's own measure
	// (providers.TranscriptLagger), and none for a session whose transcript
	// never trails.
	Holdback time.Duration

	// firstMinChars and maxChars override the sizes MinChars derives, for
	// tests.
	firstMinChars, maxChars int
}

// RealtimeHold is how a realtime session holds a spoken reply for the output
// guardrails that hold it (StreamHoldConfig.Realtime).
type RealtimeHold string

const (
	// RealtimeHoldOff holds nothing: the reply plays as it arrives, and the
	// guardrails judge its text as it streams and once it completes, as they
	// do without a hold. The default.
	RealtimeHoldOff RealtimeHold = "off"
	// RealtimeHoldTurn holds the whole reply, audio and transcript, until the
	// completed reply passes, then releases it at once.
	RealtimeHoldTurn RealtimeHold = "turn"
	// RealtimeHoldSegment holds the reply's audio and transcript a segment at
	// a time: a segment's audio is released once the guardrails passed the
	// transcript that covers it. A turn guardrail still holds the whole reply.
	RealtimeHoldSegment RealtimeHold = "segment"
)

// Valid reports whether h is a RealtimeHold ("" is RealtimeHoldOff).
func (h RealtimeHold) Valid() bool {
	switch h {
	case "", RealtimeHoldOff, RealtimeHoldTurn, RealtimeHoldSegment:
		return true
	}
	return false
}

// segmentConfig is the segment sizing c sets, defaults applied.
func (c StreamHoldConfig) segmentConfig() segmentConfig {
	c = c.withDefaults()
	return segmentConfig{minChars: c.MinChars, firstMinChars: c.firstMinChars, maxChars: c.maxChars}
}

// Defaults for StreamHoldConfig.
const (
	defaultStreamHoldMinChars         = 60
	defaultStreamHoldFirstMinChars    = 24
	defaultStreamHoldMaxChars         = 600
	defaultStreamHoldMaxSegmentJudges = 8
	defaultStreamHoldSegmentTimeout   = 10 * time.Second
)

// maxCharsPerMinChar scales the segment cap with a large MinChars.
const maxCharsPerMinChar = 10

// withDefaults returns c with every unset field defaulted.
func (c StreamHoldConfig) withDefaults() StreamHoldConfig {
	seg := defaultSegmentConfig()
	if c.MinChars <= 0 {
		c.MinChars = seg.minChars
	}
	if c.firstMinChars <= 0 {
		c.firstMinChars = min(seg.firstMinChars, c.MinChars)
	}
	if c.maxChars <= 0 {
		c.maxChars = max(seg.maxChars, maxCharsPerMinChar*c.MinChars)
	}
	if c.MaxSegmentJudges <= 0 {
		c.MaxSegmentJudges = defaultStreamHoldMaxSegmentJudges
	}
	if c.SegmentTimeout <= 0 {
		c.SegmentTimeout = defaultStreamHoldSegmentTimeout
	}
	return c
}

// streamHold is one streamed round's stream_hold guardrails (hooks.StreamHolder):
// each one's cleared offset feeds the round's holdController, and the
// segment guardrails' partial-reply judgments run here, one at a time per
// guardrail, while the model writes on.
type streamHold struct {
	req *hooks.ProviderRequest
	cfg StreamHoldConfig
	rr  roundRef

	holders []*heldGuardrail
	// judging is false when a turn guardrail holds the round: nothing can be
	// released before the completed reply passes, so no segment is judged.
	judging bool

	parent  context.Context // the round's
	ctx     context.Context // the judgments'; canceled when the round ends or is cut
	cancel  context.CancelFunc
	results chan partialVerdict

	seg *segmenter // the round's segments

	arrivals []textArrival // when each chunk's text arrived
}

// heldGuardrail is one stream_hold guardrail's state in a round.
type heldGuardrail struct {
	hook    hooks.StreamHolder
	index   int // its slot in holdController.held
	segment bool
	busy    bool
	calls   int
	judged  int // the number of segments its last judgment covered
}

// partialVerdict is a segment guardrail's judgment of the reply up to end.
type partialVerdict struct {
	g       *heldGuardrail
	text    string
	segment int
	d       hooks.Decision
	took    time.Duration
}

// textArrival records when the reply's text up to end arrived.
type textArrival struct {
	end int
	at  time.Time
}

// newStreamHold returns the round's stream_hold, or nil when no guardrail
// holds a streamed reply past its chunk check's tail, or the round's text is
// suppressed: the round then streams exactly as without one.
func (s *ProviderStage) newStreamHold(
	ctx context.Context, req *hooks.ProviderRequest, rr roundRef, text *holdController, suppressText bool,
) *streamHold {
	if suppressText || s.hookRegistry == nil {
		return nil
	}
	holders := s.hookRegistry.StreamHolders()
	if len(holders) == 0 {
		return nil
	}
	h := newHeldGuardrails(ctx, holders, req, s.config.StreamHold)
	h.rr = rr
	text.holdFor(len(holders))
	return h
}

// newHeldGuardrails returns the judging of a reply to req that holders hold
// (hooks.StreamHolder), under cfg, until ctx ends: judging is false when one
// holds the whole reply (StreamHoldTurn), since nothing can be released
// before the completed reply passes.
func newHeldGuardrails(
	ctx context.Context, holders []hooks.StreamHolder, req *hooks.ProviderRequest, cfg StreamHoldConfig,
) *streamHold {
	cfg = cfg.withDefaults()
	h := &streamHold{req: req, cfg: cfg, judging: true, seg: newSegmenter(cfg.segmentConfig())}
	for i, hook := range holders {
		g := &heldGuardrail{hook: hook, index: i, segment: hook.StreamHold() == hooks.StreamHoldSegment}
		if !g.segment {
			h.judging = false
		}
		h.holders = append(h.holders, g)
	}
	h.parent = ctx
	h.ctx, h.cancel = context.WithCancel(ctx)
	h.results = make(chan partialVerdict, len(holders))
	return h
}

// inFlight reports whether a judgment is running.
func (h *streamHold) inFlight() bool {
	for _, g := range h.holders {
		if g.busy {
			return true
		}
	}
	return false
}

// stop cancels the judgments still running, reporting how many there were.
func (h *streamHold) stop() int {
	n := 0
	for _, g := range h.holders {
		if g.busy {
			n++
		}
	}
	h.cancel()
	return n
}

// restart discards the round's judging when a retry restarts the reply.
func (h *streamHold) restart() {
	h.cancel()
	h.ctx, h.cancel = context.WithCancel(h.parent)
	h.results = make(chan partialVerdict, len(h.holders))
	h.seg.reset()
	h.arrivals = nil
	for _, g := range h.holders {
		g.busy, g.calls, g.judged = false, 0, 0
	}
}

// received notes that the reply's text up to end arrived now.
func (h *streamHold) received(end int) {
	if n := len(h.arrivals); n == 0 || h.arrivals[n-1].end < end {
		h.arrivals = append(h.arrivals, textArrival{end: end, at: time.Now()})
	}
}

// heldSince is when the byte at offset arrived.
func (h *streamHold) heldSince(offset int) time.Time {
	for _, a := range h.arrivals {
		if offset < a.end {
			return a.at
		}
	}
	return time.Now()
}

// segmentAndJudge takes delta, the text a chunk adds to the reply, finds the
// segments it completes, and starts a judgment for each idle segment
// guardrail with a segment it has not judged.
func (h *streamHold) segmentAndJudge(delta string) {
	h.seg.feed(delta)
	if !h.judging {
		return
	}
	for _, g := range h.holders {
		h.judge(g)
	}
}

// judge starts g's judgment of every segment ready since its last one,
// unless it is busy, has none new, or has made its round's last judgment.
func (h *streamHold) judge(g *heldGuardrail) {
	bounds := h.seg.segments()
	n := len(bounds)
	if !g.segment || g.busy || n == g.judged || g.calls >= h.cfg.MaxSegmentJudges {
		return
	}
	if coalesced := n - g.judged - 1; coalesced > 0 {
		logger.Debug("stream_hold: segments joined into one judgment",
			"guardrail", g.hook.(hooks.ProviderHook).Name(), "segments", coalesced+1)
	}
	g.busy = true
	g.calls++
	g.judged = n
	if g.calls == h.cfg.MaxSegmentJudges {
		logger.Debug("stream_hold: last partial-reply judgment this round; the rest is judged with the reply",
			"guardrail", g.hook.(hooks.ProviderHook).Name(), "judgments", g.calls)
	}
	judged := h.seg.text()[:bounds[n-1]]
	ctx, results, req := h.ctx, h.results, h.req
	reply := hooks.PartialReply{Text: judged, SegmentIndex: n, Timeout: h.cfg.SegmentTimeout}
	go func() {
		start := time.Now()
		d := g.hook.JudgePartialReply(ctx, req, reply)
		select {
		case results <- partialVerdict{g: g, text: judged, segment: n, d: d, took: time.Since(start)}:
		case <-ctx.Done():
		}
	}()
}

// readHeldStream is processStreamChunks for a round under stream_hold
// guardrails (rs.hold): it reads the stream and the segment judgments
// together, so text a judgment clears goes out even while the model is
// between chunks. After the stream ends it waits for the judgments still
// running, then returns with what no judgment cleared still held: the
// completed reply's check decides it (finishStreamedText).
func (s *ProviderStage) readHeldStream(ctx context.Context, rs *roundStream) (streamedRound, error) {
	h := rs.hold
	defer h.stop()
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
		case v := <-h.results:
			if got, done, err := s.applyPartialVerdict(ctx, rs, v); done {
				return got, err
			}
			h.judge(v.g)
		case <-ctx.Done():
			return rs.text.withoutHeld(&rs.acc), fmt.Errorf("stream interrupted: %w", ctx.Err())
		}
	}
	if got, done, err := s.awaitPartialVerdicts(ctx, rs); done {
		return got, err
	}
	// Nothing can complete what the chunk checks held; what the stream_hold
	// guardrails hold waits for the completed reply's check.
	rs.text.clearAll()
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
	rs.hold.segmentAndJudge(chunk.Delta)
	return streamedRound{}, false, nil
}

// awaitPartialVerdicts waits, once the stream has ended, for the judgments
// still running, applying each; it starts no new one. done reports the round
// ends here.
func (s *ProviderStage) awaitPartialVerdicts(
	ctx context.Context, rs *roundStream,
) (got streamedRound, done bool, err error) {
	// Waiting on a judge is activity, not idle silence.
	defer keepIdleAlive(ctx)()
	for rs.hold.inFlight() {
		select {
		case v := <-rs.hold.results:
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
// guardrailOutcome makes of it. done reports the round ends here.
func (s *ProviderStage) applyPartialVerdict(
	ctx context.Context, rs *roundStream, v partialVerdict,
) (got streamedRound, done bool, err error) {
	h := rs.hold
	v.g.busy = false
	if ctx.Err() != nil {
		// The round is ending: nothing was judged, and nothing fired.
		return streamedRound{}, false, nil
	}
	if v.d.Allow {
		rs.text.clearHeld(v.g.index, len(v.text))
		if relErr := s.releaseHeld(ctx, rs, nil, false); relErr != nil {
			return rs.text.withoutHeld(&rs.acc), true, relErr
		}
		return streamedRound{}, false, nil
	}
	canceled := h.stop()
	ov := s.guardrailOutcome().verdict(ctx, h.req, v.text, []hooks.Decision{v.d}, v.took, false)
	acc := &rs.acc
	acc.pendingValidations = append(acc.pendingValidations, ov.validations...)
	acc.content = ov.text
	acc.toolCalls = nil
	acc.finishReason = types.FinishReasonSafety
	acc.replaced = ov.replaces()
	acc.cut = &ov
	if s.emitter != nil {
		name, _ := v.d.Metadata[metadataKeyValidatorType].(string)
		s.emitter.StreamHoldCut(ctx, &events.StreamHoldCutData{
			Round: h.rr.round, ProviderCallID: h.rr.providerCallID, Guardrail: name,
			SegmentIndex: v.segment, ReleasedBytes: rs.text.sent.Len(), CanceledJudges: canceled,
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
	rs.hold.stop()
	rest, ok := strings.CutPrefix(rs.acc.content, rs.text.sent.String())
	if !ok {
		rest = ""
	}
	rs.text.pending = rest
	rs.text.clearAll()
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

// metadataKeyValidatorType is the decision metadata key naming the eval type
// that fired.
const metadataKeyValidatorType = "validator_type"

// releaseHeld emits the text every guardrail has cleared and not yet
// released, ending the reply with finish when it is set, then the media that
// waited behind it, and reports the release (stream.hold.released). The
// delta is marked SegmentEnd when it ends a segment, or ends the reply
// (last).
func (s *ProviderStage) releaseHeld(ctx context.Context, rs *roundStream, finish *string, last bool) error {
	from := rs.text.sent.Len()
	text := rs.text.release()
	if text != "" && !rs.suppressText {
		elem := textDeltaElement(text, finish)
		elem.Meta.SegmentEnd = last || slices.Contains(rs.hold.seg.segments(), rs.text.sent.Len())
		elem.Meta.MessageID = rs.rr.messageID
		if err := sendStreamElement(ctx, elem, rs.output); err != nil {
			return err
		}
		if s.emitter != nil {
			s.emitter.StreamHoldReleased(ctx, &events.StreamHoldReleasedData{
				Round: rs.rr.round, ProviderCallID: rs.rr.providerCallID,
				From: from, To: rs.text.sent.Len(), Held: time.Since(rs.hold.heldSince(from)),
			})
		}
	}
	if rs.text.pending != "" {
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
	ctx context.Context, got *streamedRound, msg *types.Message, output chan<- StreamElement, suppressText bool,
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
		return sendStreamElement(ctx, elem, output)
	}
	if msg.Content == "" {
		return nil
	}
	elem := textDeltaElement(msg.Content, &fr)
	elem.Meta.ReplacesText = true
	elem.Meta.MessageID = msg.ID()
	return sendStreamElement(ctx, elem, output)
}

// streamHoldRequest is the request a round's stream_hold guardrails judge
// its reply against: the one its AfterCall hooks are given.
func (s *ProviderStage) streamHoldRequest(params *streamingRoundParams) *hooks.ProviderRequest {
	return s.providerHookRequest(params.messages, params.systemPrompt, params.round, params.metadata)
}
