package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// An out-of-band response is one the session asks for outside the
// conversation (response.conversation "none"), to have the model speak a
// text in the session's voice (SpeakOutOfBand). Probed live (2026-11-18): it
// streams its audio and transcript as a reply does, its response.done has
// conversation_id null, and no conversation.item.added fires for its item
// (conversation.item.done and response.output_item.done still do); a later
// reply does not know it was said. Several run at once, beside a reply, with
// no conversation_already_has_active_response.
//
// So the session routes every event of one to the render waiting for it, and
// none to Response(): not its audio, transcript, cost or end. It is outside
// the one-reply-at-a-time queue (realtime_response_queue.go): it neither
// waits for a reply nor holds one back, and its end sends nothing waiting.
// It is not numbered as a generation, so InterruptResponse never names it,
// and it answers no caller input, so ForgetTurn never deletes it. None of
// its audio is played by the caller, so a barge-in cuts none of it.

// metaKeyOutOfBand is the response.metadata key tagging an out-of-band
// response, set to its response.create's event ID.
const metaKeyOutOfBand = "promptkit_out_of_band"

// conversationNone asks for a response outside the conversation.
const conversationNone = "none"

// outOfBandInstructions are the instructions that have the model say text.
func outOfBandInstructions(text string) string {
	return "Say exactly the following, in the same language, and nothing else: " + text
}

// oobCancelGrace is how long a render whose context ended waits for its
// canceled response's end, for what it cost.
const oobCancelGrace = 500 * time.Millisecond

// oobRender is one out-of-band response being rendered. Its fields are set
// by the receive loop under recvMu; once ended, nothing changes them and done
// is closed.
type oobRender struct {
	tag    string // its request's event ID
	respID string // set by response.created
	// canceled is set when its caller stopped waiting: the response is
	// canceled as soon as it is named.
	canceled   bool
	ended      bool
	audio      []byte
	transcript string
	usage      *UsageInfo
	err        error
	done       chan struct{}
}

// end ends the render with err (nil: as it is). Each render is ended once,
// by whatever takes it out of the session's routing.
func (r *oobRender) end(err error) {
	r.ended = true
	r.err = err
	close(r.done)
}

// outOfBand is the session's out-of-band responses in progress, by request
// tag until response.created names their response, then by response ID.
// dropped are the responses whose render ended early (rejected after it was
// created): their events still go nowhere until their response.done.
// Guarded by recvMu.
type outOfBand struct {
	byTag   map[string]*oobRender
	byResp  map[string]*oobRender
	dropped map[string]bool
}

// route is the render of the response respID, and whether the response is
// an out-of-band one (a dropped one has no render).
func (o *outOfBand) route(respID string) (*oobRender, bool) {
	r := o.byResp[respID]
	return r, r != nil || o.dropped[respID]
}

// SpeakOutOfBand implements providers.OutOfBandSpeaker: it has the model say
// text in the session's voice in a response outside the conversation, and
// returns its audio, transcript and cost. It waits for the server to apply
// the session's configuration (session.updated) first, and fails at once when
// the server rejected it. A text-only session asks for nothing.
//
// When ctx ends first, the response is canceled (response.cancel naming it,
// sent once the server has named it), and SpeakOutOfBand waits up to
// oobCancelGrace for its end, to return what it cost with ctx's error.
func (s *RealtimeSession) SpeakOutOfBand(ctx context.Context, text string) (providers.OutOfBandSpeech, error) {
	if outputModalities(s.config.Modalities)[0] != modalityAudio {
		return providers.OutOfBandSpeech{}, errors.New("the session's replies are not spoken")
	}
	select {
	case <-s.ready:
		if s.readyErr != nil {
			return providers.OutOfBandSpeech{}, s.readyErr
		}
	case <-ctx.Done():
		return providers.OutOfBandSpeech{}, ctx.Err()
	case <-s.ctx.Done():
		return providers.OutOfBandSpeech{}, providers.ErrOutOfBandSessionClosed
	}
	r, err := s.requestOutOfBand(text)
	if err != nil {
		return providers.OutOfBandSpeech{}, err
	}
	var waitErr error
	select {
	case <-r.done:
	case <-ctx.Done():
		waitErr = ctx.Err()
		if !s.awaitCanceled(r) {
			return providers.OutOfBandSpeech{}, waitErr
		}
	case <-s.ctx.Done():
		return providers.OutOfBandSpeech{}, providers.ErrOutOfBandSessionClosed
	}
	speech := providers.OutOfBandSpeech{Audio: r.audio, SampleRate: s.outputSampleRate(), Transcript: r.transcript}
	if r.usage != nil {
		speech.Cost = s.calculateCost(r.usage)
	}
	if waitErr != nil {
		return speech, waitErr
	}
	return speech, r.err
}

