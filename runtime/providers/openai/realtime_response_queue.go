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
//   - The reply an Announce asked for is marked on every chunk
//     (providers.MetaKeyAnnouncement), so the stage tells it by the mark, not
//     by its position. An Announce goes to the front of the waiting requests:
//     the message follows the reply it explains.
//   - A response the server started on its own reads the whole conversation,
//     so it answers every input added before it: the requests waiting for it
//     (an Announce excepted) are dropped, not sent after it as a second reply.
//     Tool results are not lost this way: they are in the conversation, and
//     that response answers them. While the caller speaks over a reply (server
//     VAD that creates responses), nothing waiting is sent: the server starts
//     the response to their speech, and a request sent as the interrupted
//     reply ends would talk over them.

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

// maxAnnounceGens bounds the announcement generations remembered.
const maxAnnounceGens = 8

// responseRequest is a response.create the session sends, or holds until it
// can. announce marks the one an Announce asked for; satisfied is set on a
// request sent just as the server started a response of its own, which
// answers it.
type responseRequest struct {
	ev        ResponseCreateEvent
	announce  bool
	satisfied bool
}

// requestResponse sends a response.create now when nothing is in progress or
// waiting, and holds it otherwise: in order, or first for an Announce.
func (s *RealtimeSession) requestResponse(ev ResponseCreateEvent, announce bool) error {
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
	req := &responseRequest{ev: ev, announce: announce}

	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if s.emitClosed {
		return errors.New(errSessionClosed)
	}
	if s.responseActive || s.pendingRequest != nil || s.speechTurn || len(s.waitingRequests) > 0 {
		logger.Debug("OpenAI Realtime: a response is in progress; the next one is requested once it is done",
			"announce", announce)
		if announce {
			s.waitingRequests = append([]*responseRequest{req}, s.waitingRequests...)
		} else {
			s.waitingRequests = append(s.waitingRequests, req)
		}
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
// progress. A failed send is reported on Response(). Called with recvMu held.
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
// answers the pending request its tag names (an Announce's is marked as the
// announcement), or, with no tag of this session's, it is one the server
// started itself, which answers every request waiting (an Announce excepted)
// and the pending one. Called with recvMu held, after the generation is
// numbered.
func (s *RealtimeSession) responseCreated(info *ResponseInfo) {
	s.speechTurn = false
	tag := info.Metadata[metaKeyRequest]
	logger.Debug("OpenAI Realtime: response created", "response_id", info.ID, "request", tag)
	if p := s.pendingRequest; p != nil && tag != "" && tag == p.ev.EventID {
		s.pendingRequest = nil
		if p.announce {
			s.markAnnouncement(s.gen)
		}
		return
	}
	if tag != "" {
		return // a request answered earlier (it was retried): nothing to settle
	}
	kept := s.waitingRequests[:0]
	for _, req := range s.waitingRequests {
		if req.announce {
			kept = append(kept, req)
			continue
		}
		logger.Debug("OpenAI Realtime: a response the server started answers a waiting request; it is not sent")
	}
	s.waitingRequests = kept
	if p := s.pendingRequest; p != nil && !p.announce {
		p.satisfied = true // its rejection as busy is expected: it is answered
	}
}

// markAnnouncement records gen as the reply an Announce asked for.
func (s *RealtimeSession) markAnnouncement(gen int64) {
	s.announceGens = append(s.announceGens, gen)
	if len(s.announceGens) > maxAnnounceGens {
		s.announceGens = s.announceGens[1:]
	}
}

// isAnnouncement reports whether gen is a reply an Announce asked for.
func (s *RealtimeSession) isAnnouncement(gen int64) bool {
	for _, g := range s.announceGens {
		if g == gen {
			return true
		}
	}
	return false
}

// TagsAnnouncements implements providers.AnnouncementTagger: the chunks of
// the reply an Announce asked for carry providers.MetaKeyAnnouncement.
func (s *RealtimeSession) TagsAnnouncements() bool { return true }

// requestRejected handles a server error that answers the pending request
// (it names its event_id, or names none while a request is pending), and
// reports whether it was handled without a session error: one rejected as
// busy is sent again once nothing is in progress (dropped when a response the
// server started answers it); a busy error answering no request of the
// session's is logged. Any other rejection ends the pending request, and the
// next waiting one is sent once the error is reported. Called with recvMu
// held.
func (s *RealtimeSession) requestRejected(e *ErrorEvent) (handled, ended bool) {
	busy := e.Error.Code == errCodeActiveResponse
	p := s.pendingRequest
	answers := p != nil && (e.Error.EventID == "" || e.Error.EventID == p.ev.EventID)
	switch {
	case answers && busy:
		s.pendingRequest = nil
		if !p.satisfied {
			s.waitingRequests = append([]*responseRequest{p}, s.waitingRequests...)
		}
		logger.Debug("OpenAI Realtime: a response was in progress; the response is requested again once it is done")
		if !s.responseActive {
			time.AfterFunc(createRetryDelay, s.retryRequests)
		}
		return true, false
	case busy:
		logger.Warn("OpenAI Realtime: the server reported a response in progress", "message", e.Error.Message)
		return true, false
	case answers:
		s.pendingRequest = nil
		return false, true
	}
	return false, false
}

// retryRequests sends the request waiting longest, if nothing is in progress
// by now.
func (s *RealtimeSession) retryRequests() {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	s.sendNextRequestLocked()
}
