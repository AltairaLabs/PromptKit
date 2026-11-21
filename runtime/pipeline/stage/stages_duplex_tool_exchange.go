package stage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// This file keeps the transcript a realtime session leaves valid for the tool
// calls the stage runs: an assistant message with tool calls is followed, in
// the stage's output, by a result for each of its calls before any other
// message, as a text provider requires of a conversation it is given (OpenAI
// and Claude reject a call whose results do not follow it at once).
//
// A round runs while the session goes on, so a message can come before the
// round's results: a user turn the caller speaks or types meanwhile, the reply
// to it, a call message of a later round. Such a message is held, its element
// with it, until the results are out, and is then emitted in the order it
// came. Once a message is held, a turn end or an error element is held behind
// it too, so a consumer never sees a turn boundary before the message it
// follows. Audio, text and reasoning deltas are not held. A call held for
// approval awaits the result the caller gives like any other.
//
// The hooks' history follows the same order: a message's entry is taken out
// while the message is held and put back when it is released.
//
// When the session ends, a round the worker handed over is still recorded, and
// each call left with no result (a round cut short or never run, a call still
// held for approval) is answered with a refusal, so the stored conversation
// never keeps a call without its result. Once the context is canceled, what is
// left is given partialResponseSendTimeout in all to be taken.

// errSessionClosedBeforeRun is the cause recorded for a call the session's end
// left unanswered.
const errSessionClosedBeforeRun = "the session closed before it ran"

// toolExchangeOrder holds the elements that follow a tool call message the
// stage runs until the call's results are out.
type toolExchangeOrder struct {
	mu sync.Mutex
	// awaiting are the calls of the last emitted call message with no result
	// yet, in the order the model made them.
	awaiting []types.MessageToolCall
	// held are the elements held behind them, in the order they came.
	held []heldElement
	// history is the hooks' history, kept in emission order; nil keeps none.
	history *duplexHistory
}

// heldElement is a held element, and whether its message's entry was taken out
// of the hooks' history while it is held.
type heldElement struct {
	elem      StreamElement
	inHistory bool
}

// sendMode is how an element is sent: a recorded element must reach the
// transcript even once ctx is canceled, and is then given until deadline (a
// deadline of its own when nil).
type sendMode struct {
	recorded bool
	deadline *closeDeadline
}

// closeDeadline is one partialResponseSendTimeout shared by everything sent
// with it after the context is canceled; it starts on its first use, and
// once passed every send with it gives up at once.
type closeDeadline struct {
	once sync.Once
	c    chan struct{}
}

func newCloseDeadline() *closeDeadline { return &closeDeadline{} }

func (d *closeDeadline) done() <-chan struct{} {
	d.once.Do(func() {
		d.c = make(chan struct{})
		time.AfterFunc(partialResponseSendTimeout, func() { close(d.c) })
	})
	return d.c
}

// emit sends elem downstream, or holds it (see toolExchangeOrder). It returns
// ctx.Err() when canceled first.
func (s *DuplexProviderStage) emit(ctx context.Context, elem StreamElement, output chan<- StreamElement) error {
	s.stampCallerTurn(&elem)
	return s.exchange.send(ctx, elem, output, sendMode{})
}

// emitRecorded is emit for an element that must reach the transcript even
// once ctx is canceled (a tool round's results, a canceled turn's partial
// reply): it is then given partialResponseSendTimeout to be taken.
func (s *DuplexProviderStage) emitRecorded(ctx context.Context, elem StreamElement, output chan<- StreamElement) error {
	s.stampCallerTurn(&elem)
	return s.exchange.send(ctx, elem, output, sendMode{recorded: true})
}

// holdable reports whether elem keeps its place behind a held message: a
// message, a turn end or an error.
func holdable(elem *StreamElement) bool {
	return elem.Message != nil || elem.EndOfStream || elem.Error != nil
}

