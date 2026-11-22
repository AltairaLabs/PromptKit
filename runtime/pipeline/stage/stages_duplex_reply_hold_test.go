package stage

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A guardrail's message after a held reply's released start: the turn reads
// as the caller heard it, the heard words then the message, joined by one
// space; with nothing heard the message replaces the turn's text.
func TestHeardAndMessage(t *testing.T) {
	for _, tc := range []struct {
		heard, message, content, live string
		replaces                      bool
	}{
		{"", "No.", "No.", "No.", true},
		{"Sunny skies.", "No.", "Sunny skies. No.", " No.", false},
		{"Sunny skies. ", "No.", "Sunny skies. No.", "No.", false},
		{"Sunny skies.\n", "No.", "Sunny skies.\nNo.", "No.", false},
		{"Sunny skies.", "", "", "", true},
	} {
		content, live, replaces := heardAndMessage(tc.heard, tc.message)
		assert.Equal(t, tc.content, content, "%q + %q", tc.heard, tc.message)
		assert.Equal(t, tc.live, live, "%q + %q", tc.heard, tc.message)
		assert.Equal(t, tc.replaces, replaces, "%q + %q", tc.heard, tc.message)
	}
}

// A blocked held reply's message keeps only the audio the caller heard.
func TestKeepHeardAudio(t *testing.T) {
	audio := func(b []byte) types.ContentPart {
		d := base64.StdEncoding.EncodeToString(b)
		return types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{Data: &d}}
	}
	text := "hi"
	msg := &types.Message{Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: &text}, audio([]byte{1, 2, 3, 4})}}
	keepHeardAudio(msg, 2)
	assert.Len(t, msg.Parts, 2)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte{1, 2}), *msg.Parts[1].Media.Data)

	keepHeardAudio(msg, 0)
	assert.Len(t, msg.Parts, 1, "no audio heard: no audio part")
	assert.Equal(t, types.ContentTypeText, msg.Parts[0].Type)
	keepHeardAudio(nil, 0)
}

// The stage-level behavior of the realtime hold; the SDK's
// realtime_stream_hold_test.go drives it end to end with gated judges.

// replyHoldHook is an output guardrail that holds replies (hooks.StreamHolder):
// it fails a reply, partial or complete, whose text contains failOn.
type replyHoldHook struct {
	mode    hooks.StreamHoldMode
	failOn  string
	mu      sync.Mutex
	judged  []string // what each judgment saw, partial or complete
	partial int
	// callerSaid is the caller's latest turn the last AfterCall saw.
	callerSaid string
}

func (h *replyHoldHook) Name() string                     { return "hold" }
func (h *replyHoldHook) StreamHold() hooks.StreamHoldMode { return h.mode }
func (h *replyHoldHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *replyHoldHook) AfterCall(_ context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
	h.mu.Lock()
	h.callerSaid = ""
	for _, m := range req.Messages {
		if m.Role == core.RoleUser {
			h.callerSaid = m.Content
		}
	}
	h.mu.Unlock()
	return h.judge(resp.Message.Content, false)
}

// lastCallerSaid is the caller's latest turn in the history the last
// completed-reply check saw.
func (h *replyHoldHook) lastCallerSaid() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.callerSaid
}

func (h *replyHoldHook) JudgePartialReply(_ context.Context, _ *hooks.ProviderRequest, r hooks.PartialReply) hooks.Decision {
	return h.judge(r.Text, true)
}

func (h *replyHoldHook) judge(text string, partial bool) hooks.Decision {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.judged = append(h.judged, text)
	if partial {
		h.partial++
	}
	if h.failOn != "" && strings.Contains(text, h.failOn) {
		return hooks.Deny("off topic")
	}
	return hooks.Allow
}

func (h *replyHoldHook) calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.judged...)
}

// heldPlayoutSession is a recordingSession that counts released audio
// (providers.PlayoutHolder) and reports a transcript lag.
type heldPlayoutSession struct {
	*recordingSession
	lag      time.Duration
	held     bool
	released int
	// interrupted is what HeldAudioInterrupted reports: the caller spoke over
	// held audio. HeldAudioDropped clears it and counts dropped.
	interrupted bool
	dropped     int
	droppedGens []int64
	// interruptAfter, when set, has the caller speak (interrupted) once that
	// many audio chunks were released.
	interruptAfter int
}

func (p *heldPlayoutSession) HoldPlayout() { p.held = true }
func (p *heldPlayoutSession) ReleaseAudio(c *providers.StreamChunk) bool {
	if p.interrupted {
		return false
	}
	p.released += len(c.MediaData.Data)
	if p.interruptAfter > 0 {
		if p.interruptAfter--; p.interruptAfter == 0 {
			p.interrupted = true
		}
	}
	return true
}
func (p *heldPlayoutSession) OutputTranscriptLag() time.Duration { return p.lag }
func (p *heldPlayoutSession) HeldAudioInterrupted(int64) bool    { return p.interrupted }
func (p *heldPlayoutSession) HeldAudioDropped(gen int64) {
	p.interrupted = false
	p.dropped++
	p.droppedGens = append(p.droppedGens, gen)
}

// holdStage is a duplex stage holding replies in mode for hook, on a
// heldPlayoutSession.
func replyHoldStage(t *testing.T, mode RealtimeHold, hook *replyHoldHook, lag time.Duration) (*DuplexProviderStage, *heldPlayoutSession) {
	t.Helper()
	s, rec, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(hook)))
	sess := &heldPlayoutSession{recordingSession: rec, lag: lag}
	s.session = sess
	s.SetStreamHold(StreamHoldConfig{Realtime: mode, MinChars: 1})
	s.holdPlayout()
	return s, sess
}

