package stage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/cue"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/voice"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cue stage (internal/cue) chained with the stages after it in a
// realtime chain: the output pacer, playout tracking and recording, and the
// provider stage ahead of it.

// testPacingClock is a fake clock an output pacer can run on.
type testPacingClock interface {
	now() time.Time
	sleep(ctx context.Context, d time.Duration) error
}

// outputPacer is a flush-on-cut output pacer on clock, without preroll.
func outputPacer(clock testPacingClock) *AudioPacingStage {
	p := NewNamedAudioPacingStage("audio-pacing-output").WithFlushOnCut()
	voice.SetPacingClock(p, clock.now, clock.sleep, 0)
	return p
}

// blockingClock is a pacing clock whose sleeps last until the pacer gives
// them up, as a real sleep does while the audio ahead plays.
func blockingClock() *fakeClock {
	c := newFakeClock()
	c.onSleep = func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}
	return c
}

// A cut after the cue (a barge-in) drops what has not played of the clip,
// like a reply's audio, and the next reply's audio plays.
func TestCueStage_CutDropsTheClip(t *testing.T) {
	cuts := map[string]core.StreamElement{
		"interrupt":   core.NewInterruptElement(),
		"interrupted": {Meta: core.ElementMetadata{Interrupted: true}},
	}
	for name, cut := range cuts {
		t.Run(name, func(t *testing.T) {
			pcm := cueRamp(12000) // 500 ms: five chunks
			cs := cue.NewStage(cue.Config{Clips: map[string]cue.Clip{brunoText: pcmClip(pcm, cueTestRate)}})
			c := startChain(t, cs, outputPacer(blockingClock()))
			c.in <- guardrailCueElem(brunoText)
			assert.Equal(t, brunoText, *c.next(t).Text)
			first := c.next(t)
			require.NotNil(t, first.Meta.CueAudio, "the clip's first chunk plays")
			// The second chunk now waits for the first to play out.

			next := replyAudio(100 * time.Millisecond)
			c.in <- cut
			c.in <- next
			got := c.finish(t)

			require.Len(t, got, 2, "the clip's other chunks are dropped")
			assert.True(t, core.IsCut(&got[0]))
			require.NotNil(t, got[1].Audio)
			assert.Nil(t, got[1].Meta.CueAudio)
			assert.Len(t, got[1].Audio.Samples, 4800, "the next reply's audio plays")
		})
	}
}

// While a clip renders, reply output waits behind it from the first audio
// on: an element before that audio passes at once, and the reply's audio and
// the end that follows it come after the clip, in order.
func TestCueStage_PendingClipHoldsReplyOutput(t *testing.T) {
	pcm := cueRamp(6000)
	release := make(chan struct{})
	cs := cue.NewStage(cue.Config{})
	cue.SetRender(cs, pendingRender(pcmClip(pcm, cueTestRate), release))
	c := startChain(t, cs, outputPacer(newFakeClock()))

	reply := cueRamp(2400)
	user := core.StreamElement{Message: &types.Message{Role: core.RoleUser, Content: "Tell me about Bruno."}}
	c.in <- guardrailCueElem(brunoText)
	c.in <- user
	c.in <- replyAudioOf(reply)
	c.in <- replyEnd("Hello.")

	assert.Equal(t, brunoText, *c.next(t).Text)
	first := c.next(t)
	require.NotNil(t, first.Message, "an element ahead of the reply's audio passes while the clip renders")
	assert.Equal(t, "Tell me about Bruno.", first.Message.Content)

	close(release)
	got := c.finish(t)
	require.Len(t, got, 5, "three chunks of the clip, then the reply's audio, then its end")
	assert.Equal(t, cueFaded(pcm, cueTestRate), cueAudioOf(got[:3]))
	assert.Nil(t, got[3].Meta.CueAudio)
	assert.Equal(t, reply, got[3].Audio.Samples)
	require.NotNil(t, got[4].Message)
	assert.True(t, got[4].EndOfStream)
	assert.Equal(t, "Hello.", got[4].Message.Content)
}

