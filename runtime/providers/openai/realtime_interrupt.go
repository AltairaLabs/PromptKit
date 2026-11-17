package openai

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// realtimeEventResponseCancel is the client event that cancels the response
// in progress.
const realtimeEventResponseCancel = "response.cancel"

// InterruptResponse stops the reply on the caller's behalf
// (providers.ResponseInterrupter), as a barge-in does: a response in progress
// is canceled with response.cancel and its audio still queued or arriving is
// dropped, and each audio item the caller had not fully heard is cut to what
// they heard with conversation.item.truncate (the playout estimate). The
// canceled response's response.done still ends the turn. No Interrupted
// chunk is sent: the caller reports the interruption.
//
// A cancel the server rejects because the response ended first is logged,
// not reported as a session error.
func (s *RealtimeSession) InterruptResponse(_ context.Context) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New(errSessionClosed)
	}

	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if s.responseActive {
		event := ResponseCancelEvent{ClientEvent: ClientEvent{
			EventID: s.nextEventID(), Type: realtimeEventResponseCancel,
		}}
		s.cancels.sent(event.EventID, "")
		if err := s.ws.Send(event); err != nil {
			return fmt.Errorf("cancel the response: %w", err)
		}
		s.Barge()
		s.emit(providers.BargeMarker())
	}
	s.truncateHeard()
	return nil
}

// cancelRejected reports whether a server error rejects a response.cancel
// this session sent, logging it: the response it named had already ended.
func (s *RealtimeSession) cancelRejected(e *ErrorEvent) bool {
	if !s.cancels.rejected(e.Error.EventID) {
		return false
	}
	logger.Debug("OpenAI Realtime: the response had ended before the cancel arrived",
		"code", e.Error.Code, "message", e.Error.Message)
	return true
}
