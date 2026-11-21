package sdk

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A realtime model speaks as it writes, so a guardrail that holds a reply
// (stream_hold: turn or segment) keeps it from the caller's ears only when the
// session holds the reply's audio too (StreamHoldConfig.Realtime). These tests
// drive OpenDuplex with the mock realtime session, scripting the reply's
// transcript and audio chunk by chunk, and an llm_judge guardrail whose judge
// answers scripted verdicts, each only once the test lets it.

// rtHold is a realtime conversation under an llm_judge guardrail.
type rtHold struct {
	*cueSession
	judge *holdJudge
	log   *holdLog
}

// openRealtimeHold opens a realtime conversation whose output is judged by an
// llm_judge with params, answering verdicts, under opts.
func openRealtimeHold(t *testing.T, params map[string]any, verdicts []bool, opts ...Option) *rtHold {
	t.Helper()
	log := &holdLog{}
	judge := newHoldJudge(log, verdicts...)
	judge.gate = make(chan struct{})
	gp := map[string]any{"criteria": "The reply talks only about the weather."}
	for k, v := range params {
		gp[k] = v
	}
	s := openCueSession(t, writeIngestionTestPack(t), spokenProvider(), append([]Option{
		WithCueVoice(nil, CueClipCache(cueclip.NewMemoryCache(1<<20))),
		WithGuardrail(guardrails.Output("llm_judge", gp,
			guardrails.WithMessage(holdMessage), guardrails.WithJudge(handlers.NewProviderJudge(judge)))),
	}, opts...)...)
	t.Cleanup(func() { close(judge.gate) })
	return &rtHold{cueSession: s, judge: judge, log: log}
}

// realtimeHold is WithStreamHold for a realtime hold of mode, with every
// sentence a segment of its own.
func realtimeHold(mode RealtimeHold) Option {
	return WithStreamHold(StreamHoldConfig{Realtime: mode, MinChars: 1})
}

// said is a reply chunk carrying only transcript text.
func said(text string) *providers.StreamChunk {
	return &providers.StreamChunk{Delta: text, Metadata: map[string]interface{}{"type": "output_transcription"}}
}

// spoke is a reply chunk carrying only 10 ms of audio, every sample n, so the
// caller's audio can be told apart chunk by chunk.
func spoke(n byte) *providers.StreamChunk {
	return &providers.StreamChunk{MediaData: &providers.StreamMediaData{
		MIMEType: "audio/pcm", Data: spokenAudio(n), SampleRate: cueRate}}
}

func spokenAudio(n byte) []byte { return bytes.Repeat([]byte{n, 0x01}, cueRate/100) }

// heldRead is what the caller read: a trace in order, with each reply audio
// chunk as "audio:N" and runs of cue audio as one "cue-audio" entry.
type heldRead struct{ trace []string }

func (r *heldRead) add(c providers.StreamChunk) {
	if c.Interrupted {
		r.trace = append(r.trace, "interrupted")
	}
	if c.Delta != "" {
		r.trace = append(r.trace, "text:"+c.Delta)
	}
	if c.MediaData != nil && len(c.MediaData.Data) > 0 {
		if kind, _ := c.Metadata["cue"].(string); kind != "" {
			if n := len(r.trace); n == 0 || r.trace[n-1] != "cue-audio" {
				r.trace = append(r.trace, "cue-audio")
			}
		} else {
			r.trace = append(r.trace, fmt.Sprintf("audio:%d", c.MediaData.Data[0]))
		}
	}
	if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
		fr, _ := c.Metadata["finish_reason"].(string)
		r.trace = append(r.trace, "end:"+fr)
	}
}

// withoutCue is trace without its cue audio.
func withoutCue(trace []string) []string {
	var out []string
	for _, e := range trace {
		if e != "cue-audio" {
			out = append(out, e)
		}
	}
	return out
}

func (r *heldRead) has(entry string) bool { return indexOf(r.trace, entry) >= 0 }

func (r *heldRead) ended() bool {
	for _, e := range r.trace {
		if len(e) > 4 && e[:4] == "end:" {
			return true
		}
	}
	return false
}

// until reads Response() until done reports the read complete.
func (s *rtHold) until(t *testing.T, r *heldRead, done func() bool) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for !done() {
		select {
		case c, ok := <-s.ch:
			require.True(t, ok, "the session ended")
			require.NoError(t, c.Error)
			r.add(c)
		case <-timeout:
			require.FailNow(t, "the caller never got it all", "%v", r.trace)
		}
	}
}