// Downstream of a rendering clip, a reply's end still follows its audio, so
// playout tracking holds the reply and counts its own audio only.
func TestCueStage_PendingClipKeepsReplyPlayout(t *testing.T) {
	release := make(chan struct{})
	cs := cue.NewStage(cue.Config{})
	cue.SetRender(cs, pendingRender(pcmClip(cueRamp(12000), cueTestRate), release)) // 500 ms
	fc := &fakeHoldClock{t: holdT0}
	hold := NewPlayoutHoldStage()
	hold.clock = core.HoldClock{Now: fc.now, Until: fc.until}
	c := startChain(t, cs, hold)

	c.in <- guardrailCueElem(brunoText)
	c.in <- replyAudio(time.Second)
	c.in <- replyEnd("A long story.")
	assert.Equal(t, brunoText, *c.next(t).Text)
	close(release)
	var end core.StreamElement
	for end.Message == nil {
		end = c.next(t)
	}
	assert.True(t, end.Meta.PlayoutHeld, "the reply's end came after its audio, so its Message is held")

	fc.advance(900 * time.Millisecond) // the clip, then 400 ms of the reply
	c.in <- bargeInElem()
	assert.True(t, c.next(t).Meta.Interrupted)
	rel := c.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, playoutMeta{Finish: "stop", Cut: types.PlayoutCutBargeIn, HeardMs: 400, AudioMs: 1000, Fraction: 0.4},
		playoutOf(rel.Message))
	c.finish(t)
}

// stepClock is a pacing clock whose sleeps report that they started and last
// until tick is closed or the pacer gives them up.
type stepClock struct {
	t        time.Time
	sleeping chan struct{}
	tick     chan struct{}
}

func (c *stepClock) now() time.Time { return c.t }

