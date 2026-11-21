package stage

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// What a segment hold releases, and when: each test feeds a realtime reply
// chunk by chunk and records what the caller got after each chunk, the
// segment judgments that chunk started having answered.

// gatedHoldHook is a segment guardrail whose partial-reply judgments wait
// for the test (open), and that fails a completed reply containing failOn.
type gatedHoldHook struct {
	replyHoldHook
	gate chan struct{}
}

func (h *gatedHoldHook) JudgePartialReply(ctx context.Context, req *hooks.ProviderRequest, r hooks.PartialReply) hooks.Decision {
	select {
	case <-h.gate:
	case <-ctx.Done():
		return hooks.Allow
	}
	return h.replyHoldHook.JudgePartialReply(ctx, req, r)
}

// released is a test's run of a held reply: what the caller got after each
// chunk fed, and the stream.hold.cut events.
type released struct {
	t    *testing.T
	s    *DuplexProviderStage
	out  chan StreamElement
	mu   sync.Mutex
	cuts []*events.StreamHoldCutData
}

// newReleased is a duplex stage holding replies by segment for hooks, on a
// session reporting lag, with cfg's holdback.
func newReleased(t *testing.T, cfg StreamHoldConfig, lag time.Duration, hs ...hooks.ProviderHook) (*released, *heldPlayoutSession) {
	t.Helper()
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	r := &released{t: t, out: make(chan StreamElement, 256)}
	bus.Subscribe(events.EventStreamHoldCut, func(e *events.Event) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.cuts = append(r.cuts, e.Data.(*events.StreamHoldCutData))
	})
	emitter := events.NewEmitter(bus, "run1", "sess1", "conv1")
	var opts []hooks.Option
	for _, h := range hs {
		opts = append(opts, hooks.WithProviderHook(h))
	}
	s, rec, _ := duplexStageWithHooks(t, hooks.NewRegistry(opts...))
	s.emitter = emitter
	s.hookRegistry.SetEmitter(emitter)
	sess := &heldPlayoutSession{recordingSession: rec, lag: lag}
	s.session = sess
	if cfg.MinChars == 0 {
		cfg.MinChars = 1
	}
	s.SetStreamHold(cfg)
	s.holdPlayout()
	r.s = s
	return r, sess
}

// feed handles chunk, answers the segment judgments it started that have
// answered within a short while, and returns what the caller got.
func (r *released) feed(chunk providers.StreamChunk) []string {
	r.t.Helper()
	r.handle(chunk)
	r.settle()
	return r.drain()
}

// settle applies the segment verdicts that arrive within a short while.
func (r *released) settle() {
	r.t.Helper()
	ctx := context.Background()
	for {
		ch := r.s.replyHoldVerdicts()
		if ch == nil {
			return
		}
		select {
		case v := <-ch:
			require.NoError(r.t, r.s.onReplyVerdict(ctx, v, r.out))
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}

// handle handles chunk, failing the test rather than hanging when the stage
// blocks on it.
func (r *released) handle(chunk providers.StreamChunk) {
	r.t.Helper()
	err := within(r.t, "handling a chunk", func() error {
		return r.s.handleResponseChunk(context.Background(), &chunk, r.out)
	})
	require.NoError(r.t, err)
}

func (r *released) drain() []string {
	var elems []StreamElement
	for {
		select {
		case e := <-r.out:
			elems = append(elems, e)
		default:
			return trace(elems)
		}
	}
}

func (r *released) cutEvents() []*events.StreamHoldCutData {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*events.StreamHoldCutData(nil), r.cuts...)
}

func spokenChunk(n int) providers.StreamChunk     { return transcriptChunk("", pcm(n)) }
func saidChunk(text string) providers.StreamChunk { return transcriptChunk(text, nil) }

// A segment's audio goes once its transcript passes: the audio that arrived
// before the transcript completing it. Later audio waits for the next
// verdict, or the completed reply's.
func TestReplyRelease_SegmentReleasesTheAudioBeforeItsTranscript(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldSegment}
	r, sess := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, hook)

	assert.Empty(t, r.feed(spokenChunk(10)))
	assert.Empty(t, r.feed(saidChunk("Sunny skies.")))
	assert.Empty(t, r.feed(spokenChunk(20)))
	assert.Equal(t, []string{"audio:10", "text:Sunny skies."}, r.feed(saidChunk(" Rain later.")))
	assert.Empty(t, r.feed(spokenChunk(30)))
	assert.Equal(t, []string{"audio:20", "text: Rain later.", "audio:30", "end:Sunny skies. Rain later.|stop"},
		r.feed(stopChunk()))
	assert.Equal(t, 60, sess.released)
}