// quiet reads whatever reaches the caller within a short while: the stage
// handled every chunk emitted before a judge started, so anything it let
// through is on its way.
func (s *rtHold) quiet(t *testing.T, r *heldRead) {
	t.Helper()
	deadline := time.After(150 * time.Millisecond)
	for {
		select {
		case c, ok := <-s.ch:
			require.True(t, ok)
			require.NoError(t, c.Error)
			r.add(c)
		case <-deadline:
			return
		}
	}
}

func (s *rtHold) emit(chunks ...*providers.StreamChunk) {
	for _, c := range chunks {
		s.sess.EmitChunk(c)
	}
}

// assistants is the conversation's stored assistant messages, once closed.
func (s *rtHold) assistants(t *testing.T) []types.Message {
	t.Helper()
	require.NoError(t, s.conv.Close())
	state, err := s.store.Load(context.Background(), "cue")
	if errors.Is(err, statestore.ErrNotFound) {
		return nil // nothing was stored
	}
	require.NoError(t, err)
	var out []types.Message
	for _, m := range state.Messages {
		if m.Role == "assistant" {
			out = append(out, m)
		}
	}
	return out
}

func failedValidations(m types.Message) int {
	n := 0
	for _, v := range m.Validations {
		if !v.Passed {
			n++
		}
	}
	return n
}

// Off, the default, plays the reply as it arrives: its audio reaches the
// caller before the completed reply is judged, and the session counts it as
// it arrives.
func TestRealtimeHold_OffPlaysTheReplyAsItArrives(t *testing.T) {
	s := openRealtimeHold(t, nil, []bool{true})
	r := &heldRead{}
	s.emit(said("Sunny skies."), spoke(1))
	s.until(t, r, func() bool { return r.has("audio:1") })
	s.emit(endChunk())
	assert.Equal(t, "Sunny skies.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	s.until(t, r, r.ended)

	assert.Equal(t, []string{"text:Sunny skies.", "audio:1", "end:stop"}, r.trace)
	assert.False(t, s.sess.PlayoutHeld(), "the session counts audio as it arrives")
}

// A turn hold keeps the whole reply, transcript and audio, from the caller
// until the completed reply passes, then releases it in the order it arrived.
// The session counts exactly the released audio as played.
func TestRealtimeHold_TurnHoldsTheReplyUntilItPasses(t *testing.T) {
	s := openRealtimeHold(t, nil, []bool{true}, realtimeHold(RealtimeHoldTurn))
	r := &heldRead{}
	s.emit(said("Sunny skies."), spoke(1), said(" Mild later."), spoke(2), endChunk())
	assert.Equal(t, "Sunny skies. Mild later.", recvWithin(t, s.judge.started))
	s.quiet(t, r)
	assert.Empty(t, r.trace, "nothing reaches the caller before the verdict")
	assert.Empty(t, s.sess.ReleasedAudio(), "nothing has played")

	letThrough(t, s.judge.gate)
	s.until(t, r, r.ended)

	assert.Equal(t, []string{"text:Sunny skies.", "audio:1", "text: Mild later.", "audio:2", "end:stop"}, r.trace)
	assert.True(t, s.sess.PlayoutHeld(), "the session counts audio only as it is released")
	assert.Equal(t, append(spokenAudio(1), spokenAudio(2)...), s.sess.ReleasedAudio())
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Sunny skies. Mild later.", msgs[0].Content)
	assert.Len(t, s.judge.calls(), 1, "the completed reply is judged once")
}

// A reply the turn hold's judge fails is never heard: the caller hears the
// guardrail's message, and the transcript is the message alone.
func TestRealtimeHold_TurnFailPlaysOnlyTheMessage(t *testing.T) {
	s := openRealtimeHold(t, nil, []bool{false}, realtimeHold(RealtimeHoldTurn))
	r := &heldRead{}
	s.emit(said("Pineapple pizza."), spoke(1), endChunk())
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.ended() && r.has("cue-audio") })

	// The cue plays the message as the turn ends; the two can arrive in
	// either order.
	assert.Equal(t, []string{"interrupted", "text:" + holdMessage, "end:safety"}, withoutCue(r.trace))
	assert.Empty(t, s.sess.ReleasedAudio(), "the dropped audio never played")
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, holdMessage, msgs[0].Content)
	assert.Equal(t, types.FinishReasonSafety, msgs[0].FinishReason)
	assert.Equal(t, 1, failedValidations(msgs[0]))
}