// awaitCanceled cancels r's response for a caller that stopped waiting, and
// waits up to oobCancelGrace for its end. It reports whether r ended.
func (s *RealtimeSession) awaitCanceled(r *oobRender) bool {
	s.abandonOutOfBand(r)
	select {
	case <-r.done:
		return true
	case <-time.After(oobCancelGrace):
	case <-s.ctx.Done():
	}
	return false
}

// requestOutOfBand sends the response.create for an out-of-band response
// saying text, and returns the render its events go to.
func (s *RealtimeSession) requestOutOfBand(text string) (*oobRender, error) {
	id := s.nextEventID()
	ev := ResponseCreateEvent{
		ClientEvent: ClientEvent{EventID: id, Type: realtimeEventResponseCreate},
		Response: &ResponseConfig{
			Conversation:     conversationNone,
			Instructions:     outOfBandInstructions(text),
			ToolChoice:       toolChoiceNone,
			OutputModalities: []string{modalityAudio},
			Metadata:         map[string]string{metaKeyOutOfBand: id},
		},
	}
	r := &oobRender{tag: id, done: make(chan struct{})}
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if s.emitClosed || s.ctx.Err() != nil {
		return nil, providers.ErrOutOfBandSessionClosed
	}
	if s.oob.byTag == nil {
		s.oob.byTag = map[string]*oobRender{}
		s.oob.byResp = map[string]*oobRender{}
		s.oob.dropped = map[string]bool{}
	}
	s.oob.byTag[id] = r
	if err := s.ws.Send(ev); err != nil {
		delete(s.oob.byTag, id)
		return nil, fmt.Errorf("request an out-of-band response: %w", err)
	}
	return r, nil
}

// abandonOutOfBand cancels r's response: now when the server has named it,
// else once it does.
func (s *RealtimeSession) abandonOutOfBand(r *oobRender) {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if r.ended {
		return
	}
	r.canceled = true
	if r.respID != "" {
		s.cancelOutOfBand(r)
	}
}

// cancelOutOfBand sends the response.cancel naming r's response. A cancel
// the server rejects (the response had ended) is logged, not a session
// error. Called with recvMu held.
func (s *RealtimeSession) cancelOutOfBand(r *oobRender) {
	event := ResponseCancelEvent{
		ClientEvent: ClientEvent{EventID: s.nextEventID(), Type: realtimeEventResponseCancel},
		ResponseID:  r.respID,
	}
	s.cancels.sent(event.EventID, "")
	if err := s.ws.Send(event); err != nil {
		logger.Warn("OpenAI Realtime: failed to cancel an out-of-band response", "error", err)
	}
}

// routeOutOfBand hands event to the out-of-band render it belongs to, and
// reports whether it did: such an event is not handled as the session's
// own. Called with recvMu held.
func (s *RealtimeSession) routeOutOfBand(event any) bool {
	if len(s.oob.byTag) == 0 && len(s.oob.byResp) == 0 && len(s.oob.dropped) == 0 {
		return false
	}
	switch e := event.(type) {
	case *ResponseCreatedEvent:
		return s.outOfBandCreated(&e.Response)
	case *ResponseDoneEvent:
		return s.endOutOfBand(&e.Response)
	case *ResponseAudioDeltaEvent:
		r, ours := s.oob.route(e.ResponseID)
		if r != nil {
			r.addAudio(e.Delta)
		}
		return ours
	case *ResponseAudioTranscriptDoneEvent:
		r, ours := s.oob.route(e.ResponseID)
		if r != nil {
			r.transcript = e.Transcript
		}
		return ours
	case *ResponseAudioDoneEvent:
		return s.oob.owns(e.ResponseID)
	case *ResponseAudioTranscriptDeltaEvent:
		return s.oob.owns(e.ResponseID)
	case *ResponseTextDeltaEvent:
		return s.oob.owns(e.ResponseID)
	case *ResponseTextDoneEvent:
		return s.oob.owns(e.ResponseID)
	case *ResponseFunctionCallArgumentsDoneEvent:
		return s.oob.owns(e.ResponseID)
	case *ResponseOutputItemDoneEvent:
		return s.oob.owns(e.ResponseID)
	case *ErrorEvent:
		return s.outOfBandRejected(e)
	}
	return false
}

