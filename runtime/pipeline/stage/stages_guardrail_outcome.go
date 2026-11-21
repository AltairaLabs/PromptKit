package stage

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// guardrailOutcome decides what guardrails firing on a turn mean, for the text
// stage (ProviderStage) and the realtime stage (DuplexProviderStage) alike.
// For output, on a streamed chunk (the chunk interceptors) and on a completed
// reply (AfterCall), it runs the hooks, records every firing, chooses the text
// that blocks the turn once, and returns a verdict; the stages only apply the
// verdict to their own output. For input, whose hooks each stage runs where
// its input arrives, it chooses a blocked turn's message (blockedMessage) and
// records the firing (record) the same way.
type guardrailOutcome struct {
	registry *hooks.Registry
	emitter  *events.Emitter
	caller   types.Caller
	// turn is the 0-based turn the firings are reported against.
	turn int
	// choose is the text a firing in direction blocks the turn with, given
	// the text the guardrail chose: the stage's BlockedMessageFunc's choice
	// (chooseBlockedText), recorded on the decision.
	choose func(ctx context.Context, d *hooks.Decision, direction, text string) string
}

// blockedMessage is the assistant turn a firing in direction blocks a turn
// with (blockedTurnMessage), its text the stage's choice (choose), which is
// recorded on d.
func (g guardrailOutcome) blockedMessage(
	ctx context.Context, req *hooks.ProviderRequest, d *hooks.Decision, direction string,
) types.Message {
	msg := blockedTurnMessage(req, *d)
	msg.Content = g.choose(ctx, d, direction, msg.Content)
	return msg
}

