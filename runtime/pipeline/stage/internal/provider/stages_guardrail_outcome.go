package provider

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// GuardrailOutcome decides what guardrails firing on a turn mean, for the text
// stage (ProviderStage) and the realtime stage (DuplexProviderStage) alike.
// For output, on a streamed chunk (the chunk interceptors) and on a completed
// reply (AfterCall), it runs the hooks, records every firing, chooses the text
// that blocks the turn once, and returns a verdict; the stages only apply the
// verdict to their own output. For input, whose hooks each stage runs where
// its input arrives, it chooses a blocked turn's message (BlockedMessage) and
// records the firing (record) the same way.
type GuardrailOutcome struct {
	Registry *hooks.Registry
	Emitter  *events.Emitter
	Caller   types.Caller
	// Turn is the 0-based Turn the firings are reported against.
	Turn int
	// Choose is the text a firing in direction blocks the turn with, given
	// the text the guardrail chose: the stage's BlockedMessageFunc's choice
	// (ChooseBlockedText), recorded on the decision.
	Choose func(ctx context.Context, d *hooks.Decision, direction, text string) string
}

// BlockedMessage is the assistant turn a firing in direction blocks a turn
// with (BlockedTurnMessage), its text the stage's choice (Choose), which is
// recorded on d.
func (g GuardrailOutcome) BlockedMessage(
	ctx context.Context, req *hooks.ProviderRequest, d *hooks.Decision, direction string,
) types.Message {
	msg := BlockedTurnMessage(req, *d)
	msg.Content = g.Choose(ctx, d, direction, msg.Content)
	return msg
}

// Record reports a firing in direction, which took dur: its validation.failed
// event, and its validation Record appended to vs, when vs is not nil.
func (g GuardrailOutcome) Record(vs *[]types.ValidationResult, d hooks.Decision, direction string, dur time.Duration) {
	recordGuardrailFiring(g.Emitter, nil, d, direction, dur, g.Turn)
	if vs == nil {
		return
	}
	if v, ok := guardrailValidation(d, direction); ok {
		*vs = append(*vs, v)
	}
}

// verdictKind is what an output verdict does to the reply.
type verdictKind uint8

const (
	// verdictAllow lets the reply through, holding back what is not cleared.
	verdictAllow verdictKind = iota
	// verdictTruncate keeps a prefix of the model's own text and ends it.
	verdictTruncate
	// verdictReplace replaces the reply with a guardrail's own text.
	verdictReplace
	// verdictDeny is a hook that blocked the reply without enforcing: the
	// reply is replaced with the blocked message.
	verdictDeny
)

// OutputVerdict is a GuardrailOutcome's decision on a reply.
type OutputVerdict struct {
	kind verdictKind
	// Holds, on an allowed chunk, is each chunk interceptor's hold: how many
	// bytes at the end of the reply so far it has not cleared.
	Holds []int
	// Text is the reply's Text from here on, when it blocks: a truncation's
	// prefix, or the message that replaces the reply.
	Text string
	// Decision is the firing whose text stands, with the blocked message it
	// chose recorded in its metadata.
	Decision hooks.Decision
	// Firings are every hook that did not allow the reply, in order.
	Firings []hooks.Decision
	// Validations are the firings' validation records, plus any an AfterCall
	// hook added to the reply it was given.
	Validations []types.ValidationResult
}

// Blocks reports whether the verdict ends the reply.
func (v *OutputVerdict) Blocks() bool { return v.kind != verdictAllow }

// Replaces reports whether the reply's text is replaced with a guardrail's,
// rather than truncated: the text is not the model's, so it is not judged
// again, and a caller showing the streamed text swaps it for this.
func (v *OutputVerdict) Replaces() bool { return v.kind == verdictReplace || v.kind == verdictDeny }