// A session whose turn completes only when the reply would have finished
// playing reports the end of the reply's content first (GenerationComplete):
// the turn hold judges and releases the reply then, once, not at the turn's
// completion.
func TestRealtimeHold_TurnReleasesWhenGenerationCompletes(t *testing.T) {
	s := openRealtimeHold(t, nil, []bool{true}, realtimeHold(RealtimeHoldTurn))
	r := &heldRead{}
	s.emit(said("Sunny skies."), spoke(1), &providers.StreamChunk{GenerationComplete: true})
	assert.Equal(t, "Sunny skies.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("audio:1") })
	assert.Equal(t, []string{"text:Sunny skies.", "audio:1"}, r.trace, "released before the turn completes")

	s.emit(&providers.StreamChunk{GenerationComplete: true}, endChunk())
	s.until(t, r, r.ended)
	assert.Equal(t, []string{"text:Sunny skies.", "audio:1", "end:stop"}, r.trace)
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Sunny skies.", msgs[0].Content)
	assert.Len(t, s.judge.calls(), 1, "the reply is not judged again at the turn's completion")
}

// A reply the judge fails when its generation completes is cut then: the
// caller hears the message, and the turn ends blocked when it completes.
func TestRealtimeHold_TurnFailWhenGenerationCompletes(t *testing.T) {
	s := openRealtimeHold(t, nil, []bool{false}, realtimeHold(RealtimeHoldTurn))
	r := &heldRead{}
	s.emit(said("Pineapple pizza."), spoke(1), &providers.StreamChunk{GenerationComplete: true})
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("cue-audio") })
	s.emit(endChunk())
	s.until(t, r, r.ended)

	assert.Equal(t, []string{"interrupted", "text:" + holdMessage, "cue-audio", "end:safety"}, r.trace)
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, holdMessage, msgs[0].Content)
	assert.Equal(t, 1, failedValidations(msgs[0]))
	assert.Len(t, s.judge.calls(), 1)
}

// A segment hold releases a segment's audio once the judge passed the
// transcript that covers it: the audio that arrived before that transcript
// was complete. The rest is released with the completed reply.
func TestRealtimeHold_SegmentReleasesAudioAsItsTranscriptPasses(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true, true},
		realtimeHold(RealtimeHoldSegment))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies."), spoke(2), said(" Rain later."))
	assert.Equal(t, "Sunny skies.", recvWithin(t, s.judge.started))
	s.quiet(t, r)
	assert.Empty(t, r.trace, "nothing before the segment's verdict")

	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("text:Sunny skies.") })
	s.quiet(t, r)
	assert.Equal(t, []string{"audio:1", "text:Sunny skies."}, r.trace,
		"audio that arrived after the segment's transcript waits")

	s.emit(spoke(3), endChunk())
	assert.Equal(t, "Sunny skies. Rain later.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	s.until(t, r, r.ended)
	assert.Equal(t, []string{
		"audio:1", "text:Sunny skies.", "audio:2", "text: Rain later.", "audio:3", "end:stop",
	}, r.trace)
	assert.Equal(t, append(append(spokenAudio(1), spokenAudio(2)...), spokenAudio(3)...), s.sess.ReleasedAudio())
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Sunny skies. Rain later.", msgs[0].Content)
}

// A segment the judge fails mid-reply cuts the reply: the caller heard the
// segments already released, then hears the message, and the held audio is
// dropped. The transcript is what was heard: the released prefix and the
// message.
func TestRealtimeHold_SegmentFailKeepsTheHeardPrefix(t *testing.T) {
	speech := newCueTTS(cueRate / 10)
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true, false},
		realtimeHold(RealtimeHoldSegment), WithCueVoice(speech, CueClipCache(cueclip.NewMemoryCache(1<<20))))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies."), spoke(2), said(" Pineapple pizza."))
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("text:Sunny skies.") })
	s.emit(spoke(3), said(" Snow soon."))
	assert.Equal(t, "Sunny skies. Pineapple pizza.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("cue-audio") })
	s.emit(spoke(4), endChunk())
	s.until(t, r, r.ended)

	assert.Equal(t, []string{
		"audio:1", "text:Sunny skies.", "interrupted", "text: " + holdMessage, "cue-audio", "end:safety",
	}, r.trace)
	assert.Equal(t, spokenAudio(1), s.sess.ReleasedAudio(), "only the released segment played")
	assert.Equal(t, 1, s.sess.Interrupts(), "the model is asked to stop the reply")
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Sunny skies. "+holdMessage, msgs[0].Content)
	assert.Equal(t, types.FinishReasonSafety, msgs[0].FinishReason)
	assert.Equal(t, 1, failedValidations(msgs[0]))
	assert.Equal(t, spokenAudio(1), storedAudio(t, msgs[0]), "the stored audio is what was heard")
	assert.Equal(t, 1, speech.calls(holdMessage), "the cue speaks the message")
}