// A transcript delta that runs past the passed segment waits, and so does
// the audio after it.
func TestReplyRelease_ADeltaRunningPastTheSegmentWaits(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldSegment}
	r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, hook)

	assert.Empty(t, r.feed(spokenChunk(10)))
	assert.Equal(t, []string{"audio:10"}, r.feed(saidChunk("Sunny skies. Rain")))
	assert.Equal(t, []string{"Sunny skies."}, hook.calls())
}

// The holdback keeps back the audio received last before the passed
// transcript: the session's own figure, unless the host sets one.
func TestReplyRelease_HoldbackKeepsRecentAudio(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg StreamHoldConfig
		lag time.Duration
	}{
		"the session's lag": {StreamHoldConfig{Realtime: RealtimeHoldSegment}, 10 * time.Millisecond},
		"the host's":        {StreamHoldConfig{Realtime: RealtimeHoldSegment, Holdback: 10 * time.Millisecond}, time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			hook := &replyHoldHook{mode: hooks.StreamHoldSegment}
			r, _ := newReleased(t, tc.cfg, tc.lag, hook)
			r.feed(spokenChunk(480)) // 10 ms at 24 kHz
			r.feed(spokenChunk(482))
			r.feed(saidChunk("Sunny skies."))
			assert.Equal(t, []string{"audio:480"}, r.feed(saidChunk(" Rain later.")))
		})
	}
}

// Two segment guardrails release only what both passed.
func TestReplyRelease_WhatEveryGuardrailPassed(t *testing.T) {
	fast := &replyHoldHook{mode: hooks.StreamHoldSegment}
	slow := &gatedHoldHook{replyHoldHook: replyHoldHook{mode: hooks.StreamHoldSegment}, gate: make(chan struct{})}
	r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, fast, slow)

	r.feed(spokenChunk(10))
	r.feed(saidChunk("Sunny skies."))
	assert.Empty(t, r.feed(saidChunk(" Rain later.")), "one guardrail has not passed it")
	close(slow.gate)
	r.settle()
	assert.Equal(t, []string{"audio:10", "text:Sunny skies."}, r.drain())
}

// Segments completed while a judgment ran are judged together as soon as it
// returns.
func TestReplyRelease_JudgesWhatBecameReadyMeanwhile(t *testing.T) {
	hook := &gatedHoldHook{replyHoldHook: replyHoldHook{mode: hooks.StreamHoldSegment}, gate: make(chan struct{}, 4)}
	r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, hook)

	r.feed(saidChunk("Sunny skies. Rain"))
	r.feed(saidChunk(" later. Snow"))
	hook.gate <- struct{}{}
	hook.gate <- struct{}{}
	r.settle()
	assert.Equal(t, []string{"Sunny skies.", "Sunny skies. Rain later."}, hook.calls())
	assert.Equal(t, []string{"text:Sunny skies. Rain"}, r.drain(), "the next delta runs past the second segment")
}

// A failing segment cuts the reply, reports the cut, and keeps what was
// heard in the turn's text, followed by the message.
func TestReplyRelease_SegmentFailKeepsTheHeardPrefix(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldSegment, failOn: "Bruno"}
	r, sess := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, hook)
	msg := prompt.DefaultBlockedMessage

	r.feed(spokenChunk(10))
	r.feed(saidChunk("Sunny skies."))
	assert.Equal(t, []string{"audio:10", "text:Sunny skies."}, r.feed(saidChunk(" Bruno sings.")))
	r.feed(spokenChunk(20))
	assert.Equal(t, []string{"interrupt", "text: " + msg}, r.feed(saidChunk(" Rain.")))
	assert.Equal(t, []string{"end:Sunny skies. " + msg + "|safety"}, r.feed(stopChunk()))
	assert.Equal(t, 10, sess.released)
	require.Eventually(t, func() bool { return len(r.cutEvents()) == 1 }, 5*time.Second, time.Millisecond)
	cut := r.cutEvents()[0]
	assert.Equal(t, 2, cut.SegmentIndex)
	assert.Equal(t, len("Sunny skies."), cut.ReleasedBytes)
}

