package core

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TurnFailureSignal is the error of the element that tells a downstream stage
// an upstream stage failed the turn. It reads and unwraps as the failure itself
// (a *StageError); its type is what marks the element as the signal, apart
// from error elements stages emit as non-fatal reports.
type TurnFailureSignal struct{ err error }

func (e *TurnFailureSignal) Error() string { return e.err.Error() }
func (e *TurnFailureSignal) Unwrap() error { return e.err }

// NewTurnFailureSignal is the element that tells downstream stages an upstream
// stage failed the turn with err.
func NewTurnFailureSignal(err error) StreamElement {
	return NewErrorElement(&TurnFailureSignal{err: err})
}

// IsTurnFailureSignal reports whether elem tells that an upstream stage failed
// the turn. A stage acting on its input's end (calling a model, say) must not
// act on a turn that failed.
func IsTurnFailureSignal(elem *StreamElement) bool {
	var sig *TurnFailureSignal
	return elem.Error != nil && errors.As(elem.Error, &sig)
}

// TagErrorSource records stage as the source of elem's error when nothing has
// yet: the first stage whose output carried the error put it there.
func TagErrorSource(elem *StreamElement, stage string) {
	if elem.Error == nil || ErrorSource(elem) != "" {
		return
	}
	elem.errorStage = stage
	elem.errorTagged = elem.Error
}

// ErrorSource is the stage that put elem's error on the stream, or "" when
// that is not known: never tagged, or the error was replaced after tagging.
func ErrorSource(elem *StreamElement) string {
	if elem.errorStage == "" || !sameErrorValue(elem.errorTagged, elem.Error) {
		return ""
	}
	return elem.errorStage
}

// sameErrorValue reports whether a and b are the same error value. Unlike ==
// it does not panic on an error whose dynamic type is not comparable; such an
// error is never the same value as another.
func sameErrorValue(a, b error) bool {
	if a == nil || b == nil {
		return a == b
	}
	t := reflect.TypeOf(a)
	if t != reflect.TypeOf(b) || !t.Comparable() {
		return false
	}
	return a == b
}

// ForwardUpstreamErrors passes a failed turn's error elements downstream.
func ForwardUpstreamErrors(ctx context.Context, elems []StreamElement, output chan<- StreamElement) error {
	for i := range elems {
		select {
		case output <- elems[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// SendStreamElement forwards one element downstream, honoring cancellation.
func SendStreamElement(ctx context.Context, elem StreamElement, output chan<- StreamElement) error {
	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsCut reports whether elem cuts the reply being played.
func IsCut(elem *StreamElement) bool {
	return elem.Interrupt || elem.Meta.Interrupted
}

// HoldClock is the time source of the stages that wait for audio to play
// out; tests replace it.
type HoldClock struct {
	Now func() time.Time
	// Until delivers on the channel once the clock reaches deadline (at once
	// if it already has). It takes a time, not a duration, so time that moves
	// between the stage reading the clock and arming the wait is not lost.
	Until func(deadline time.Time) <-chan time.Time
}

// RealHoldClock is the wall clock.
var RealHoldClock = HoldClock{Now: time.Now, Until: func(deadline time.Time) <-chan time.Time {
	return time.After(time.Until(deadline))
}}

// StoreCallerTurn records caller turn index on reply m (types.MetaCallerTurn),
// for the transcript, on a Meta of its own. Nil-safe.
func StoreCallerTurn(m *types.Message, index int) {
	if m == nil || m.Role != RoleAssistant {
		return
	}
	meta := make(map[string]interface{}, len(m.Meta)+1)
	for k, v := range m.Meta {
		meta[k] = v
	}
	meta[types.MetaCallerTurn] = index
	m.Meta = meta
}
