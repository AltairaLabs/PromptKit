package stage

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Message.Meta keys PlayoutHoldStage sets on a reply the caller did not hear
// in full.
const (
	// MetaHeardMs is how much of the reply's audio the caller heard, in
	// milliseconds (int64).
	MetaHeardMs = "heard_ms"
	// MetaAudioMs is the reply's audio duration, in milliseconds (int64).
	MetaAudioMs = "audio_ms"
	// MetaPlayedFraction is MetaHeardMs over MetaAudioMs, from 0 to 1
	// (float64, three decimals).
	MetaPlayedFraction = "played_fraction"
)

// Interruption causes (types.MetaInterruptedCause) for a reply cut during
// playout.
const (
	// InterruptedCausePlayout: the caller barged in while the reply played.
	InterruptedCausePlayout = "barge-in-playout"
	// InterruptedCauseSessionClosed: the session ended while it played.
	InterruptedCauseSessionClosed = "session-closed"
)

// playoutHoldPoll bounds how long the stage sleeps before rechecking a
// playout end that moved (more audio arrived).
const playoutHoldPoll = 250 * time.Millisecond

// PlayoutHoldStage keeps a realtime reply out of the transcript until the
// caller has heard it. A realtime model sends a reply's audio faster than it
// plays, so the reply completes long before the caller has heard it, and a
// caller who barges in while it plays hears only part of it.
//
// The stage estimates playout from the audio it forwards (audio.Playout, the
// estimate the realtime providers use to cut their own copy of the reply).
// When a reply that has audio completes, its Message is held: the element
// still goes on at once, marked Meta.PlayoutHeld so the transcript stages
// (recording, message broadcast, memory, save) skip it, and every Message
// after it is held too, so the transcript keeps its order. When the audio has
// played out, each held Message is released unchanged on a
// Meta.PlayoutRelease element. When the caller barges in (an element marked
// Meta.Interrupted) or the session ends first, a reply not fully heard is
// released with its full text, FinishReason interrupted and Meta heard_ms,
// audio_ms and played_fraction. The text is not cut: the providers' audio
// transcripts carry no timing, so what the caller heard cannot be mapped to
// words.
//
// A reply without audio, with nothing held ahead of it, passes unchanged.
type PlayoutHoldStage struct {
	BaseStage
}

// NewPlayoutHoldStage creates a playout hold stage.
func NewPlayoutHoldStage() *PlayoutHoldStage {
	return &PlayoutHoldStage{BaseStage: NewBaseStage("playout-hold", StageTypeTransform)}
}

// heldMessage is a Message awaiting release; reply is set for a reply whose
// audio is tracked under key.
type heldMessage struct {
	elem  StreamElement
	reply bool
	key   int
}

// playoutHold is one Process run's state.
type playoutHold struct {
	playout *audio.Playout
	// seq keys the audio of the reply now arriving; audio is each key's total.
	seq   int
	audio map[int]time.Duration
	held  []heldMessage
}

// Process implements the Stage interface.
func (s *PlayoutHoldStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)
	h := &playoutHold{playout: audio.NewPlayout(nil), audio: map[int]time.Duration{}}
	var wake *time.Timer
	defer func() {
		if wake != nil {
			wake.Stop()
		}
	}()
	for {
		var wakeC <-chan time.Time
		if len(h.held) > 0 {
			wake = resetTimer(wake, min(time.Until(h.playout.End()), playoutHoldPoll))
			wakeC = wake.C
		}
		select {
		case elem, ok := <-input:
			if !ok {
				return h.release(ctx, output, h.playout.Stop(), InterruptedCauseSessionClosed)
			}
			if err := h.handle(ctx, elem, output); err != nil {
				return err
			}
		case <-wakeC:
			if !h.playout.Playing() {
				if err := h.release(ctx, output, nil, ""); err != nil {
					return err
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// resetTimer (re)arms t for d (at least 1ms), creating it if nil.
func resetTimer(t *time.Timer, d time.Duration) *time.Timer {
	d = max(d, time.Millisecond)
	if t == nil {
		return time.NewTimer(d)
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
	return t
}

// handle tracks one element's audio, holds its Message when it must wait, and
// forwards it. A barge-in releases what was held with what the caller heard.
func (h *playoutHold) handle(ctx context.Context, elem StreamElement, output chan<- StreamElement) error {
	if a := elem.Audio; a != nil && len(a.Samples) > 0 {
		d := audio.PCM16Duration(len(a.Samples), a.SampleRate, a.Channels)
		h.playout.Add(strconv.Itoa(h.seq), 0, d)
		h.audio[h.seq] += d
	}
	h.hold(&elem)
	if err := sendStreamElement(ctx, elem, output); err != nil {
		return err
	}
	if !elem.Meta.Interrupted {
		return nil
	}
	// The playback stopped: what was held is released with what was heard,
	// and audio still tracked for a reply cut short belongs to no message.
	err := h.release(ctx, output, h.playout.Stop(), InterruptedCausePlayout)
	h.audio = map[int]time.Duration{}
	h.seq++
	return err
}

// hold holds elem's Message, marking elem, when it is a reply with audio or
// anything is already held.
func (h *playoutHold) hold(elem *StreamElement) {
	msg := elem.Message
	if msg == nil || elem.Meta.PlayoutRelease {
		return
	}
	reply := msg.Role == roleAssistant && (elem.EndOfStream || elem.Meta.Interrupted) && h.audio[h.seq] > 0
	if !reply && len(h.held) == 0 {
		return
	}
	held := heldMessage{elem: *elem, reply: reply, key: h.seq}
	if reply {
		h.seq++
	}
	h.held = append(h.held, held)
	elem.Meta.PlayoutHeld = true
}

// release emits every held Message in order. A reply among cuts (the
// playout's unheard items) is amended with what was heard and cause.
func (h *playoutHold) release(
	ctx context.Context, output chan<- StreamElement, cuts []audio.PlayoutCut, cause string,
) error {
	heard := make(map[string]time.Duration, len(cuts))
	for _, c := range cuts {
		heard[c.ItemID] = c.Heard
	}
	for i := range h.held {
		m := h.held[i]
		msg := m.elem.Message
		if got, cut := heard[strconv.Itoa(m.key)]; m.reply && cut {
			msg = heardReply(msg, got, h.audio[m.key], cause)
		}
		out := StreamElement{Message: msg, EndOfStream: m.elem.EndOfStream, Timestamp: m.elem.Timestamp}
		out.Meta.PlayoutRelease = true
		if err := sendStreamElement(ctx, out, output); err != nil {
			return err
		}
		if m.reply {
			delete(h.audio, m.key)
		}
	}
	h.held = nil
	return nil
}

// heardReply is a copy of msg marked as interrupted after heard of its total
// audio. The text is kept whole.
func heardReply(msg *types.Message, heard, total time.Duration, cause string) *types.Message {
	c := *msg
	c.Meta = make(map[string]interface{}, len(msg.Meta)+5)
	for k, v := range msg.Meta {
		c.Meta[k] = v
	}
	c.FinishReason = types.FinishReasonInterrupted
	c.Meta[finishReasonMetaKey] = finishReasonInterrupted
	c.Meta[types.MetaInterruptedCause] = cause
	c.Meta[MetaHeardMs] = heard.Milliseconds()
	c.Meta[MetaAudioMs] = total.Milliseconds()
	fraction := 0.0
	if total > 0 {
		fraction = math.Round(float64(heard)/float64(total)*1000) / 1000
	}
	c.Meta[MetaPlayedFraction] = fraction
	return &c
}
