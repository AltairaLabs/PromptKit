package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

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

// oobRender is one out-of-band response being rendered. Its fields are set
// by the receive loop, under recvMu, before done is closed.
type oobRender struct {
	audio      []byte
	transcript string
	usage      *UsageInfo
	err        error
	done       chan struct{}
}

// outOfBand is the session's out-of-band responses in progress, by request
// tag until response.created names their response, then by response ID.
// Guarded by recvMu.
type outOfBand struct {
	byTag  map[string]*oobRender
	byResp map[string]*oobRender
}

// SpeakOutOfBand implements providers.OutOfBandSpeaker: it has the model say
// text in the session's voice in a response outside the conversation, and
// returns its audio, transcript and cost. It waits for the server to apply
// the session's configuration (session.updated) first. A text-only session
// asks for nothing.
func (s *RealtimeSession) SpeakOutOfBand(ctx context.Context, text string) (providers.OutOfBandSpeech, error) {
	if outputModalities(s.config.Modalities)[0] != modalityAudio {
		return providers.OutOfBandSpeech{}, errors.New("the session's replies are not spoken")
	}
	select {
	case <-s.ready:
	case <-ctx.Done():
		return providers.OutOfBandSpeech{}, ctx.Err()
	case <-s.ctx.Done():
		return providers.OutOfBandSpeech{}, errors.New(errSessionClosed)
	}
	r, err := s.requestOutOfBand(text)
	if err != nil {
		return providers.OutOfBandSpeech{}, err
	}
	select {
	case <-r.done:
	case <-ctx.Done():
		// The response stays routed to r, so none of it reaches Response().
		return providers.OutOfBandSpeech{}, ctx.Err()
	case <-s.ctx.Done():
		return providers.OutOfBandSpeech{}, errors.New(errSessionClosed)
	}
	speech := providers.OutOfBandSpeech{Audio: r.audio, SampleRate: s.outputSampleRate(), Transcript: r.transcript}
	if r.usage != nil {
		speech.Cost = s.calculateCost(r.usage)
	}
	return speech, r.err
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
	r := &oobRender{done: make(chan struct{})}
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if s.emitClosed {
		return nil, errors.New(errSessionClosed)
	}
	if s.oob.byTag == nil {
		s.oob.byTag = map[string]*oobRender{}
		s.oob.byResp = map[string]*oobRender{}
	}
	s.oob.byTag[id] = r
	if err := s.ws.Send(ev); err != nil {
		delete(s.oob.byTag, id)
		return nil, fmt.Errorf("request an out-of-band response: %w", err)
	}
	return r, nil
}

// routeOutOfBand hands event to the out-of-band render it belongs to, and
// reports whether it did: such an event is not handled as the session's
// own. Called with recvMu held.
func (s *RealtimeSession) routeOutOfBand(event any) bool {
	if len(s.oob.byTag) == 0 && len(s.oob.byResp) == 0 {
		return false
	}
	switch e := event.(type) {
	case *ResponseCreatedEvent:
		r := s.oob.byTag[e.Response.Metadata[metaKeyOutOfBand]]
		if r != nil {
			s.oob.byResp[e.Response.ID] = r
		}
		return r != nil
	case *ResponseDoneEvent:
		return s.endOutOfBand(&e.Response)
	case *ResponseAudioDeltaEvent:
		r := s.oob.byResp[e.ResponseID]
		if r != nil {
			r.addAudio(e.Delta)
		}
		return r != nil
	case *ResponseAudioTranscriptDoneEvent:
		r := s.oob.byResp[e.ResponseID]
		if r != nil {
			r.transcript = e.Transcript
		}
		return r != nil
	case *ResponseAudioDoneEvent:
		return s.oob.byResp[e.ResponseID] != nil
	case *ResponseAudioTranscriptDeltaEvent:
		return s.oob.byResp[e.ResponseID] != nil
	case *ResponseTextDeltaEvent:
		return s.oob.byResp[e.ResponseID] != nil
	case *ResponseTextDoneEvent:
		return s.oob.byResp[e.ResponseID] != nil
	case *ResponseFunctionCallArgumentsDoneEvent:
		return s.oob.byResp[e.ResponseID] != nil
	case *ResponseOutputItemDoneEvent:
		return s.oob.byResp[e.ResponseID] != nil
	case *ErrorEvent:
		return s.outOfBandRejected(e)
	}
	return false
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

// endOutOfBand ends the out-of-band render info answers, if any. Called with
// recvMu held.
func (s *RealtimeSession) endOutOfBand(info *ResponseInfo) bool {
	r, ok := s.oob.byResp[info.ID]
	tag := info.Metadata[metaKeyOutOfBand]
	if !ok {
		r, ok = s.oob.byTag[tag]
	}
	if !ok {
		return false
	}
	delete(s.oob.byResp, info.ID)
	delete(s.oob.byTag, tag)
	r.usage = info.Usage
	if info.Status != realtimeStatusCompleted {
		r.err = fmt.Errorf("the out-of-band response ended %s", info.Status)
	}
	close(r.done)
	return true
}

// outOfBandRejected fails the render whose request a server error names.
// Called with recvMu held.
func (s *RealtimeSession) outOfBandRejected(e *ErrorEvent) bool {
	r, ok := s.oob.byTag[e.Error.EventID]
	if !ok || e.Error.EventID == "" {
		return false
	}
	delete(s.oob.byTag, e.Error.EventID)
	r.err = fmt.Errorf("the server rejected the out-of-band response: %s: %s", e.Error.Code, e.Error.Message)
	close(r.done)
	return true
}

// markReady records that the server applied the session's configuration.
func (s *RealtimeSession) markReady() {
	s.readyOnce.Do(func() { close(s.ready) })
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
