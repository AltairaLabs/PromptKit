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
// came. Elements without a message (audio, text, reasoning, errors) are not
// held. A call held for the caller (approval) is answered later, by the
// caller: it holds nothing back once it is surfaced.
//
// When the session ends, a round the worker had not handed over is still
// recorded, and each call left with no result (a round cut short, or never
// run) is answered with a refusal, so the stored conversation never keeps a
// call without its result.

// errSessionClosedBeforeRun is the cause recorded for a call the session's end
// left unanswered.
const errSessionClosedBeforeRun = "the session closed before it ran"

// toolExchangeOrder holds the messages that follow a tool call message the
// stage runs until the call's results are out.
type toolExchangeOrder struct {
	mu sync.Mutex
	// awaiting are the calls of the last emitted call message with no result
	// yet, in the order the model made them.
	awaiting []types.MessageToolCall
	// held are the elements held behind them, in the order they came.
	held []StreamElement
}

// emit sends elem downstream, or holds it while a tool call message emitted
// before it still awaits results (see toolExchangeOrder). It returns ctx.Err()
// when canceled first.
func (s *DuplexProviderStage) emit(ctx context.Context, elem StreamElement, output chan<- StreamElement) error {
	return s.exchange.send(ctx, elem, output, false)
}

// emitRecorded is emit for an element that must reach the transcript even
// once ctx is canceled (a tool round's results): it is then given
// partialResponseSendTimeout to be taken, as a canceled turn's partial reply
// is.
func (s *DuplexProviderStage) emitRecorded(ctx context.Context, elem StreamElement, output chan<- StreamElement) error {
	return s.exchange.send(ctx, elem, output, true)
}

// send places elem: a message behind an unanswered call message is held; any
// other element is sent, and a result or hold that answers the last call
// releases what was held.
func (o *toolExchangeOrder) send(
	ctx context.Context, elem StreamElement, output chan<- StreamElement, recorded bool,
) error {
	if elem.Message == nil && len(elem.Meta.PendingTools) == 0 {
		return sendElement(ctx, elem, output, recorded)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if elem.Message != nil && len(o.awaiting)+len(o.held) > 0 && !o.answersLocked(&elem) {
		o.held = append(o.held, elem)
		return nil
	}
	if err := sendElement(ctx, elem, output, recorded); err != nil {
		return err
	}
	o.trackLocked(&elem)
	return o.releaseLocked(ctx, output, recorded)
}

// answersLocked reports whether elem is a result for an awaited call.
func (o *toolExchangeOrder) answersLocked(elem *StreamElement) bool {
	tr := elem.Message.ToolResult
	if tr == nil {
		return false
	}
	for _, tc := range o.awaiting {
		if tc.ID == tr.ID {
			return true
		}
	}
	return false
}

// trackLocked records what an emitted element does to the awaited calls: a
// call message the stage runs makes its calls awaited; a result, or a hold for
// the caller, answers one.
func (o *toolExchangeOrder) trackLocked(elem *StreamElement) {
	if m := elem.Message; m != nil && elem.Meta.ToolCallsRunning && len(m.ToolCalls) > 0 {
		o.awaiting = append([]types.MessageToolCall(nil), m.ToolCalls...)
		return
	}
	if m := elem.Message; m != nil && m.ToolResult != nil {
		o.answerLocked(m.ToolResult.ID)
	}
	for i := range elem.Meta.PendingTools {
		o.answerLocked(elem.Meta.PendingTools[i].CallID)
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
func (o *toolExchangeOrder) releaseLocked(ctx context.Context, output chan<- StreamElement, recorded bool) error {
	for len(o.awaiting) == 0 && len(o.held) > 0 {
		next := o.held[0]
		if err := sendElement(ctx, next, output, recorded); err != nil {
			return err
		}
		o.held = o.held[1:]
		o.trackLocked(&next)
	}
	return nil
}

// closeOut answers every call still awaiting results with a refusal naming
// the session's end, and emits everything held, in order, as recorded
// elements. Run once nothing else emits.
func (o *toolExchangeOrder) closeOut(ctx context.Context, output chan<- StreamElement) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(o.awaiting)+len(o.held) > 0 {
		for _, tc := range o.awaiting {
			errMsg := fmt.Sprintf("Tool %s not executed: %s", tc.Name, errSessionClosedBeforeRun)
			refusal := toolRefusal(tc, errMsg)
			refusal.ToolResult.ErrorType = types.ToolErrorExecution
			_ = sendElement(ctx, StreamElement{Message: &refusal}, output, true)
		}
		o.awaiting = nil
		if err := o.releaseLocked(ctx, output, true); err != nil {
			// Downstream stopped taking output: what is left cannot be
			// recorded.
			o.held = o.held[1:]
		}
	}
}

// sendElement sends elem on output. A recorded element whose ctx is canceled
// first is still given partialResponseSendTimeout to be taken; it reports
// ctx.Err() only when it was not.
func sendElement(ctx context.Context, elem StreamElement, output chan<- StreamElement, recorded bool) error {
	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
	}
	if !recorded {
		return ctx.Err()
	}
	// NOSONAR: intentional fresh timer - ctx is canceled, and the result must
	// still reach the stages that store it.
	timer := time.NewTimer(partialResponseSendTimeout)
	defer timer.Stop()
	select {
	case output <- elem:
		return nil
	case <-timer.C:
		return ctx.Err()
	}
}