// Enforce makes msg the reply the verdict leaves, as a blocked reply is on
// every path: its text, tool calls dropped (an assistant message carrying
// calls with no results is a protocol error on the next call, and a blocked
// reply's calls must not run), its reasoning dropped (the model's thinking
// toward what was blocked can hold it), finish reason safety, and the
// firing's metadata, with the message's stable id kept. The validations are
// the caller's to fold.
func (v *OutputVerdict) Enforce(msg *types.Message) {
	msg.Content = v.Text
	msg.ToolCalls = nil
	msg.Reasoning = nil
	msg.FinishReason = types.FinishReasonSafety
	if meta := DecisionMessageMeta(v.Decision); meta != nil {
		// The message keeps its stable id: it was streamed under it.
		id := msg.ID()
		msg.Meta = meta
		withStableID(msg, id)
	}
}

// JudgeChunk runs the chunk interceptors on a streamed chunk, whose Content is
// the reply so far. The chunk is not changed.
func (g GuardrailOutcome) JudgeChunk(ctx context.Context, chunk *providers.StreamChunk) OutputVerdict {
	if !g.Registry.HasChunkInterceptors() {
		return OutputVerdict{}
	}
	start := time.Now()
	j := g.Registry.JudgeChunk(ctx, hooks.ChunkRequest{Chunk: chunk, Caller: g.Caller, TurnIndex: g.Turn})
	if len(j.Firings) == 0 {
		return OutputVerdict{Holds: j.Holds}
	}
	return g.Verdict(ctx, nil, chunk.Content, j.Firings, time.Since(start), false)
}

// JudgeResponse runs the AfterCall hooks on a completed reply, msg, for the
// call req describes, which took latencyMs. msg is not changed: a hook is
// given a copy. denyIsError is set by a stage that fails the turn on a deny
// (HookDeniedError) rather than replacing the reply: no blocked message is
// chosen for it.
func (g GuardrailOutcome) JudgeResponse(
	ctx context.Context, req *hooks.ProviderRequest, msg *types.Message, round int, latencyMs int64, denyIsError bool,
) OutputVerdict {
	if !g.Registry.HasProviderHooks() {
		return OutputVerdict{}
	}
	resp := &hooks.ProviderResponse{
		ProviderID: req.ProviderID,
		Model:      req.Model,
		Message:    *msg,
		Round:      round,
		LatencyMs:  latencyMs,
	}
	// The hook's copy starts with the message's validations; only what a hook
	// adds after them is new. It gets its own slice, so a hook's append cannot
	// write into the message's.
	resp.Message.Validations = slices.Clone(msg.Validations)
	known := len(msg.Validations)

	start := time.Now()
	d := g.Registry.RunAfterProviderCall(ctx, req, resp)
	if d.Allow {
		return OutputVerdict{}
	}
	v := g.Verdict(ctx, req, msg.Content, []hooks.Decision{d}, time.Since(start), denyIsError)
	v.Validations = append(v.Validations, addedValidations(resp.Message.Validations, known)...)
	return v
}

// Verdict turns firings on a reply whose text was original into a Verdict.
// The last firing stands: the deny that stopped the chain, or the last
// enforcing hook, whose text the others judged. Its text is the reply's; a
// replacement's or a deny's is the stage's choice (choose), made once, except
// for a deny the stage returns as an error (denyIsError), which has no text. Every
// firing is recorded: a validation.failed event each, and a validation record
// each, so a truncation a later guardrail replaced still shows.
func (g GuardrailOutcome) Verdict(
	ctx context.Context, req *hooks.ProviderRequest, original string, firings []hooks.Decision,
	dur time.Duration, denyIsError bool,
) OutputVerdict {
	last := len(firings) - 1
	d := firings[last]
	v := OutputVerdict{}
	content := original
	if d.Content != nil {
		content = *d.Content
	}
	switch {
	case !d.Enforced:
		v.kind = verdictDeny
		if !denyIsError {
			v.Text = g.Choose(ctx, &d, hooks.DirectionOutput, BlockedTurnMessage(req, d).Content)
		}
	case replacedText(original, content):
		v.kind = verdictReplace
		v.Text = g.Choose(ctx, &d, hooks.DirectionOutput, content)
	default:
		v.kind = verdictTruncate
		v.Text = content
	}
	firings[last] = d
	v.Decision = d
	v.Firings = firings
	for _, f := range firings {
		g.Record(&v.Validations, f, hooks.DirectionOutput, dur)
	}
	return v
}