func (c *stepClock) sleep(ctx context.Context, d time.Duration) error {
	select {
	case c.sleeping <- struct{}{}:
	default:
	}
	select {
	case <-c.tick:
		c.t = c.t.Add(d)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// A preemptible cue's audio still queued at the pacer is dropped when reply
// audio arrives; a guardrail cue's is not.
func TestCueStage_PreemptibleCueGivesWayToReplyAudio(t *testing.T) {
	cases := []struct {
		name       string
		cue        core.StreamElement
		wantChunks int // of the clip's 4, after the first
	}{
		{"backchannel", core.StreamElement{Cue: &core.Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}, 0},
		{"guardrail", guardrailCueElem("mm-hm"), 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pcm := cueRamp(9600) // 400 ms: four chunks
			cs := cue.NewStage(cue.Config{Clips: map[string]cue.Clip{"mm-hm": pcmClip(pcm, cueTestRate)}})
			src := startChain(t, cs)
			src.in <- tc.cue
			played := src.finish(t)
			require.Len(t, played, 5)

			clock := &stepClock{t: time.Unix(0, 0), sleeping: make(chan struct{}, 1), tick: make(chan struct{})}
			in, out := make(chan core.StreamElement), make(chan core.StreamElement, 64)
			done := make(chan error, 1)
			go func() { done <- outputPacer(clock).Process(context.Background(), in, out) }()
			for _, e := range played {
				in <- e
			}
			assert.Equal(t, tc.cue.Cue, (<-out).Cue)
			assert.NotNil(t, (<-out).Meta.CueAudio, "the first chunk plays")
			<-clock.sleeping // the second waits for it

			reply := cueRamp(240)
			marker := "marker"
			in <- core.StreamElement{Audio: &core.AudioData{Samples: reply, SampleRate: cueTestRate, Channels: 1}}
			in <- core.StreamElement{Text: &marker} // the pacer has read the reply audio
			if tc.wantChunks == 0 {
				// The preempted chunk stops waiting at once, and the reply starts
				// its own sequence: it goes out with no wait at all.
				for _, want := range []string{"reply", "marker"} {
					select {
					case e := <-out:
						if want == "reply" {
							assert.Equal(t, reply, e.Audio.Samples)
						} else {
							assert.Equal(t, marker, *e.Text)
						}
					case <-time.After(3 * time.Second):
						require.FailNow(t, "the preempted cue kept the reply waiting")
					}
				}
				close(in)
				require.NoError(t, <-done)
				assert.Empty(t, out)
				return
			}
			close(clock.tick)
			close(in)
			require.NoError(t, <-done)

			var got []core.StreamElement
			for e := range out {
				got = append(got, e)
			}
			require.Len(t, got, tc.wantChunks+2)
			for _, e := range got[:tc.wantChunks] {
				assert.NotNil(t, e.Meta.CueAudio)
			}
			assert.Equal(t, reply, got[tc.wantChunks].Audio.Samples, "the reply's audio plays")
			assert.Equal(t, marker, *got[tc.wantChunks+1].Text)
		})
	}
}

// Cue audio plays out before a held Message is released, but it is not the
// reply's audio: a reply after a cue reports its own audio only.
func TestCueStage_PlayoutHoldKeepsCueAudioOutOfTheReply(t *testing.T) {
	r := startHold(t)
	cueAudio := replyAudio(500 * time.Millisecond)
	cueAudio.Meta.CueAudio = &core.Cue{Kind: core.CueGuardrailMessage, Text: brunoText}
	r.pass(t, cueAudio, replyAudio(time.Second), replyEnd("A long story."))

	r.clock.advance(900 * time.Millisecond) // the cue, then 400 ms of the reply
	r.pass(t, bargeInElem())
	rel := r.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, playoutMeta{Finish: "stop", Cut: types.PlayoutCutBargeIn, HeardMs: 400, AudioMs: 1000, Fraction: 0.4},
		playoutOf(rel.Message))
	close(r.in)
	require.NoError(t, <-r.done)
}

// A held Message waits for cue audio ahead of it to play out.
func TestCueStage_PlayoutHoldWaitsForCueAudio(t *testing.T) {
	r := startHold(t)
	cueAudio := replyAudio(500 * time.Millisecond)
	cueAudio.Meta.CueAudio = &core.Cue{Kind: core.CueGuardrailMessage, Text: brunoText}
	got := r.pass(t, replyAudio(100*time.Millisecond), cueAudio, replyEnd("Hi."))
	assert.True(t, got[2].Meta.PlayoutHeld)
	r.clock.advance(550 * time.Millisecond)
	assert.True(t, r.idle(t), "the cue is still playing")
	r.clock.advance(50 * time.Millisecond)
	rel := r.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, "Hi.", rel.Message.Content)
	close(r.in)
	require.NoError(t, <-r.done)
}