// storedAudio is the audio a stored message keeps.
func storedAudio(t *testing.T, m types.Message) []byte {
	t.Helper()
	var out []byte
	for _, p := range m.Parts {
		if p.Type == types.ContentTypeAudio && p.Media != nil && p.Media.Data != nil {
			raw, err := base64.StdEncoding.DecodeString(*p.Media.Data)
			require.NoError(t, err)
			out = append(out, raw...)
		}
	}
	return out
}

// The caller barging in while the reply is held drops what is held: it was
// never heard, so it is not in the transcript, and nothing is judged.
func TestRealtimeHold_BargeInDropsTheHeldReply(t *testing.T) {
	s := openRealtimeHold(t, nil, []bool{true}, realtimeHold(RealtimeHoldTurn))
	r := &heldRead{}
	s.emit(said("Sunny skies."), spoke(1), &providers.StreamChunk{Interrupted: true})
	s.until(t, r, func() bool { return r.has("interrupted") })
	s.quiet(t, r)

	assert.Equal(t, []string{"interrupted"}, r.trace)
	assert.Empty(t, s.sess.ReleasedAudio())
	assert.Empty(t, s.judge.calls())
	assert.Empty(t, s.assistants(t), "no reply was heard")
}

// Barged in on after a segment was released, the reply's transcript is the
// released segment, the rest dropped unheard.
func TestRealtimeHold_BargeInKeepsTheReleasedSegment(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true},
		realtimeHold(RealtimeHoldSegment))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies."), spoke(2), said(" Rain later."))
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("text:Sunny skies.") })
	s.emit(&providers.StreamChunk{Interrupted: true})
	s.until(t, r, r.ended)
	s.quiet(t, r)

	// The cut reply ends its turn, as any reply barge-in cuts short does.
	assert.Equal(t, []string{"audio:1", "text:Sunny skies.", "interrupted", "end:interrupted"}, r.trace)
	assert.Equal(t, spokenAudio(1), s.sess.ReleasedAudio())
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Sunny skies.", msgs[0].Content)
	assert.Equal(t, types.FinishReasonInterrupted, msgs[0].FinishReason)
}

// With a holdback, a segment release keeps back the audio received most
// recently, which a trailing transcript may not cover yet: here the last
// 10 ms before the segment's transcript completed.
func TestRealtimeHold_SegmentHoldbackKeepsRecentAudio(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true, true},
		WithStreamHold(StreamHoldConfig{Realtime: RealtimeHoldSegment, MinChars: 1, Holdback: 10 * time.Millisecond}))
	r := &heldRead{}
	s.emit(spoke(1), spoke(2), said("Sunny skies."), said(" Rain later."))
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("audio:1") })
	s.quiet(t, r)
	assert.Equal(t, []string{"audio:1"}, r.trace, "the last 10 ms and what follows it wait")

	s.emit(endChunk())
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, r.ended)
	assert.Equal(t, []string{"audio:1", "audio:2", "text:Sunny skies.", "text: Rain later.", "end:stop"}, r.trace)
}