// HoldController is the reply text a streaming reply has received but not yet
// released to the caller, because a guardrail has not cleared it, and the
// media that arrived after it.
//
// Each holding guardrail reports a cleared offset into the reply's text: how
// far it has judged the text safe to release. The controller releases up to
// the least of them. A tail guardrail (hooks.Decision.Hold, a banned word's
// possible start) has cleared all it received but its hold (clearTails). A
// stream_hold guardrail (hooks.StreamHolder) has cleared up to the end of the
// last segment it passed (segment), or nothing until the completed reply
// passes (turn) (clearHeld). With no guardrail holding, everything received is
// released at once.
type HoldController struct {
	sent    strings.Builder // released so far
	Pending string          // received, not yet released
	// cleared is each chunk interceptor's cleared offset.
	cleared []int
	// held is each stream_hold guardrail's cleared offset.
	held  []int
	media []core.StreamElement // media that arrived after pending text
}

// Add takes delta, the text a chunk adds to the reply.
func (h *HoldController) Add(delta string) { h.Pending += delta }

// received is the length of the reply's text so far.
func (h *HoldController) received() int { return h.sent.Len() + len(h.Pending) }

// ClearTails records the chunk interceptors' report on the text received so
// far: interceptor i has cleared all of it but its last holds[i] bytes. It
// replaces their previous report.
func (h *HoldController) ClearTails(holds []int) {
	h.cleared = h.cleared[:0]
	for _, n := range holds {
		h.cleared = append(h.cleared, h.received()-n)
	}
}

// ClearAll records that the chunk interceptors hold nothing any more: the
// reply has ended, so nothing can complete what they held. A stream_hold
// guardrail's hold stands until it judged the completed reply (releaseHeld).
func (h *HoldController) ClearAll() { h.cleared = h.cleared[:0] }

// holdFor starts n stream_hold guardrails' holds, none of the reply cleared.
func (h *HoldController) holdFor(n int) { h.held = make([]int, n) }

// clearHeld records that stream_hold guardrail i has cleared the reply up to
// offset.
func (h *HoldController) clearHeld(i, offset int) { h.held[i] = max(h.held[i], offset) }

// releaseHeld records that the stream_hold guardrails passed the completed
// reply: they hold nothing any more.
func (h *HoldController) releaseHeld() { h.held = nil }

// text is the reply's text so far, released and held.
func (h *HoldController) text() string { return h.sent.String() + h.Pending }

// Release returns the text every guardrail has cleared that was not released
// before, and records it as released.
func (h *HoldController) Release() string {
	upto := h.received()
	for _, c := range h.cleared {
		upto = min(upto, c)
	}
	for _, c := range h.held {
		upto = min(upto, c)
	}
	n := min(max(upto-h.sent.Len(), 0), len(h.Pending))
	out := h.Pending[:n]
	h.Pending = h.Pending[n:]
	h.sent.WriteString(out)
	return out
}

// emitMedia emits the media a chunk carries after the chunk's text: at once
// when none of its text is held, else once the text is released.
func (h *HoldController) emitMedia(ctx context.Context, chunk *providers.StreamChunk, output chan<- core.StreamElement) error {
	if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
		h.media = append(h.media, StreamMediaToElement(chunk.MediaData))
	}
	if h.Pending != "" {
		return nil
	}
	return h.sendMedia(ctx, output)
}

// sendMedia emits the media waiting behind held text.
func (h *HoldController) sendMedia(ctx context.Context, output chan<- core.StreamElement) error {
	for len(h.media) > 0 {
		if err := core.SendStreamElement(ctx, h.media[0], output); err != nil {
			return err
		}
		h.media = h.media[1:]
	}
	return nil
}

// withoutHeld is the round's output less the text still held: a round that
// ends early keeps only the text the caller has had.
func (h *HoldController) withoutHeld(acc *streamAccumulator) streamedRound {
	got := acc.collected()
	got.content = strings.TrimSuffix(got.content, h.Pending)
	return got
}

// Reset discards the reply: a retry restarts it, or a block or a barge-in
// ended it.
func (h *HoldController) Reset() {
	h.sent.Reset()
	h.Pending = ""
	h.cleared = h.cleared[:0]
	clear(h.held)
	h.media = nil
}
