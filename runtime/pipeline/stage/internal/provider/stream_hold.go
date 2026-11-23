package provider

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/segment"
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
func (c StreamHoldConfig) segmentConfig() segment.Config {
	c = c.withDefaults()
	return segment.Config{MinChars: c.MinChars, FirstMinChars: c.firstMinChars, MaxChars: c.maxChars}
}

// defaultSegmentConfig is the segment sizing stream_hold uses by default.
func defaultSegmentConfig() segment.Config {
	return segment.Config{
		MinChars:      defaultStreamHoldMinChars,
		FirstMinChars: defaultStreamHoldFirstMinChars,
		MaxChars:      defaultStreamHoldMaxChars,
	}
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
		c.MinChars = seg.MinChars
	}
	if c.firstMinChars <= 0 {
		c.firstMinChars = min(seg.FirstMinChars, c.MinChars)
	}
	if c.maxChars <= 0 {
		c.maxChars = max(seg.MaxChars, maxCharsPerMinChar*c.MinChars)
	}
	if c.MaxSegmentJudges <= 0 {
		c.MaxSegmentJudges = defaultStreamHoldMaxSegmentJudges
	}
	if c.SegmentTimeout <= 0 {
		c.SegmentTimeout = defaultStreamHoldSegmentTimeout
	}
	return c
}

// StreamHold is one streamed round's stream_hold guardrails (hooks.StreamHolder):
// each one's cleared offset feeds the round's HoldController, and the
// segment guardrails' partial-reply judgments run here, one at a time per
// guardrail, while the model writes on.
type StreamHold struct {
	Req *hooks.ProviderRequest
	cfg StreamHoldConfig
	rr  RoundRef

	Holders []*HeldGuardrail
	// Judging is false when a turn guardrail holds the round: nothing can be
	// released before the completed reply passes, so no segment is judged.
	Judging bool

	parent  context.Context // the round's
	Ctx     context.Context // the judgments'; canceled when the round ends or is cut
	cancel  context.CancelFunc
	Results chan PartialVerdict

	seg *segment.Segmenter // the round's segments

	arrivals []textArrival // when each chunk's text arrived
}

// HeldGuardrail is one stream_hold guardrail's state in a round.
type HeldGuardrail struct {
	hook    hooks.StreamHolder
	Index   int // its slot in HoldController.held
	segment bool
	Busy    bool
	calls   int
	judged  int // the number of segments its last judgment covered
}

// PartialVerdict is a segment guardrail's judgment of the reply up to end.
type PartialVerdict struct {
	Guardrail *HeldGuardrail
	Text      string
	Segment   int
	Decision  hooks.Decision
	Took      time.Duration
}

// textArrival records when the reply's text up to end arrived.
type textArrival struct {
	end int
	at  time.Time
}

// NewHeldGuardrails returns the judging of a reply to req that holders hold
// (hooks.StreamHolder), under cfg, until ctx ends: judging is false when one
// holds the whole reply (StreamHoldTurn), since nothing can be released
// before the completed reply passes.
func NewHeldGuardrails(
	ctx context.Context, holders []hooks.StreamHolder, req *hooks.ProviderRequest, cfg StreamHoldConfig,
) *StreamHold {
	cfg = cfg.withDefaults()
	h := &StreamHold{Req: req, cfg: cfg, Judging: true, seg: segment.New(cfg.segmentConfig())}
	for i, hook := range holders {
		g := &HeldGuardrail{hook: hook, Index: i, segment: hook.StreamHold() == hooks.StreamHoldSegment}
		if !g.segment {
			h.Judging = false
		}
		h.Holders = append(h.Holders, g)
	}
	h.parent = ctx
	h.Ctx, h.cancel = context.WithCancel(ctx)
	h.Results = make(chan PartialVerdict, len(holders))
	return h
}

// InFlight reports whether a judgment is running.
func (h *StreamHold) InFlight() bool {
	for _, g := range h.Holders {
		if g.Busy {
			return true
		}
	}
	return false
}

// Stop cancels the judgments still running, reporting how many there were.
func (h *StreamHold) Stop() int {
	n := 0
	for _, g := range h.Holders {
		if g.Busy {
			n++
		}
	}
	h.cancel()
	return n
}

// restart discards the round's judging when a retry restarts the reply.
func (h *StreamHold) restart() {
	h.cancel()
	h.Ctx, h.cancel = context.WithCancel(h.parent)
	h.Results = make(chan PartialVerdict, len(h.Holders))
	h.seg.Reset()
	h.arrivals = nil
	for _, g := range h.Holders {
		g.Busy, g.calls, g.judged = false, 0, 0
	}
}

// received notes that the reply's text up to end arrived now.
func (h *StreamHold) received(end int) {
	if n := len(h.arrivals); n == 0 || h.arrivals[n-1].end < end {
		h.arrivals = append(h.arrivals, textArrival{end: end, at: time.Now()})
	}
}

// heldSince is when the byte at offset arrived.
func (h *StreamHold) heldSince(offset int) time.Time {
	for _, a := range h.arrivals {
		if offset < a.end {
			return a.at
		}
	}
	return time.Now()
}

// SegmentAndJudge takes delta, the text a chunk adds to the reply, finds the
// segments it completes, and starts a judgment for each idle segment
// guardrail with a segment it has not judged.
func (h *StreamHold) SegmentAndJudge(delta string) {
	h.seg.Feed(delta)
	if !h.Judging {
		return
	}
	for _, g := range h.Holders {
		h.Judge(g)
	}
}

// Judge starts g's judgment of every segment ready since its last one,
// unless it is busy, has none new, or has made its round's last judgment.
func (h *StreamHold) Judge(g *HeldGuardrail) {
	bounds := h.seg.Segments()
	n := len(bounds)
	if !g.segment || g.Busy || n == g.judged || g.calls >= h.cfg.MaxSegmentJudges {
		return
	}
	if coalesced := n - g.judged - 1; coalesced > 0 {
		logger.Debug("stream_hold: segments joined into one judgment",
			"guardrail", g.hook.(hooks.ProviderHook).Name(), "segments", coalesced+1)
	}
	g.Busy = true
	g.calls++
	g.judged = n
	if g.calls == h.cfg.MaxSegmentJudges {
		logger.Debug("stream_hold: last partial-reply judgment this round; the rest is judged with the reply",
			"guardrail", g.hook.(hooks.ProviderHook).Name(), "judgments", g.calls)
	}
	judged := h.seg.Text()[:bounds[n-1]]
	ctx, results, req := h.Ctx, h.Results, h.Req
	reply := hooks.PartialReply{Text: judged, SegmentIndex: n, Timeout: h.cfg.SegmentTimeout}
	go func() {
		start := time.Now()
		d := g.hook.JudgePartialReply(ctx, req, reply)
		select {
		case results <- PartialVerdict{Guardrail: g, Text: judged, Segment: n, Decision: d, Took: time.Since(start)}:
		case <-ctx.Done():
		}
	}()
}

// MetadataKeyValidatorType is the decision metadata key naming the eval type
// that fired.
const MetadataKeyValidatorType = "validator_type"