// A realtime hold must be one of the modes.
func TestRealtimeHold_RejectsAnUnknownMode(t *testing.T) {
	_, err := OpenDuplex(writeIngestionTestPack(t), "main", WithProvider(spokenProvider()),
		WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithStreamHold(StreamHoldConfig{Realtime: "sentence"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `Realtime "sentence" is not one of "off", "turn" or "segment"`)
}

// A transcript delta that runs past the passed segment waits for the next
// verdict, with the audio after it: not all of its words were judged.
func TestRealtimeHold_SegmentHoldsADeltaRunningPastIt(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true, true},
		realtimeHold(RealtimeHoldSegment))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies. Rain"), spoke(2), said(" later."))
	assert.Equal(t, "Sunny skies.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("audio:1") })
	s.quiet(t, r)
	assert.Equal(t, []string{"audio:1"}, r.trace)

	s.emit(endChunk())
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, r.ended)
	assert.Equal(t, []string{"audio:1", "text:Sunny skies. Rain", "audio:2", "text: later.", "end:stop"}, r.trace)
}

// Segments completed while the judge was busy are judged together as soon as
// it returns, without waiting for more of the reply.
func TestRealtimeHold_SegmentJudgesWhatBecameReadyMeanwhile(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true, true, true},
		realtimeHold(RealtimeHoldSegment))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies."), said(" Rain later."), said(" Snow soon."))
	assert.Equal(t, "Sunny skies.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	assert.Equal(t, "Sunny skies. Rain later.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("text: Rain later.") })
	assert.Equal(t, []string{"audio:1", "text:Sunny skies.", "text: Rain later."}, r.trace)
}

// Two segment guardrails release only what both have passed.
func TestRealtimeHold_SegmentReleasesWhatEveryGuardrailPassed(t *testing.T) {
	log2 := &holdLog{}
	judge2 := newHoldJudge(log2, true, true)
	judge2.gate = make(chan struct{})
	t.Cleanup(func() { close(judge2.gate) })
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true, true},
		realtimeHold(RealtimeHoldSegment),
		WithGuardrail(guardrails.Output("llm_judge", map[string]any{
			"criteria": "The reply is polite.", "stream_hold": "segment",
		}, guardrails.WithMessage(holdMessage), guardrails.WithJudge(handlers.NewProviderJudge(judge2)))))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies."), said(" Rain later."))
	assert.Equal(t, "Sunny skies.", recvWithin(t, s.judge.started))
	assert.Equal(t, "Sunny skies.", recvWithin(t, judge2.started))
	letThrough(t, s.judge.gate)
	s.quiet(t, r)
	assert.Empty(t, r.trace, "the second guardrail has not passed it yet")

	letThrough(t, judge2.gate)
	s.until(t, r, func() bool { return r.has("text:Sunny skies.") })
	assert.Equal(t, []string{"audio:1", "text:Sunny skies."}, r.trace)
}

// A turn hold judges a segment guardrail's reply once, complete: nothing can
// be released before the completed reply passes, so no segment is judged.
func TestRealtimeHold_TurnJudgesASegmentGuardrailOnce(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true},
		realtimeHold(RealtimeHoldTurn))
	r := &heldRead{}
	s.emit(said("Sunny skies."), spoke(1), said(" Rain later."), spoke(2), endChunk())
	assert.Equal(t, "Sunny skies. Rain later.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	s.until(t, r, r.ended)
	assert.Equal(t, []string{"text:Sunny skies.", "audio:1", "text: Rain later.", "audio:2", "end:stop"}, r.trace)
	assert.Len(t, s.judge.calls(), 1)
}

// A reply the provider ends as interrupted (OpenAI Realtime's canceled
// response) after a segment was released keeps only that segment: the held
// rest was never heard.
func TestRealtimeHold_CanceledReplyKeepsTheReleasedSegment(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true, true},
		realtimeHold(RealtimeHoldSegment))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies."), spoke(2), said(" Rain later."))
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("text:Sunny skies.") })
	canceled := types.FinishReasonInterrupted
	s.emit(&providers.StreamChunk{FinishReason: &canceled})
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, r.ended)

	assert.NotContains(t, r.trace, "audio:2")
	assert.NotContains(t, r.trace, "text: Rain later.")
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Sunny skies.", msgs[0].Content)
	assert.Equal(t, spokenAudio(1), storedAudio(t, msgs[0]))
}

