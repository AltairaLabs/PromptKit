package openai

import "github.com/AltairaLabs/PromptKit/runtime/v2/logger"

// OpenAI Realtime runs one response at a time: a response.create sent while
// one is in progress is rejected (conversation_already_has_active_response).
// A response is in progress from the response.create that asks for it (or a
// response the server started itself, as on server VAD) until its
// response.done. So the session sends a response.create only when none is in
// progress, and holds one asked for meanwhile (text typed while a reply or a
// spoken guardrail message plays, tool results) until the response in
// progress is done. The input the create answers is added at once; only the
// request for the reply waits, as the stage expects: user input never ends a
// reply under way, and the user's turn starts with the next one.

// errCodeActiveResponse is the server's error for a response.create sent
// while a response is in progress.
const errCodeActiveResponse = "conversation_already_has_active_response"

// maxSentCreates bounds the response.create events remembered for matching a
// rejection to the create it rejects.
const maxSentCreates = 8

// requestResponse sends ev, a response.create, now when no response is in
// progress, else once the one in progress is done.
func (s *RealtimeSession) requestResponse(ev ResponseCreateEvent) error {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if s.responseBusyLocked() {
		logger.Debug("OpenAI Realtime: a response is in progress; the next one is requested once it is done")
		s.queuedCreates = append(s.queuedCreates, ev)
		return nil
	}
	return s.sendCreateLocked(ev)
}

// responseBusyLocked reports whether a response is in progress, or asked for
// and not yet created. Called with recvMu held.
func (s *RealtimeSession) responseBusyLocked() bool {
	return s.responseActive || s.createPending
}

// sendCreateLocked sends ev and remembers it until the server answers it.
// Called with recvMu held.
func (s *RealtimeSession) sendCreateLocked(ev ResponseCreateEvent) error {
	s.createPending = true
	s.sentCreates = append(s.sentCreates, ev)
	if len(s.sentCreates) > maxSentCreates {
		s.sentCreates = s.sentCreates[1:]
	}
	return s.ws.Send(ev)
}

// sendQueuedCreate sends the response.create held longest, once no response
// is in progress. Called with recvMu held, on response.done.
func (s *RealtimeSession) sendQueuedCreate() {
	if len(s.queuedCreates) == 0 || s.responseBusyLocked() || s.emitClosed {
		return
	}
	ev := s.queuedCreates[0]
	s.queuedCreates = s.queuedCreates[1:]
	if err := s.sendCreateLocked(ev); err != nil {
		logger.Warn("OpenAI Realtime: failed to request the next response", "error", err)
	}
}

// createRejectedAsBusy reports whether a server error rejects a
// response.create this session sent because a response was in progress (one
// the server started itself as the create went out): the create is held for
// that response's end, not reported as a session error. Called with recvMu
// held.
func (s *RealtimeSession) createRejectedAsBusy(e *ErrorEvent) bool {
	if e.Error.Code != errCodeActiveResponse {
		return false
	}
	ev, ok := s.takeSentCreate(e.Error.EventID)
	if !ok {
		return false
	}
	s.queuedCreates = append([]ResponseCreateEvent{ev}, s.queuedCreates...)
	// Sent again on that response's response.done, not now: the server says
	// one is in progress, whether or not its response.created has arrived.
	logger.Debug("OpenAI Realtime: a response was in progress; the response is requested again once it is done")
	return true
}

// createRejected reports whether a server error rejects a response.create
// this session sent (for any other reason): it is no longer pending. Called
// with recvMu held.
func (s *RealtimeSession) createRejected(e *ErrorEvent) bool {
	_, ok := s.takeSentCreate(e.Error.EventID)
	return ok
}

// takeSentCreate forgets the sent response.create eventID names, and returns
// it; no create is pending once the server answered one with an error.
// Called with recvMu held.
func (s *RealtimeSession) takeSentCreate(eventID string) (ResponseCreateEvent, bool) {
	if eventID == "" {
		return ResponseCreateEvent{}, false
	}
	for i, ev := range s.sentCreates {
		if ev.EventID == eventID {
			s.sentCreates = append(s.sentCreates[:i], s.sentCreates[i+1:]...)
			s.createPending = false
			return ev, true
		}
	}
	return ResponseCreateEvent{}, false
}