// The completed reply failing after segments were released keeps what was
// heard, followed by the message.
func TestReplyRelease_CompletedReplyFailKeepsTheHeardPrefix(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldSegment, failOn: "Bruno"}
	r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, hook)
	msg := prompt.DefaultBlockedMessage

	assert.Empty(t, r.feed(saidChunk("Sunny skies.")))
	assert.Equal(t, []string{"text:Sunny skies."}, r.feed(saidChunk(" Rain later.")),
		"a segment whose transcript is the first held piece goes once it passes")
	assert.Equal(t, []string{"text: Rain later."}, r.feed(saidChunk(" Bruno")))
	assert.Equal(t, []string{"interrupt", "text: " + msg, "end:Sunny skies. Rain later. " + msg + "|safety"},
		r.feed(stopChunk()))
}

// A chunk whose text and audio the hold took sends nothing, not even an
// empty element.
func TestReplyRelease_AHeldChunkSendsNothing(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldSegment}
	r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, hook)
	r.handle(spokenChunk(10))
	r.handle(saidChunk("Sunny"))
	assert.Empty(t, r.out, "nothing was sent")
}

// A turn hold releases a reply when its generation completes, before its
// turn does, once the completed reply passes.
func TestReplyRelease_GenerationCompleteReleasesAtOnce(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
	r, sess := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldTurn}, 0, hook)
	assert.Empty(t, r.feed(transcriptChunk("Sunny skies.", pcm(10))))
	assert.Equal(t, []string{"text:Sunny skies.", "audio:10"}, r.feed(providers.StreamChunk{GenerationComplete: true}))
	assert.Equal(t, 10, sess.released)
	assert.Equal(t, []string{"Sunny skies."}, hook.calls())
}

// partialFailHook is a segment guardrail that answers each partial-reply
// judgment after delay, failing it, and passes the completed reply.
type partialFailHook struct {
	replyHoldHook
	delay time.Duration
}

func (h *partialFailHook) JudgePartialReply(ctx context.Context, req *hooks.ProviderRequest, r hooks.PartialReply) hooks.Decision {
	h.replyHoldHook.JudgePartialReply(ctx, req, r)
	select {
	case <-time.After(h.delay):
	case <-ctx.Done():
		return hooks.Allow
	}
	return hooks.Deny("off topic")
}

// Every segment judgment still running when the reply completes decides
// before the completed reply's check: one guardrail's pass coming first does
// not end the wait for another's failure. The last chunk's own audio goes
// with the cut reply.
func TestReplyRelease_EverySegmentVerdictRunningAtTheEndDecides(t *testing.T) {
	fast := &replyHoldHook{mode: hooks.StreamHoldSegment}
	slow := &partialFailHook{replyHoldHook: replyHoldHook{mode: hooks.StreamHoldSegment}, delay: 20 * time.Millisecond}
	r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, fast, slow)
	msg := prompt.DefaultBlockedMessage

	r.handle(saidChunk("Sunny skies."))
	r.handle(saidChunk(" Rain later."))
	last := spokenChunk(30)
	stop := "stop"
	last.FinishReason = &stop
	assert.Equal(t, []string{"interrupt", "text:" + msg, "end:" + msg + "|safety"}, r.feed(last))
}

// A reply the provider ends as canceled, or the caller barges in on, keeps
// only what was released: its text and its audio.
func TestReplyRelease_CutShortKeepsWhatWasReleased(t *testing.T) {
	canceled := types.FinishReasonInterrupted
	for name, last := range map[string]providers.StreamChunk{
		"canceled":  {FinishReason: &canceled},
		"barged in": {Interrupted: true},
	} {
		t.Run(name, func(t *testing.T) {
			hook := &replyHoldHook{mode: hooks.StreamHoldSegment}
			r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldSegment}, 0, hook)
			r.feed(spokenChunk(10))
			r.feed(saidChunk("Sunny skies."))
			r.feed(spokenChunk(20))
			r.feed(saidChunk(" Rain later."))
			r.feed(spokenChunk(30))
			r.handle(last)
			var msg *types.Message
			for {
				select {
				case e := <-r.out:
					if e.Message != nil && e.Message.Role == roleAssistant {
						msg = e.Message
					}
					continue
				default:
				}
				break
			}
			require.NotNil(t, msg)
			assert.Equal(t, "Sunny skies.", msg.Content)
			assert.Equal(t, pcm(10), partAudio(t, msg))
		})
	}
}