// A reply the provider supersedes with another before it completes is never
// released: only the reply that completes is heard.
func TestRealtimeHold_SupersededReplyIsNeverHeard(t *testing.T) {
	s := openRealtimeHold(t, nil, []bool{true}, realtimeHold(RealtimeHoldTurn))
	r := &heldRead{}
	first := said("Sunny skies.")
	first.Generation = 1
	firstAudio := spoke(1)
	firstAudio.Generation = 1
	second := said("Rain later.")
	second.Generation = 2
	secondAudio := spoke(2)
	secondAudio.Generation = 2
	end := endChunk()
	end.Generation = 2
	s.emit(first, firstAudio, second, secondAudio, end)
	assert.Equal(t, "Rain later.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	s.until(t, r, r.ended)

	assert.Equal(t, []string{"text:Rain later.", "audio:2", "end:stop"}, r.trace)
	assert.Equal(t, spokenAudio(2), s.sess.ReleasedAudio())
}

// A chunk check that cuts a held reply drops what is held, and the segment
// judgment still running is canceled: its verdict can release nothing.
func TestRealtimeHold_ChunkCutDropsTheHeldReply(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true},
		realtimeHold(RealtimeHoldSegment),
		WithGuardrail(guardrails.Output("content_excludes", map[string]any{"patterns": []any{"Pineapple"}},
			guardrails.WithMessage("No fruit."))))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies."), said(" Rain"))
	assert.Equal(t, "Sunny skies.", recvWithin(t, s.judge.started))
	s.emit(said(" Pineapple"))
	s.until(t, r, func() bool { return r.has("text:No fruit.") })
	// A judgment still running would pass now; a canceled one takes nothing.
	select {
	case s.judge.gate <- struct{}{}:
	case <-time.After(200 * time.Millisecond):
	}
	s.quiet(t, r)
	s.emit(spoke(2), endChunk())
	s.until(t, r, r.ended)
	s.quiet(t, r)

	assert.Equal(t, []string{"interrupted", "text:No fruit.", "cue-audio", "end:safety"}, r.trace)
	assert.Empty(t, s.sess.ReleasedAudio())
}

// A segment judgment still running when the reply completes decides before
// the completed reply's check: its firing cuts the reply, and the completed
// reply is not judged again.
func TestRealtimeHold_SegmentVerdictRunningAtTheEndDecidesFirst(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true, false},
		realtimeHold(RealtimeHoldSegment))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies."), said(" Pineapple pizza."))
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("text:Sunny skies.") })
	s.emit(said(" Rain."))
	assert.Equal(t, "Sunny skies. Pineapple pizza.", recvWithin(t, s.judge.started))
	last := spoke(2)
	stop := "stop"
	last.FinishReason = &stop
	s.emit(last)
	s.quiet(t, r)
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.ended() && r.has("cue-audio") })

	// The cue plays the message as the turn ends; the two can arrive in
	// either order.
	assert.Equal(t, []string{
		"audio:1", "text:Sunny skies.", "interrupted", "text: " + holdMessage, "end:safety",
	}, withoutCue(r.trace))
	assert.Len(t, s.judge.calls(), 2, "the completed reply is not judged again")
	assert.Equal(t, spokenAudio(1), s.sess.ReleasedAudio())
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Sunny skies. "+holdMessage, msgs[0].Content)
	assert.Equal(t, spokenAudio(1), storedAudio(t, msgs[0]))
}

// Once the reply is complete, a segment judgment that passes starts no other:
// the completed reply's check judges the rest.
func TestRealtimeHold_CompletedReplyJudgesWhatSegmentsDidNot(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true, true},
		realtimeHold(RealtimeHoldSegment))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies."), said(" Rain later."))
	assert.Equal(t, "Sunny skies.", recvWithin(t, s.judge.started))
	s.emit(said(" Snow soon."), spoke(2), endChunk())
	s.quiet(t, r)
	letThrough(t, s.judge.gate)
	assert.Equal(t, "Sunny skies. Rain later. Snow soon.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	s.until(t, r, r.ended)

	assert.Equal(t, []string{
		"audio:1", "text:Sunny skies.", "text: Rain later.", "text: Snow soon.", "audio:2", "end:stop",
	}, r.trace)
	assert.Len(t, s.judge.calls(), 2)
}

