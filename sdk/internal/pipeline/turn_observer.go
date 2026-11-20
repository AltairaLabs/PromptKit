package pipeline

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
)

// TurnRecord is one reply of a duplex session that has entered the transcript.
type TurnRecord struct {
	// Continues reports that the reply called tools whose results the model
	// answers in a further reply, so the caller's turn is not over yet.
	Continues bool
	// Stored is how many messages the pipeline had stored in the transcript
	// when the turn was reported, the reply's own included (and the caller's
	// late transcript, when the report waited for it): the transcript the
	// session opened on plus Stored is the turn's transcript, even when later
	// messages are stored before the turn is reported.
	Stored int
}

// TurnReporter is told about the replies of a duplex session as they enter
// the transcript (turnObserverStage). Begin is called when the observer
// starts and End when it has reported everything it will, so a caller can
// wait for the last report. Each method runs on the pipeline's goroutine.
type TurnReporter interface {
	Begin()
	TurnRecorded(ctx context.Context, turn TurnRecord)
	End()
}

// DefaultLateTranscriptWait is how long a turn whose caller transcript has not
// arrived yet waits for it before it is reported without it.
const DefaultLateTranscriptWait = 2 * time.Second

// turnObserverStage is a passthrough stage, placed after the state store save
// stage, that reports each reply the save stage has stored (TurnReporter)
// before forwarding it:
//
//   - a realtime reply: the element carrying its assistant Message and ending
//     it (EndOfStream), or ending it cut short by barge-in (Meta.Interrupted);
//   - a realtime reply held until it was heard: its PlayoutRelease, the turn
//     end recorded on it (Meta.PlayoutTurnEnd), not the held element, whose
//     Message the transcript does not take yet;
//   - a cascaded (VAD) turn: its EndOfTurn, which follows the turn's messages.
//
// The save stage stores a turn before it forwards the element that ends it
// (IncrementalSaveConfig.FlushEachTurn), so the transcript holds the turn when
// it is reported.
//
// A reply stored with no caller message since the last turn reported waits,
// at most lateWait, for the caller's transcript: a provider that delivers it
// late (OpenAI Realtime, without reordering) stores it after the reply
// (Meta.LateTranscript). The turn is reported when it arrives, so its
// transcript holds the caller's words, or when the wait ends, another caller
// message or turn arrives, or the input ends.
type turnObserverStage struct {
	stage.BaseStage
	reporter TurnReporter
	lateWait time.Duration
}

func newTurnObserverStage(reporter TurnReporter, lateWait time.Duration) *turnObserverStage {
	return &turnObserverStage{
		BaseStage: stage.NewBaseStage("turn-observer", stage.StageTypeTransform),
		reporter:  reporter,
		lateWait:  lateWait,
	}
}

// turnObservation is one Process run's state.
type turnObservation struct {
	reporter TurnReporter
	lateWait time.Duration
	// stored counts the messages the save stage stored, in order.
	stored int
	// callerSpoke is set once a caller message is stored after the last turn
	// reported.
	callerSpoke bool
	// pending is a turn waiting for its caller's late transcript, until
	// deadline.
	pending  *TurnRecord
	deadline time.Time
}

// Process implements stage.Stage. It reads its input to the end, even once
// ctx is canceled, so no stage before it blocks on a send while it winds down,
// and reports a turn still waiting when the input ends.
func (s *turnObserverStage) Process(ctx context.Context, input <-chan stage.StreamElement,
	output chan<- stage.StreamElement) error {
	defer close(output)
	s.reporter.Begin()
	defer s.reporter.End()
	o := &turnObservation{reporter: s.reporter, lateWait: s.lateWait}
	for {
		elem, ok, waited := o.next(input)
		if waited {
			o.reportPending(ctx)
			continue
		}
		if !ok {
			o.reportPending(ctx)
			return ctx.Err()
		}
		o.observe(ctx, &elem)
		select {
		case output <- elem:
		case <-ctx.Done():
		}
	}
}

// next reads the next input element, or reports waited once the turn waiting
// for its caller's transcript has waited its bound.
func (o *turnObservation) next(input <-chan stage.StreamElement) (elem stage.StreamElement, ok, waited bool) {
	if o.pending == nil {
		elem, ok = <-input
		return elem, ok, false
	}
	timer := time.NewTimer(time.Until(o.deadline))
	defer timer.Stop()
	select {
	case elem, ok = <-input:
		return elem, ok, false
	case <-timer.C:
		return elem, false, true
	}
}

// observe counts what elem stores and reports the turn it completes.
func (o *turnObservation) observe(ctx context.Context, elem *stage.StreamElement) {
	// The save stage before this one stored exactly these, in this order.
	if stage.IsTranscriptMessage(elem) {
		o.stored++
		if elem.Message.Role == "user" {
			o.callerMessage(ctx, elem.Meta.LateTranscript)
		}
	}
	ends, continues := endsRecordedTurn(elem)
	switch {
	case !ends:
	case continues:
		o.reporter.TurnRecorded(ctx, TurnRecord{Continues: true, Stored: o.stored})
	default:
		o.reportPending(ctx)
		if o.callerSpoke || o.lateWait <= 0 {
			o.report(ctx, o.stored)
			return
		}
		o.pending = &TurnRecord{Stored: o.stored}
		o.deadline = time.Now().Add(o.lateWait)
	}
}

// callerMessage handles a stored caller message: the late transcript of the
// turn waiting for it completes that turn; any other is the next turn's.
func (o *turnObservation) callerMessage(ctx context.Context, late bool) {
	if o.pending != nil && late {
		o.pending = nil
		o.report(ctx, o.stored)
		return
	}
	o.reportPending(ctx)
	if !late {
		o.callerSpoke = true
	}
}

// reportPending reports the turn waiting for its caller's transcript, if any,
// without it.
func (o *turnObservation) reportPending(ctx context.Context) {
	if o.pending == nil {
		return
	}
	stored := o.pending.Stored
	o.pending = nil
	o.report(ctx, stored)
}

// report reports a completed turn over the first stored messages.
func (o *turnObservation) report(ctx context.Context, stored int) {
	o.callerSpoke = false
	o.reporter.TurnRecorded(ctx, TurnRecord{Stored: stored})
}

// endsRecordedTurn reports whether elem ends a reply the transcript now holds,
// and whether that reply continues into a tool round.
func endsRecordedTurn(elem *stage.StreamElement) (ends, continues bool) {
	if elem.EndOfTurn {
		return true, false
	}
	msg := elem.Message
	if msg == nil || msg.Role != "assistant" || elem.Meta.PlayoutHeld {
		return false, false
	}
	if elem.Meta.PlayoutRelease {
		ends = elem.Meta.PlayoutTurnEnd
	} else {
		ends = elem.EndOfStream || elem.Meta.Interrupted
	}
	return ends, ends && len(msg.ToolCalls) > 0
}
