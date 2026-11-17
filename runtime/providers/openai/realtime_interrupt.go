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
// It acts on the response numbered generation (StreamChunk.Generation) only.
// A response not created yet is canceled as soon as it is created, unless
// the caller starts a new turn first (speech_started, or a response.create
// the client sends, text or tool results). A response that has completed
// while a later one began is left alone, and so is the later one.
// Generation 0 is the response in progress, or the next one.
//
// A cancel the server rejects because the response ended first is logged,
// not reported as a session error.
func (s *RealtimeSession) InterruptResponse(_ context.Context, generation int64) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New(errSessionClosed)
	}

	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if s.emitClosed {
		return errors.New(errSessionClosed)
	}
	target := generation
	if target == 0 {
		target = s.gen
		if !s.responseActive {
			target++
		}
	}
	switch {
	case target > s.gen:
		s.cancelGen = target // not created yet: none of it has played
	case target == s.gen:
		if s.responseActive {
			if err := s.cancelResponse(); err != nil {
				return err
			}
		}
		s.truncateHeard()
	}
	return nil
}

// cancelResponse cancels the response in progress and drops its audio still
// queued or arriving. Called with recvMu held.
func (s *RealtimeSession) cancelResponse() error {
	event := ResponseCancelEvent{ClientEvent: ClientEvent{
		EventID: s.nextEventID(), Type: realtimeEventResponseCancel,
	}}
	if n := len(s.respOrder); n > 0 {
		event.ResponseID = s.respOrder[n-1]
	}
	s.cancels.sent(event.EventID, "")
	if err := s.ws.Send(event); err != nil {
		return fmt.Errorf("cancel the response: %w", err)
	}
	s.Barge()
	s.emit(providers.BargeMarker())
	return nil
}

// cancelPending cancels a response just created when InterruptResponse named
// it before it existed, so the reply is neither generated nor billed. Called
// with recvMu held, on response.created.
func (s *RealtimeSession) cancelPending() {
	if s.cancelGen == 0 || s.gen != s.cancelGen {
		return
	}
	s.cancelGen = 0
	if err := s.cancelResponse(); err != nil {
		logger.Warn("OpenAI Realtime: failed to cancel the interrupted turn's response", "error", err)
	}
}

// startClientTurn records that the client started a turn (response.create):
// a cancel pending from the last turn does not apply to it.
func (s *RealtimeSession) startClientTurn() {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	s.cancelGen = 0
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

// Announce implements providers.Announcer: a response whose instructions are
// to say text, without tools. It adds no user item, so the conversation gains
// no user turn; the spoken reply joins it as the assistant's. A text-only
// session asks for nothing.
func (s *RealtimeSession) Announce(_ context.Context, text string) (bool, error) {
	if outputModalities(s.config.Modalities)[0] != modalityAudio {
		return false, nil
	}
	err := s.TriggerResponse(&ResponseConfig{
		Instructions: providers.AnnounceInstructions(text),
		ToolChoice:   "none",
	})
	return err == nil, err
}