// partAudio is a message's audio.
func partAudio(t *testing.T, m *types.Message) []byte {
	t.Helper()
	for _, p := range m.Parts {
		if p.Type == types.ContentTypeAudio && p.Media != nil && p.Media.Data != nil {
			b, err := base64.StdEncoding.DecodeString(*p.Media.Data)
			require.NoError(t, err)
			return b
		}
	}
	return nil
}

// A reply the provider supersedes before it completes is never released.
func TestReplyRelease_SupersededReplyIsDropped(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
	r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldTurn}, 0, hook)
	first := transcriptChunk("Sunny.", pcm(10))
	first.Generation = 1
	second := transcriptChunk("Rain.", pcm(20))
	second.Generation = 2
	end := stopChunk()
	end.Generation = 2
	r.feed(first)
	r.feed(second)
	assert.Equal(t, []string{"text:Rain.", "audio:20", "end:Rain.|stop"}, r.feed(end))
}

// The last chunk's own text and audio are held with the rest, and released
// or dropped with them.
func TestReplyRelease_TheLastChunksOutputIsHeld(t *testing.T) {
	for name, tc := range map[string]struct {
		failOn string
		want   []string
	}{
		"passed": {"", []string{"text:Sunny.", "audio:10", "text: Rain.", "audio:20", "end:Sunny. Rain.|stop"}},
		"failed": {"Rain", []string{"interrupt", "text:" + prompt.DefaultBlockedMessage,
			"end:" + prompt.DefaultBlockedMessage + "|safety"}},
	} {
		t.Run(name, func(t *testing.T) {
			hook := &replyHoldHook{mode: hooks.StreamHoldTurn, failOn: tc.failOn}
			r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldTurn}, 0, hook)
			r.feed(transcriptChunk("Sunny.", pcm(10)))
			last := transcriptChunk(" Rain.", pcm(20))
			stop := "stop"
			last.FinishReason = &stop
			assert.Equal(t, tc.want, r.feed(last))
		})
	}
}

// A held reply's text is held whole: a chunk check's tail hold does not
// split it.
func TestReplyRelease_TextIsHeldWholeUnderAChunkCheck(t *testing.T) {
	hook := &replyHoldHook{mode: hooks.StreamHoldTurn}
	excl, err := guardrails.NewGuardrailHook("content_excludes", map[string]any{"patterns": []any{"Pineapple"}})
	require.NoError(t, err)
	r, _ := newReleased(t, StreamHoldConfig{Realtime: RealtimeHoldTurn}, 0, hook, excl)
	r.feed(saidChunk("Sunny Pine"))
	r.feed(saidChunk(" trees."))
	assert.Equal(t, []string{"text:Sunny Pine", "text: trees.", "end:Sunny Pine trees.|stop"}, r.feed(stopChunk()))
}

// Only a chunk that marks the reply complete and carries nothing else is the
// end of its content.
func TestIsGenerationCompleteOnly(t *testing.T) {
	stop := "stop"
	assert.True(t, isGenerationCompleteOnly(&providers.StreamChunk{GenerationComplete: true}))
	for _, c := range []providers.StreamChunk{
		{},
		{GenerationComplete: true, Content: "x"},
		{GenerationComplete: true, Delta: "x"},
		{GenerationComplete: true, MediaData: &providers.StreamMediaData{}},
		{GenerationComplete: true, FinishReason: &stop},
		{GenerationComplete: true, ToolCalls: []types.MessageToolCall{{ID: "1"}}},
		{GenerationComplete: true, CostInfo: &types.CostInfo{}},
		{GenerationComplete: true, Interrupted: true},
	} {
		assert.False(t, isGenerationCompleteOnly(&c), "%+v", c)
	}
}

// keepHeardAudio leaves parts that carry no audio data alone, and an audio
// part shorter than what was heard whole.
func TestKeepHeardAudio_LeavesOtherParts(t *testing.T) {
	img := types.ContentPart{Type: types.ContentTypeImage}
	noMedia := types.ContentPart{Type: types.ContentTypeAudio}
	noData := types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{}}
	short := types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{Data: ptrString("AQI=")}}
	msg := &types.Message{Parts: []types.ContentPart{img, noMedia, noData, short}}
	keepHeardAudio(msg, 5)
	assert.Equal(t, []types.ContentPart{img, noMedia, noData, short}, msg.Parts)
}

func ptrString(s string) *string { return &s }