// A segment judgment that runs past its bound fails closed: the held reply is
// never heard, the caller hears the message, and the stored turn says why.
func TestRealtimeHold_SegmentTimeoutFailsClosed(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true},
		WithStreamHold(StreamHoldConfig{Realtime: RealtimeHoldSegment, MinChars: 1, SegmentTimeout: 20 * time.Millisecond}))
	r := &heldRead{}
	s.emit(spoke(1), said("Sunny skies."), said(" Rain later."))
	recvWithin(t, s.judge.started) // never let through: the judge never answers
	s.until(t, r, func() bool { return r.has("cue-audio") })
	s.emit(spoke(2), endChunk())
	s.until(t, r, r.ended)

	assert.Equal(t, []string{"interrupted", "text:" + holdMessage, "cue-audio", "end:safety"}, r.trace)
	assert.Empty(t, s.sess.ReleasedAudio(), "nothing held was heard")
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, holdMessage, msgs[0].Content)
	assert.Equal(t, types.FinishReasonSafety, msgs[0].FinishReason)
	require.Len(t, msgs[0].Validations, 1)
	assert.Equal(t, "timeout", msgs[0].Validations[0].Details["reason"])
}

// A caller who starts speaking while a reply that finished generating is
// still held for its judge speaks over it: the session reports it, and the
// reply is never released, though the judge then passes it. The stored turn
// is what was heard of it: nothing.
func TestRealtimeHold_CallerSpeakingOverTheHeldReplyDropsIt(t *testing.T) {
	s := openRealtimeHold(t, nil, []bool{true}, realtimeHold(RealtimeHoldTurn))
	r := &heldRead{}
	s.emit(said("Sunny skies."), spoke(1), endChunk())
	recvWithin(t, s.judge.started)
	s.sess.InterruptHeldAudio()
	s.emit(&providers.StreamChunk{Interrupted: true})
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("interrupted") })
	s.quiet(t, r)

	assert.NotContains(t, r.trace, "text:Sunny skies.")
	assert.NotContains(t, r.trace, "audio:1")
	assert.NotContains(t, r.trace, "end:stop", "no turn of the reply ends: none was heard")
	assert.Empty(t, s.sess.ReleasedAudio(), "none of the reply played")
	assert.Positive(t, s.sess.HeldDrops(), "the session is told the held reply was dropped")
	for _, m := range s.assistants(t) {
		assert.Empty(t, m.Content, "nothing of the reply was heard")
		assert.Empty(t, storedAudio(t, m))
	}
}

// A judge that errors fails closed under either hold: the caller hears the
// guardrail's message and none of the reply.
func TestRealtimeHold_JudgeErrorFailsClosed(t *testing.T) {
	for name, tc := range map[string]struct {
		params map[string]any
		mode   RealtimeHold
	}{
		"turn":    {nil, RealtimeHoldTurn},
		"segment": {map[string]any{"stream_hold": "segment"}, RealtimeHoldSegment},
	} {
		t.Run(name, func(t *testing.T) {
			s := openRealtimeHold(t, tc.params, nil, realtimeHold(tc.mode))
			s.judge.fail = errors.New("judge unavailable")
			r := &heldRead{}
			s.emit(spoke(1), said("Sunny skies."), said(" Rain later."), spoke(2), endChunk())
			recvWithin(t, s.judge.started)
			letThrough(t, s.judge.gate)
			s.until(t, r, func() bool { return r.ended() && r.has("cue-audio") })

			assert.Equal(t, []string{"interrupted", "text:" + holdMessage, "end:safety"}, withoutCue(r.trace))
			assert.Empty(t, s.sess.ReleasedAudio())
			msgs := s.assistants(t)
			require.Len(t, msgs, 1)
			assert.Equal(t, holdMessage, msgs[0].Content)
			assert.Equal(t, types.FinishReasonSafety, msgs[0].FinishReason)
		})
	}
}

// A reply with a transcript and no audio is held and released like any
// other, its text a segment at a time.
func TestRealtimeHold_TranscriptOnlyReply(t *testing.T) {
	s := openRealtimeHold(t, map[string]any{"stream_hold": "segment"}, []bool{true, true},
		realtimeHold(RealtimeHoldSegment))
	r := &heldRead{}
	s.emit(said("Sunny skies."), said(" Rain later."))
	assert.Equal(t, "Sunny skies.", recvWithin(t, s.judge.started))
	letThrough(t, s.judge.gate)
	s.until(t, r, func() bool { return r.has("text:Sunny skies.") })
	s.emit(endChunk())
	recvWithin(t, s.judge.started)
	letThrough(t, s.judge.gate)
	s.until(t, r, r.ended)

	assert.Equal(t, []string{"text:Sunny skies.", "text: Rain later.", "end:stop"}, r.trace)
	msgs := s.assistants(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Sunny skies. Rain later.", msgs[0].Content)
}