// outOfBandCreated routes the response info names to its render, and
// cancels it when its caller stopped waiting. Called with recvMu held.
func (s *RealtimeSession) outOfBandCreated(info *ResponseInfo) bool {
	r := s.oob.byTag[info.Metadata[metaKeyOutOfBand]]
	if r == nil {
		return false
	}
	r.respID = info.ID
	s.oob.byResp[info.ID] = r
	if r.canceled {
		s.cancelOutOfBand(r)
	}
	return true
}

// owns reports whether respID is an out-of-band response.
func (o *outOfBand) owns(respID string) bool {
	_, ours := o.route(respID)
	return ours
}

// addAudio appends a base64 audio delta.
func (r *oobRender) addAudio(delta string) {
	pcm, err := base64.StdEncoding.DecodeString(delta)
	if err != nil {
		logger.Warn("OpenAI Realtime: failed to decode out-of-band audio", "error", err)
		return
	}
	r.audio = append(r.audio, pcm...)
}

// endOutOfBand ends the out-of-band render info answers, if any, and stops
// routing its response. Called with recvMu held.
func (s *RealtimeSession) endOutOfBand(info *ResponseInfo) bool {
	if s.oob.dropped[info.ID] {
		delete(s.oob.dropped, info.ID)
		return true
	}
	r, ok := s.oob.byResp[info.ID]
	if !ok {
		r, ok = s.oob.byTag[info.Metadata[metaKeyOutOfBand]]
	}
	if !ok {
		return false
	}
	delete(s.oob.byResp, info.ID)
	delete(s.oob.byTag, r.tag)
	r.usage = info.Usage
	var err error
	if info.Status != realtimeStatusCompleted {
		err = fmt.Errorf("the out-of-band response ended %s", info.Status)
	}
	r.end(err)
	return true
}

// outOfBandRejected ends the render whose request a server error names. A
// response already created for it is dropped: its events go nowhere until
// its end. Called with recvMu held.
func (s *RealtimeSession) outOfBandRejected(e *ErrorEvent) bool {
	r, ok := s.oob.byTag[e.Error.EventID]
	if !ok || e.Error.EventID == "" {
		return false
	}
	delete(s.oob.byTag, e.Error.EventID)
	if r.respID != "" {
		delete(s.oob.byResp, r.respID)
		s.oob.dropped[r.respID] = true
	}
	r.end(fmt.Errorf("the server rejected the out-of-band response: %s: %s", e.Error.Code, e.Error.Message))
	return true
}

// markReady records that the server applied the session's configuration.
func (s *RealtimeSession) markReady() {
	s.readyOnce.Do(func() { close(s.ready) })
}

// configRejected reports whether a server error rejects the session's
// configuration (its first session.update), and if so fails the renders
// waiting for it: no session.updated is coming. Called with recvMu held.
func (s *RealtimeSession) configRejected(e *ErrorEvent) bool {
	if e.Error.EventID == "" || e.Error.EventID != s.updateEventID {
		return false
	}
	s.readyOnce.Do(func() {
		s.readyErr = fmt.Errorf("the server rejected the session configuration: %s: %s", e.Error.Code, e.Error.Message)
		close(s.ready)
	})
	return true
}

// SpeakOutOfBand forwards to the underlying session if it implements
// providers.OutOfBandSpeaker — the pipeline type-asserts the wrapper, so
// hidden, cues would not be spoken in the session's voice.
func (s *realtimeSessionBookkeeping) SpeakOutOfBand(ctx context.Context, text string) (providers.OutOfBandSpeech, error) {
	if sp, ok := s.StreamInputSession.(providers.OutOfBandSpeaker); ok {
		return sp.SpeakOutOfBand(ctx, text)
	}
	return providers.OutOfBandSpeech{}, errors.New("realtime session cannot speak out of band")
}

var _ providers.OutOfBandSpeaker = (*RealtimeSession)(nil)