// record reports a firing in direction, which took dur: its validation.failed
// event, and its validation record appended to vs, when vs is not nil.
func (g guardrailOutcome) record(vs *[]types.ValidationResult, d hooks.Decision, direction string, dur time.Duration) {
	recordGuardrailFiring(g.emitter, nil, d, direction, dur, g.turn)
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

// outputVerdict is a guardrailOutcome's decision on a reply.
type outputVerdict struct {
	kind verdictKind
	// holds, on an allowed chunk, is each chunk interceptor's hold: how many
	// bytes at the end of the reply so far it has not cleared.
	holds []int
	// text is the reply's text from here on, when it blocks: a truncation's
	// prefix, or the message that replaces the reply.
	text string
	// decision is the firing whose text stands, with the blocked message it
	// chose recorded in its metadata.
	decision hooks.Decision
	// firings are every hook that did not allow the reply, in order.
	firings []hooks.Decision
	// validations are the firings' validation records, plus any an AfterCall
	// hook added to the reply it was given.
	validations []types.ValidationResult
}

// blocks reports whether the verdict ends the reply.
func (v *outputVerdict) blocks() bool { return v.kind != verdictAllow }

// replaces reports whether the reply's text is replaced with a guardrail's,
// rather than truncated: the text is not the model's, so it is not judged
// again, and a caller showing the streamed text swaps it for this.
func (v *outputVerdict) replaces() bool { return v.kind == verdictReplace || v.kind == verdictDeny }

// enforce makes msg the reply the verdict leaves, as a blocked reply is on
// every path: its text, tool calls dropped (an assistant message carrying
// calls with no results is a protocol error on the next call, and a blocked
// reply's calls must not run), its reasoning dropped (the model's thinking
// toward what was blocked can hold it), finish reason safety, and the
// firing's metadata. The validations are the caller's to fold.
func (v *outputVerdict) enforce(msg *types.Message) {
	msg.Content = v.text
	msg.ToolCalls = nil
	msg.Reasoning = nil
	msg.FinishReason = types.FinishReasonSafety
	if meta := decisionMessageMeta(v.decision); meta != nil {
		msg.Meta = meta
	}
}

// judgeChunk runs the chunk interceptors on a streamed chunk, whose Content is
// the reply so far. The chunk is not changed.
func (g guardrailOutcome) judgeChunk(ctx context.Context, chunk *providers.StreamChunk) outputVerdict {
	if !g.registry.HasChunkInterceptors() {
		return outputVerdict{}
	}
	start := time.Now()
	j := g.registry.JudgeChunk(ctx, hooks.ChunkRequest{Chunk: chunk, Caller: g.caller, TurnIndex: g.turn})
	if len(j.Firings) == 0 {
		return outputVerdict{holds: j.Holds}
	}
	return g.verdict(ctx, nil, chunk.Content, j.Firings, time.Since(start), false)
}

// judgeResponse runs the AfterCall hooks on a completed reply, msg, for the
// call req describes, which took latencyMs. msg is not changed: a hook is
// given a copy. denyIsError is set by a stage that fails the turn on a deny
// (HookDeniedError) rather than replacing the reply: no blocked message is
// chosen for it.
func (g guardrailOutcome) judgeResponse(
	ctx context.Context, req *hooks.ProviderRequest, msg *types.Message, round int, latencyMs int64, denyIsError bool,
) outputVerdict {
	if !g.registry.HasProviderHooks() {
		return outputVerdict{}
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
	d := g.registry.RunAfterProviderCall(ctx, req, resp)
	if d.Allow {
		return outputVerdict{}
	}
	v := g.verdict(ctx, req, msg.Content, []hooks.Decision{d}, time.Since(start), denyIsError)
	v.validations = append(v.validations, addedValidations(resp.Message.Validations, known)...)
	return v
}

// verdict turns firings on a reply whose text was original into a verdict.
// The last firing stands: the deny that stopped the chain, or the last
// enforcing hook, whose text the others judged. Its text is the reply's; a
// replacement's or a deny's is the stage's choice (choose), made once, except
// for a deny the stage returns as an error (denyIsError), which has no text. Every
// firing is recorded: a validation.failed event each, and a validation record
// each, so a truncation a later guardrail replaced still shows.
func (g guardrailOutcome) verdict(
	ctx context.Context, req *hooks.ProviderRequest, original string, firings []hooks.Decision,
	dur time.Duration, denyIsError bool,
) outputVerdict {
	last := len(firings) - 1
	d := firings[last]
	v := outputVerdict{}
	content := original
	if d.Content != nil {
		content = *d.Content
	}
	switch {
	case !d.Enforced:
		v.kind = verdictDeny
		if !denyIsError {
			v.text = g.choose(ctx, &d, hooks.DirectionOutput, blockedTurnMessage(req, d).Content)
		}
	case replacedText(original, content):
		v.kind = verdictReplace
		v.text = g.choose(ctx, &d, hooks.DirectionOutput, content)
	default:
		v.kind = verdictTruncate
		v.text = content
	}
	firings[last] = d
	v.decision = d
	v.firings = firings
	for _, f := range firings {
		g.record(&v.validations, f, hooks.DirectionOutput, dur)
	}
	return v
}

// holdController is the reply text a streaming reply has received but not yet
// released to the caller, because a guardrail has not cleared it, and the
// media that arrived after it.
//
// Each holding guardrail reports a cleared offset into the reply's text: how
// far it has judged the text safe to release. The controller releases up to
// the least of them. A tail guardrail (hooks.Decision.Hold, a banned word's
// possible start) has cleared all it received but its hold (clearTails). With
// no guardrail holding, everything received is released at once.
type holdController struct {
	sent    strings.Builder // released so far
	pending string          // received, not yet released
	// cleared is each holding guardrail's cleared offset.
	cleared []int
	media   []StreamElement // media that arrived after pending text
}

// add takes delta, the text a chunk adds to the reply.
func (h *holdController) add(delta string) { h.pending += delta }

// received is the length of the reply's text so far.
func (h *holdController) received() int { return h.sent.Len() + len(h.pending) }

// clearTails records the chunk interceptors' report on the text received so
// far: interceptor i has cleared all of it but its last holds[i] bytes. It
// replaces their previous report.
func (h *holdController) clearTails(holds []int) {
	h.cleared = h.cleared[:0]
	for _, n := range holds {
		h.cleared = append(h.cleared, h.received()-n)
	}
}

// clearAll records that nothing is held any more: the reply has ended.
func (h *holdController) clearAll() { h.cleared = h.cleared[:0] }

// release returns the text every guardrail has cleared that was not released
// before, and records it as released.
func (h *holdController) release() string {
	upto := h.received()
	for _, c := range h.cleared {
		upto = min(upto, c)
	}
	n := min(max(upto-h.sent.Len(), 0), len(h.pending))
	out := h.pending[:n]
	h.pending = h.pending[n:]
	h.sent.WriteString(out)
	return out
}

// emitMedia emits the media a chunk carries after the chunk's text: at once
// when none of its text is held, else once the text is released.
func (h *holdController) emitMedia(ctx context.Context, chunk *providers.StreamChunk, output chan<- StreamElement) error {
	if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
		h.media = append(h.media, StreamMediaToElement(chunk.MediaData))
	}
	if h.pending != "" {
		return nil
	}
	return h.sendMedia(ctx, output)
}

// sendMedia emits the media waiting behind held text.
func (h *holdController) sendMedia(ctx context.Context, output chan<- StreamElement) error {
	for len(h.media) > 0 {
		if err := sendStreamElement(ctx, h.media[0], output); err != nil {
			return err
		}
		h.media = h.media[1:]
	}
	return nil
}

// withoutHeld is the round's output less the text still held: a round that
// ends early keeps only the text the caller has had.
func (h *holdController) withoutHeld(acc *streamAccumulator) streamedRound {
	got := acc.collected()
	got.content = strings.TrimSuffix(got.content, h.pending)
	return got
}

// reset discards the reply: a retry restarts it, or a block or a barge-in
// ended it.
func (h *holdController) reset() {
	h.sent.Reset()
	h.pending = ""
	h.cleared = h.cleared[:0]
	h.media = nil
}