// A recording holds the cue's audio as assistant audio tagged with the cue's
// kind, and no message for it.
func TestCueStage_RecordingTagsCueAudio(t *testing.T) {
	pcm := cueRamp(6000)
	store := &fakeEventStore{}
	rec := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionOutput, SessionID: "s",
		IncludeAudio: true})
	cs := cue.NewStage(cue.Config{Clips: map[string]cue.Clip{brunoText: pcmClip(pcm, cueTestRate)}})
	c := startChain(t, cs, rec)

	reply := cueRamp(480)
	c.in <- core.StreamElement{Audio: &core.AudioData{Samples: reply, SampleRate: cueTestRate, Channels: 1,
		Format: core.AudioFormatPCM16}, Timestamp: time.Now()}
	c.in <- guardrailCueElem(brunoText)
	end := core.StreamElement{EndOfStream: true, Message: &types.Message{Role: core.RoleAssistant, Content: brunoText,
		FinishReason: types.FinishReasonSafety}}
	c.in <- end
	c.finish(t)

	audioEvents := store.filterByType(events.EventAudioOutput)
	require.Len(t, audioEvents, 4)
	var kinds, from []string
	var clip []byte
	for i, e := range audioEvents {
		data := e.Data.(*events.AudioEventData)
		kinds = append(kinds, data.CueKind)
		from = append(from, data.GeneratedFrom)
		if i > 0 {
			clip = append(clip, data.Payload.InlineData...)
		}
	}
	assert.Equal(t, []string{"", "guardrail_message", "guardrail_message", "guardrail_message"}, kinds)
	assert.Equal(t, []string{"model", "cue", "cue", "cue"}, from)
	assert.Equal(t, reply, audioEvents[0].Data.(*events.AudioEventData).Payload.InlineData)
	assert.Equal(t, cueFaded(pcm, cueTestRate), clip)

	msgs := store.filterByType(events.EventMessageCreated)
	require.Len(t, msgs, 1, "no message for the cue")
	assert.Equal(t, brunoText, msgs[0].Data.(*events.MessageCreatedData).Content)
}

// An input element carrying only a cue (a backchannel a mic-side stage asks
// for) goes to the output, not to the session.
func TestDuplexProviderStage_ForwardsCueOnlyInput(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)
	elem := core.StreamElement{Cue: &core.Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}
	out := make(chan core.StreamElement, 4)
	done := make(chan error, 1)

	require.False(t, s.handleInputElement(context.Background(), &elem, out, done))
	require.Len(t, out, 1)
	assert.Equal(t, &core.Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}, (<-out).Cue)
	assert.Empty(t, done)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	assert.Empty(t, sess.texts)
	assert.Empty(t, sess.chunks)
}

// blockMessageHook is a provider hook that blocks with msg.
type blockMessageHook struct{ msg string }

func (h blockMessageHook) Name() string { return "block" }

func (h blockMessageHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h blockMessageHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

func (h blockMessageHook) BlockMessages() []string { return []string{h.msg} }

// When the provider stage creates its session, the cue stage starts
// rendering its guardrails' messages in the session's voice, and the first
// reply does not wait for it.
func TestCueVoice_SessionStartDoesNotDelayTheFirstReply(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	svc.gate = make(chan struct{})
	provider := providersmock.NewStreamingProvider("t", "m", false).WithAutoRespond("Hello.").
		WithOutputAudio(providers.OutputAudio{Voice: "Verse", SampleRate: 24000, Family: "test-live"})
	duplex := NewDuplexProviderStage(provider, baseConfig())
	duplex.SetHookRegistry(hooks.NewRegistry(hooks.WithProviderHook(blockMessageHook{msg: "Guarded."})))
	cs := newCueStageFor(duplex, cue.Config{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)})
	stored := storedTexts(cs)

	text := "hi"
	out, err := runDuplexStage(t, duplex, []core.StreamElement{{Text: &text, EndOfStream: true}})
	require.NoError(t, err)
	var replies []string
	for _, e := range out {
		if e.Message != nil && e.Message.Role == core.RoleAssistant {
			replies = append(replies, e.Message.Content)
		}
	}
	require.NotEmpty(t, replies, "the reply came while the clip was still rendering")
	assert.Equal(t, "Hello.", replies[0])
	select {
	case got := <-stored:
		require.FailNow(t, fmt.Sprintf("the clip for %q rendered before it was released", got))
	default:
	}

	close(svc.gate)
	assert.Equal(t, "Guarded.", waitStored(t, stored))
	assert.Equal(t, "v-2", svc.lastConfig().Voice, "in the session's voice")
	clip, isReady, ok := cue.Held(cue.BankOf(cs), "Guarded.")
	require.True(t, ok)
	assert.True(t, isReady)
	assert.Equal(t, cueFaded(pcm, 24000), clip.Audio.Samples)
}
