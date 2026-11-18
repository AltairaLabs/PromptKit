package openai

import (
	"errors"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// OpenAI Realtime runs one response at a time: a response.create sent while
// one is in progress is rejected (conversation_already_has_active_response).
// So the session sends a response.create only when no response is in
// progress, none it asked for is awaiting its response.created, and none
// asked for earlier is still waiting: requests go out one at a time, in the
// order they were made. The input a request answers (typed text, tool
// results) is added to the conversation at once; only the request for the
// reply waits, as the stage expects: input never ends a reply under way, and
// its turn starts with the next reply.
//
// Every request carries its own tag in response.metadata, which the server
// echoes on response.created, so the session knows which response answers
// which request, and which response the server started on its own (server
// VAD, for the caller's speech):
//
//   - While the caller speaks (server VAD that creates responses), nothing
//     waiting is sent: the server starts the response to their speech, and a
//     request sent as an interrupted reply ends would talk over them. That
//     response reads the whole conversation, so the requests that were
//     waiting before the caller began to speak are
//     answered by it and dropped, not sent after it as a second reply: their
//     input (typed text, tool results) reached the server before the speech
//     began. A request asked for once the caller began to speak, or sent as
//     the server started its response, is kept and sent after it: its input
//     may have reached the server after that response began, and so not be
//     in it.
//   - The hold on speech ends when no response is coming for it: the input
//     audio is cleared, a server error comes while no request is pending (the
//     server's response failed), or speechResponseGrace passes after
//     speech_stopped with no response.created.

// errCodeActiveResponse is the server's error for a response.create sent
// while a response is in progress.
const errCodeActiveResponse = "conversation_already_has_active_response"

// metaKeyRequest is the response.metadata key carrying a request's tag.
const metaKeyRequest = "promptkit_request"

// createRetryDelay is how long a request the server rejected as busy waits to
// be sent again when no response is in progress on the session's side (the
// server's response.created has not arrived): it is sent again then, or on
// the next response.done, whichever comes first.
const createRetryDelay = 100 * time.Millisecond

// speechResponseGrace is how long after speech_stopped the requests waiting
// are held for the server's response to the speech; with no response.created
// by then, none is coming.
const speechResponseGrace = 2 * time.Second

// responseRequest is a response.create the session sends, or holds until it
// can. speechSeq is the count of the caller's speech turns when it was asked
// for.
type responseRequest struct {
	ev        ResponseCreateEvent
	speechSeq int64
}

// requestResponse sends a response.create now when nothing is in progress or
// waiting, and holds it otherwise, in order.
func (s *RealtimeSession) requestResponse(ev ResponseCreateEvent) error {
	// The caller's config is copied, not changed, to carry the tag.
	config := ResponseConfig{}
	if ev.Response != nil {
		config = *ev.Response
	}
	meta := make(map[string]string, len(config.Metadata)+1)
	for k, v := range config.Metadata {
		meta[k] = v
	}
	meta[metaKeyRequest] = ev.EventID
	config.Metadata = meta
	ev.Response = &config

	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if s.emitClosed {
		return errors.New(errSessionClosed)
	}
	req := &responseRequest{ev: ev, speechSeq: s.speechSeq}
	if s.responseActive || s.pendingRequest != nil || s.speechTurn || len(s.waitingRequests) > 0 {
		logger.Debug("OpenAI Realtime: a response is in progress; the next one is requested once it is done")
		s.waitingRequests = append(s.waitingRequests, req)
		return nil
	}
	return s.sendRequestLocked(req)
}

// sendRequestLocked sends req and holds it as pending until the server
// answers it. The send starts the caller's new turn: a cancel pending for
// the last one does not apply to it. Called with recvMu held.
func (s *RealtimeSession) sendRequestLocked(req *responseRequest) error {
	s.cancelGen = 0
	s.pendingRequest = req
	if err := s.ws.Send(req.ev); err != nil {
		s.pendingRequest = nil
		return fmt.Errorf("request a response: %w", err)
	}
	return nil
}

// sendNextRequestLocked sends the request waiting longest once nothing is in
// progress. A failed send is reported on Response() as an error: the request
// is well-formed, so only a broken connection fails it, and that ends the
// session. Called with recvMu held.
func (s *RealtimeSession) sendNextRequestLocked() {
	if s.emitClosed || s.responseActive || s.pendingRequest != nil || s.speechTurn ||
		len(s.waitingRequests) == 0 {
		return
	}
	req := s.waitingRequests[0]
	s.waitingRequests = s.waitingRequests[1:]
	if err := s.sendRequestLocked(req); err != nil {
		logger.Warn("OpenAI Realtime: failed to request the next response", "error", err)
		s.emit(providers.StreamChunk{Error: err})
	}
}

// responseCreated settles the requests on response.created: the response
// answers the pending request its tag names, or, with no tag of this
// session's, it is one the server started itself, which answers the requests
// waiting since before the caller's speech began. Called with recvMu held,
// after the generation is numbered.
func (s *RealtimeSession) responseCreated(info *ResponseInfo) {
	s.speechTurn = false
	tag := info.Metadata[metaKeyRequest]
	logger.Debug("OpenAI Realtime: response created", "response_id", info.ID, "request", tag)
	if p := s.pendingRequest; p != nil && tag != "" && tag == p.ev.EventID {
		s.pendingRequest = nil
		return
	}
	if tag != "" {
		return // a request answered earlier (it was retried): nothing to settle
	}
	kept := s.waitingRequests[:0]
	for _, req := range s.waitingRequests {
		if req.speechSeq >= s.speechSeq {
			kept = append(kept, req)
			continue
		}
		logger.Debug("OpenAI Realtime: a response the server started answers a waiting request; it is not sent")
	}
	s.waitingRequests = kept
}

// speechStopped starts the grace for the server's response to the caller's
// speech: with no response.created by then, the requests waiting go. Called
// with recvMu held.
func (s *RealtimeSession) speechStopped() {
	if !s.speechTurn {
		return
	}
	seq := s.speechSeq
	grace := s.speechGrace
	if grace <= 0 {
		grace = speechResponseGrace
	}
	time.AfterFunc(grace, func() {
		s.recvMu.Lock()
		defer s.recvMu.Unlock()
		if s.speechSeq == seq {
			s.endSpeechTurn("no response to the caller's speech")
		}
	})
}

// endSpeechTurn ends the hold on the requests waiting for the server's
// response to the caller's speech, which is not coming, and sends the next
// one. Called with recvMu held.
func (s *RealtimeSession) endSpeechTurn(why string) {
	if !s.speechTurn {
		return
	}
	logger.Debug("OpenAI Realtime: the requests waiting go", "why", why)
	s.speechTurn = false
	s.sendNextRequestLocked()
}

// requestRejected handles a server error that answers the pending request
// (it names its event_id, or names none while a request is pending), and
// reports whether it was handled without a session error: one rejected as
// busy is sent again, first, once nothing is in progress; a busy error
// answering no request of the session's is logged. Any other rejection ends
// the pending request, which is returned. Called with recvMu held.
func (s *RealtimeSession) requestRejected(e *ErrorEvent) (handled bool, ended *responseRequest) {
	busy := e.Error.Code == errCodeActiveResponse
	p := s.pendingRequest
	answers := p != nil && (e.Error.EventID == "" || e.Error.EventID == p.ev.EventID)
	switch {
	case answers && busy:
		s.pendingRequest = nil
		s.waitingRequests = append([]*responseRequest{p}, s.waitingRequests...)
		logger.Debug("OpenAI Realtime: a response was in progress; the response is requested again once it is done")
		if !s.responseActive {
			time.AfterFunc(createRetryDelay, s.retryRequests)
		}
		return true, nil
	case busy:
		logger.Warn("OpenAI Realtime: the server reported a response in progress", "message", e.Error.Message)
		return true, nil
	case answers:
		s.pendingRequest = nil
		return false, p
	}
	return false, nil
}

// retryRequests sends the request waiting longest, if nothing is in progress
// by now.
func (s *RealtimeSession) retryRequests() {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	s.sendNextRequestLocked()
}