func TestReplyHold_TurnReleasesAPassedReplyInOrder(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
	s, sess := replyHoldStage(t, RealtimeHoldTurn, hook, 0)
	elems, err := feedChunks(t, s, transcriptChunk("Sunny skies.", pcm(10)), transcriptChunk(" Mild.", pcm(20)), stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{"text:Sunny skies.", "audio:10", "text: Mild.", "audio:20", "end:Sunny skies. Mild.|stop"},
		trace(elems))
	assert.True(t, sess.held)
	assert.Equal(t, 30, sess.released)
	assert.Equal(t, []string{"Sunny skies. Mild."}, hook.calls())
}

func TestReplyHold_TurnDropsAFailedReply(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldTurn, failOn: "Bruno"}
	s, sess := replyHoldStage(t, RealtimeHoldTurn, hook, 0)
	elems, err := feedChunks(t, s, transcriptChunk("About Bruno.", pcm(10)), stopChunk())
	require.NoError(t, err)

	msg := prompt.DefaultBlockedMessage
	assert.Equal(t, []string{"interrupt", "text:" + msg, "end:" + msg + "|safety"}, trace(elems))
	assert.Zero(t, sess.released)
	m, _ := turnMessage(t, elems)
	for _, p := range m.Parts {
		assert.NotEqual(t, types.ContentTypeAudio, p.Type, "no audio was heard")
	}
}

func TestReplyHold_GenerationCompleteReleasesBeforeTheTurnEnds(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
	s, sess := replyHoldStage(t, RealtimeHoldTurn, hook, 0)
	elems, err := feedChunks(t, s,
		transcriptChunk("Sunny skies.", pcm(10)),
		providers.StreamChunk{GenerationComplete: true},
		transcriptChunk("", pcm(5)), // audio after the verdict plays as it arrives
		stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{"text:Sunny skies.", "audio:10", "audio:5", "end:Sunny skies.|stop"}, trace(elems))
	assert.Equal(t, 15, sess.released)
	assert.Equal(t, []string{"Sunny skies."}, hook.calls(), "judged once")
}

func TestReplyHold_GenerationCompleteCutsAFailedReply(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldTurn, failOn: "Bruno"}
	s, _ := replyHoldStage(t, RealtimeHoldTurn, hook, 0)
	elems, err := feedChunks(t, s,
		transcriptChunk("About Bruno.", pcm(10)),
		providers.StreamChunk{GenerationComplete: true},
		transcriptChunk(" More.", pcm(5)),
		stopChunk())
	require.NoError(t, err)

	msg := prompt.DefaultBlockedMessage
	assert.Equal(t, []string{"interrupt", "text:" + msg, "end:" + msg + "|safety"}, trace(elems))
	assert.Len(t, hook.calls(), 1)
}

func TestReplyHold_BargeInDropsTheHeldReply(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
	s, sess := replyHoldStage(t, RealtimeHoldTurn, hook, 0)
	elems, err := feedChunks(t, s, transcriptChunk("Sunny skies.", pcm(10)), providers.StreamChunk{Interrupted: true})
	require.NoError(t, err)

	for _, e := range trace(elems) {
		assert.NotContains(t, e, "text:")
		assert.NotContains(t, e, "audio:")
	}
	assert.Zero(t, sess.released)
	assert.Empty(t, hook.calls())
}

func TestReplyHold_SegmentReleasesThenTheCompletedReply(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldSegment}
	s, sess := replyHoldStage(t, RealtimeHoldSegment, hook, 0)
	elems, err := feedChunks(t, s,
		transcriptChunk("Sunny skies.", pcm(10)), transcriptChunk(" Rain later.", pcm(20)), stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sunny skies.", "audio:10", "text: Rain later.", "audio:20", "end:Sunny skies. Rain later.|stop",
	}, trace(elems))
	assert.Equal(t, 30, sess.released)
	assert.Equal(t, []string{"Sunny skies.", "Sunny skies. Rain later."}, hook.calls())
}

func TestReplyHold_SegmentFailCutsTheReply(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldSegment, failOn: "Sunny"}
	s, sess := replyHoldStage(t, RealtimeHoldSegment, hook, time.Second)
	elems, err := feedChunks(t, s,
		transcriptChunk("Sunny skies.", pcm(10)), transcriptChunk(" Rain later.", pcm(20)), stopChunk())
	require.NoError(t, err)

	msg := prompt.DefaultBlockedMessage
	assert.Equal(t, []string{"interrupt", "text:" + msg, "end:" + msg + "|safety"}, trace(elems))
	assert.Zero(t, sess.released)
	assert.Equal(t, []string{"Sunny skies."}, hook.calls(), "the completed reply is not judged again")
	assert.Nil(t, s.replyHold)
}

// Off, a holding guardrail holds nothing, and the session counts audio as it
// arrives.
func TestReplyHold_OffHoldsNothing(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
	s, sess := replyHoldStage(t, RealtimeHoldOff, hook, 0)
	elems, err := feedChunks(t, s, transcriptChunk("Sunny skies.", pcm(10)), stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{"text:Sunny skies.", "audio:10", "end:Sunny skies.|stop"}, trace(elems))
	assert.False(t, sess.held)
}
