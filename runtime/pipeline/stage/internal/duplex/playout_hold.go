package duplex

import (
	"context"
	"strconv"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	heardreplies "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/heard"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// playoutCueItem is the playout item cue audio is tracked under: never a
// reply's key, so no Message is amended for it.
const playoutCueItem = "cue"

// playoutHoldPoll bounds how long the stage sleeps before rechecking a
// playout end that moved (more audio arrived).
const playoutHoldPoll = 250 * time.Millisecond

// PlayoutHoldStage keeps a realtime reply out of the transcript until the
// caller has heard it. A realtime model sends a reply's audio faster than it
// plays, so the reply completes long before the caller has heard it, and a
// caller who barges in while it plays hears only part of it.
//
// The stage estimates playout from the audio it forwards (audio.Playout, the
// estimate the realtime providers use to cut their own copy of the reply). It
// assumes the audio is played in real time as it arrives, so it belongs only
// in a pipeline that plays to a listener (the SDK wires it for a paced
// speaker or WithPlayoutTracking).
//
// When a reply that has audio completes, its Message is held: the element
// still goes on at once, marked Meta.PlayoutHeld (and stamped with the time it
// was held) so the transcript stages (recording, message broadcast, memory,
// save) skip it, and every Message after it is held too, so the transcript
// keeps its order. When the audio has played out, each held Message is
// released unchanged on a Meta.PlayoutRelease element. When the caller barges
// in (an element marked Meta.Interrupted) or the session ends first (input
// closed or the context canceled), a reply not fully heard is released with
// its full text and FinishReason, plus Meta heard_ms, audio_ms,
// played_fraction and playout_cut. The text is not cut: the providers' audio
// transcripts carry no timing, so what the caller heard cannot be mapped to
// words.
//
// A reply cut while it still arrives (an Interrupt element: a guardrail's cut,
// marked safety, or a barge-in the provider reported as the reply's end) is
// not held: its Message records what was heard before the cut, with
// playout_cut guardrail or barge-in.
//
// A reply without audio, with nothing held ahead of it, passes unchanged.
type PlayoutHoldStage struct {
	core.BaseStage
	clock core.HoldClock
}

// NewPlayoutHoldStage creates a playout hold stage.
func NewPlayoutHoldStage() *PlayoutHoldStage {
	return &PlayoutHoldStage{BaseStage: core.NewBaseStage("playout-hold", core.StageTypeTransform), clock: core.RealHoldClock}
}

// heldMessage is a Message awaiting release; reply is set for a reply whose
// audio is tracked under key.
type heldMessage struct {
	elem  core.StreamElement
	reply bool
	key   int
}

// playoutHold is one Process run's state.
type playoutHold struct {
	clock   core.HoldClock
	playout *audio.Playout
	// seq keys the audio of the reply now arriving; audio is each reply's
	// total.
	seq   int
	audio map[int]time.Duration
	held  []heldMessage
	fwd   core.CancelForwarder
	// cut is the cut of the reply still arriving (an Interrupt before its
	// Message): its Message, when it comes, records it and is not held.
	cut *replyCut
}

// replyCut is what the caller heard of reply key before an Interrupt cut it.
type replyCut struct {
	key   int
	heard time.Duration
	cause string
}

// Process implements the Stage interface. A held Message is never lost: on
// input close or cancellation everything held is released, with what was
// heard, before Process returns, and after cancellation the input is still
// read to its end (the hand-off is bounded, see CancelForwarder).
func (s *PlayoutHoldStage) Process(ctx context.Context, input <-chan core.StreamElement, output chan<- core.StreamElement) error {
	defer close(output)
	h := &playoutHold{clock: s.clock, playout: audio.NewPlayout(s.clock.Now), audio: map[int]time.Duration{}}
	for {
		if len(h.held) > 0 && !h.playout.Playing() {
			h.release(ctx, output, nil, "")
		}
		var wake <-chan time.Time
		if len(h.held) > 0 {
			now := h.clock.Now()
			wake = h.clock.Until(minTime(h.playout.End(), now.Add(playoutHoldPoll)))
		}
		select {
		case elem, ok := <-input:
			if !ok {
				h.release(ctx, output, h.playout.Stop(), types.PlayoutCutSessionClosed)
				return nil
			}
			h.handle(ctx, elem, output)
		case <-wake: // the loop rechecks the playout
		case <-ctx.Done():
			h.release(ctx, output, h.playout.Stop(), types.PlayoutCutSessionClosed)
			for elem := range input {
				h.fwd.Forward(ctx, output, elem)
			}
			return ctx.Err()
		}
	}
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// handle tracks one element's audio, holds its Message when it must wait, and
// forwards it. A barge-in releases what was held with what the caller heard.
func (h *playoutHold) handle(ctx context.Context, elem core.StreamElement, output chan<- core.StreamElement) {
	if a := elem.Audio; a != nil && len(a.Samples) > 0 {
		d := audio.PCM16Duration(len(a.Samples), a.SampleRate, a.Channels)
		if elem.Meta.CueAudio != nil {
			// A cue's audio plays out like a reply's, but it is no reply's.
			h.playout.Add(playoutCueItem, 0, d)
		} else {
			h.playout.Add(strconv.Itoa(h.seq), 0, d)
			h.audio[h.seq] += d
		}
	}
	if h.takeCut(&elem) {
		h.fwd.Forward(ctx, output, elem)
		return
	}
	heldReply := h.hold(&elem)
	h.fwd.Forward(ctx, output, elem)
	switch {
	case elem.Meta.Interrupted:
		// The playback stopped: what was held is released with what was heard.
		h.release(ctx, output, h.playout.Stop(), types.PlayoutCutBargeIn)
		h.endReply()
	case elem.Interrupt:
		// A reply cut before its Message: a guardrail's cut (marked safety),
		// or a barge-in the provider reported as the reply's end.
		h.interrupt(ctx, output, elem)
	case !heldReply && (elem.EndOfStream || elem.Error != nil):
		// A reply that ended without a held message (an error, no content):
		// its audio belongs to no message, and not to the next reply.
		h.endReply()
	}
}

// interrupt handles an Interrupt: the playback stopped, so what was held is
// released with what was heard, and the reply still arriving, if it has
// audio, keeps what was heard of it for its Message (takeCut).
func (h *playoutHold) interrupt(ctx context.Context, output chan<- core.StreamElement, elem core.StreamElement) {
	cause := types.PlayoutCutBargeIn
	if fr := elem.Meta.FinishReason; fr != nil && *fr == types.FinishReasonSafety {
		cause = types.PlayoutCutGuardrail
	}
	cuts := h.playout.Stop()
	h.release(ctx, output, cuts, cause)
	total := h.audio[h.seq]
	if total == 0 {
		return
	}
	heard := total
	for _, c := range cuts {
		if c.ItemID == strconv.Itoa(h.seq) {
			heard = c.Heard
		}
	}
	h.cut = &replyCut{key: h.seq, heard: heard, cause: cause}
}

// takeCut records a cut on the Message of the reply it cut, which is not
// held: it no longer plays. It reports whether elem was that reply.
func (h *playoutHold) takeCut(elem *core.StreamElement) bool {
	msg := elem.Message
	if h.cut == nil || h.cut.key != h.seq || msg == nil || msg.Role != core.RoleAssistant ||
		!(elem.EndOfStream || elem.Meta.Interrupted) {
		return false
	}
	elem.Message = heardreplies.CutReply(msg, h.cut.heard, h.audio[h.seq], h.cut.cause)
	h.cut = nil
	h.endReply()
	return true
}

// endReply drops the current reply's audio accounting and starts the next.
func (h *playoutHold) endReply() {
	delete(h.audio, h.seq)
	h.seq++
}

// hold holds elem's Message, marking and stamping elem, when it is a reply
// with audio or anything is already held. It reports whether elem was held as
// a reply.
func (h *playoutHold) hold(elem *core.StreamElement) bool {
	msg := elem.Message
	if msg == nil || elem.Meta.PlayoutRelease {
		return false
	}
	reply := msg.Role == core.RoleAssistant && (elem.EndOfStream || elem.Meta.Interrupted) && h.audio[h.seq] > 0
	if !reply && len(h.held) == 0 {
		return false
	}
	if elem.Timestamp.IsZero() {
		elem.Timestamp = h.clock.Now() // its place in the recording is now, not its release
	}
	h.held = append(h.held, heldMessage{elem: *elem, reply: reply, key: h.seq})
	if reply {
		h.seq++
		h.playout.NewResponse() // forget earlier replies already played
	}
	elem.Meta.PlayoutHeld = true
	return reply
}

// release emits every held Message in order. A reply among cuts (the
// playout's unheard items) is amended with what was heard and why.
func (h *playoutHold) release(ctx context.Context, output chan<- core.StreamElement, cuts []audio.PlayoutCut, cause string) {
	heard := make(map[string]time.Duration, len(cuts))
	for _, c := range cuts {
		heard[c.ItemID] = c.Heard
	}
	for i := range h.held {
		m := h.held[i]
		msg := m.elem.Message
		if got, cut := heard[strconv.Itoa(m.key)]; m.reply && cut {
			msg = heardreplies.CutReply(msg, got, h.audio[m.key], cause)
		}
		// Not a turn end: the held element already carried this reply's, to
		// every stage downstream, when it happened.
		out := core.StreamElement{Message: msg, Timestamp: m.elem.Timestamp}
		out.Meta.PlayoutRelease = true
		out.Meta.PlayoutTurnEnd = m.elem.EndOfStream || m.elem.Meta.Interrupted
		// A late caller transcript stays marked as the turn it answers, and a
		// reply keeps the caller turn it answers.
		out.Meta.LateTranscript = m.elem.Meta.LateTranscript
		out.Meta.CallerTurn = m.elem.Meta.CallerTurn
		out.Meta.AwaitsTranscript = m.elem.Meta.AwaitsTranscript
		h.fwd.Forward(ctx, output, out)
		if m.reply {
			delete(h.audio, m.key)
		}
	}
	h.held = nil
}