// send places elem: a message behind an unanswered call message is held, and
// so is a turn end or an error behind a held message; any other element is
// sent, and a result that answers the last call releases what was held.
func (o *toolExchangeOrder) send(
	ctx context.Context, elem StreamElement, output chan<- StreamElement, mode sendMode,
) error {
	if !holdable(&elem) {
		return sendElement(ctx, elem, output, mode)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.answersLocked(&elem) && (len(o.held) > 0 || (elem.Message != nil && len(o.awaiting) > 0)) {
		o.holdLocked(elem)
		return nil
	}
	if err := sendElement(ctx, elem, output, mode); err != nil {
		return err
	}
	o.trackLocked(&elem)
	return o.releaseLocked(ctx, output, mode)
}

// holdLocked holds elem, taking its message's entry out of the hooks'
// history until it is released.
func (o *toolExchangeOrder) holdLocked(elem StreamElement) {
	h := heldElement{elem: elem}
	if elem.Message != nil && o.history != nil {
		h.inHistory = o.history.remove(elem.Message)
	}
	o.held = append(o.held, h)
}

// answersLocked reports whether elem is a result for an awaited call.
func (o *toolExchangeOrder) answersLocked(elem *StreamElement) bool {
	if elem.Message == nil || elem.Message.ToolResult == nil {
		return false
	}
	id := elem.Message.ToolResult.ID
	for _, tc := range o.awaiting {
		if tc.ID == id {
			return true
		}
	}
	return false
}

// trackLocked records what an emitted element does to the awaited calls: a
// call message the stage runs makes its calls awaited, and a result answers
// one.
func (o *toolExchangeOrder) trackLocked(elem *StreamElement) {
	m := elem.Message
	switch {
	case m == nil:
	case elem.Meta.ToolCallsRunning && len(m.ToolCalls) > 0:
		o.awaiting = append([]types.MessageToolCall(nil), m.ToolCalls...)
	case m.ToolResult != nil:
		o.answerLocked(m.ToolResult.ID)
	}
}

// answerLocked removes the awaited call id.
func (o *toolExchangeOrder) answerLocked(id string) {
	for i, tc := range o.awaiting {
		if tc.ID == id {
			o.awaiting = append(o.awaiting[:i], o.awaiting[i+1:]...)
			return
		}
	}
}

// releaseLocked emits the held elements, in order, while no call awaits
// results: a held call message stops the release until its own results are
// out. An element that cannot be sent stays held, at the front.
func (o *toolExchangeOrder) releaseLocked(ctx context.Context, output chan<- StreamElement, mode sendMode) error {
	for len(o.awaiting) == 0 && len(o.held) > 0 {
		next := o.held[0]
		if err := sendElement(ctx, next.elem, output, mode); err != nil {
			return err
		}
		o.held = o.held[1:]
		if next.inHistory {
			o.history.add(next.elem.Message)
		}
		o.trackLocked(&next.elem)
	}
	return nil
}

// closeOut answers every call still awaiting results with a refusal naming
// the session's end, and emits everything held, in order. Run once nothing
// else emits. The first element downstream does not take ends it: nothing
// after it is sent, so no result goes out for a call message that did not.
func (o *toolExchangeOrder) closeOut(ctx context.Context, output chan<- StreamElement, deadline *closeDeadline) {
	o.mu.Lock()
	defer o.mu.Unlock()
	mode := sendMode{recorded: true, deadline: deadline}
	for len(o.awaiting)+len(o.held) > 0 {
		for _, tc := range o.awaiting {
			errMsg := fmt.Sprintf("Tool %s not executed: %s", tc.Name, errSessionClosedBeforeRun)
			refusal := toolRefusal(tc, errMsg)
			refusal.ToolResult.ErrorType = types.ToolErrorExecution
			if sendElement(ctx, StreamElement{Message: &refusal}, output, mode) != nil {
				o.dropLocked()
				return
			}
		}
		o.awaiting = nil
		if o.releaseLocked(ctx, output, mode) != nil {
			o.dropLocked()
			return
		}
	}
}

// dropLocked forgets what is awaited and held: downstream stopped taking
// output.
func (o *toolExchangeOrder) dropLocked() {
	o.awaiting = nil
	o.held = nil
}

// sendElement sends elem on output. A recorded element whose ctx is canceled
// first is still given until its deadline to be taken; it reports ctx.Err()
// only when it was not.
func sendElement(ctx context.Context, elem StreamElement, output chan<- StreamElement, mode sendMode) error {
	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
	}
	if !mode.recorded {
		return ctx.Err()
	}
	deadline := mode.deadline
	if deadline == nil {
		deadline = newCloseDeadline()
	}
	select {
	case output <- elem:
		return nil
	case <-deadline.done():
		return ctx.Err()
	}
}
