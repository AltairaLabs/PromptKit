package stage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// oobSpeaker is a spoken session that speaks out of band: each call says
// the next of said (the text itself once said runs out) as pcm at rate, at
// cost.
type oobSpeaker struct {
	*providersmock.MockStreamSession
	pcm  []byte
	rate int
	cost *types.CostInfo
	err  error
	// delay is how long each call takes.
	delay time.Duration
	// gate, when set, holds each call until it is closed.
	gate chan struct{}

	mu    sync.Mutex
	said  []string
	texts []string
}

func newOOBSpeaker(voice string, pcm []byte) *oobSpeaker {
	return &oobSpeaker{
		MockStreamSession: providersmock.NewMockStreamSession().WithOutputAudio(
			providers.OutputAudio{Voice: voice, SampleRate: 24000, Family: "test-live"}),
		pcm:  pcm,
		rate: 24000,
	}
}

func (s *oobSpeaker) SpeakOutOfBand(ctx context.Context, text string) (providers.OutOfBandSpeech, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return providers.OutOfBandSpeech{}, ctx.Err()
		}
	}
	s.mu.Lock()
	s.texts = append(s.texts, text)
	s.mu.Unlock()
	if s.gate != nil {
		<-s.gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	said := text
	if len(s.said) > 0 {
		said, s.said = s.said[0], s.said[1:]
	}
	if s.err != nil {
		return providers.OutOfBandSpeech{Cost: s.cost}, s.err
	}
	return providers.OutOfBandSpeech{Audio: s.pcm, SampleRate: s.rate, Transcript: said, Cost: s.cost}, nil
}

func (s *oobSpeaker) calls(text string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, t := range s.texts {
		if t == text {
			n++
		}
	}
	return n
}

// The session's own out-of-band audio is the first source: with a TTS
// service configured too, the clip is the session's, keyed by its family and
// voice, and the service is not asked.
func TestCueOutOfBand_IsTheFirstSource(t *testing.T) {
	pcm := cueRamp(4800)
	sess := newOOBSpeaker("alloy", pcm)
	svc := newFakeTTS(cueRamp(2400))
	cache := cueclip.NewMemoryCache(1 << 20)
	cue := cueStageForSession(t, cueConfig{TTS: svc, Cache: cache}, sess)

	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, brunoText))
	assert.Equal(t, 1, sess.calls(brunoText))
	assert.Zero(t, svc.calls(brunoText))
	got, ok, err := cache.Get(context.Background(), cueclip.KeyFor(brunoText, "alloy", "session:test-live", pcm24k))
	require.NoError(t, err)
	require.True(t, ok, "kept under the session family's key")
	assert.Equal(t, cueclip.Meta{Text: brunoText, Voice: "alloy", Source: "session:test-live"}, got.Meta)
}

// Audio at another rate is resampled to the session's output rate.
func TestCueOutOfBand_IsResampled(t *testing.T) {
	pcm16 := cueRamp(3200)
	sess := newOOBSpeaker("alloy", pcm16)
	sess.rate = 16000
	cue := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)}, sess)
	assert.Equal(t, cueFaded(resampled(t, pcm16, 16000, 24000), 24000), playedFor(t, cue, brunoText))
}

// The model can say more than the text. A transcript that matches only once
// case, punctuation and spacing are set aside is a match; one that does not
// is rendered once more, and a second miss falls to the next source.
func TestCueOutOfBand_TranscriptCheck(t *testing.T) {
	cases := []struct {
		name      string
		said      []string
		wantCalls int
		wantOOB   bool
	}{
		{"match", []string{brunoText}, 1, true},
		{"loose match", []string{"we  DON'T talk about Bruno"}, 1, true},
		{"miss then match", []string{"Understood. " + brunoText, brunoText}, 2, true},
		{"two misses", []string{"Understood. " + brunoText, "Sure. " + brunoText}, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pcm := cueRamp(4800)
			sess := newOOBSpeaker("alloy", pcm)
			sess.said = tc.said
			ttsPCM := cueRamp(2400)
			svc := newFakeTTS(ttsPCM)
			cue := cueStageForSession(t, cueConfig{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)}, sess)

			got := playedFor(t, cue, brunoText)
			assert.Equal(t, tc.wantCalls, sess.calls(brunoText))
			if tc.wantOOB {
				assert.Equal(t, cueFaded(pcm, 24000), got)
				assert.Zero(t, svc.calls(brunoText))
			} else {
				assert.Equal(t, cueFaded(ttsPCM, 24000), got, "the next source, the TTS service")
				assert.Equal(t, 1, svc.calls(brunoText))
			}
		})
	}
}

// Each try gets RenderTimeout: a miss and its retry may take longer than
// one render's timeout together.
func TestCueOutOfBand_EachTryGetsTheRenderTimeout(t *testing.T) {
	pcm := cueRamp(4800)
	sess := newOOBSpeaker("alloy", pcm)
	sess.said = []string{"Understood. " + brunoText, brunoText}
	sess.delay = 60 * time.Millisecond
	cue := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20), RenderTimeout: 100 * time.Millisecond,
		OnDemandBudget: time.Second}, sess)

	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, brunoText))
	assert.Equal(t, 2, sess.calls(brunoText))
}

// The transcript check sets aside case, punctuation and spacing: dashes,
// slashes and the like separate words, apostrophes do not.
func TestCueOutOfBand_SameSpeech(t *testing.T) {
	cases := []struct {
		said, text string
		want       bool
	}{
		{"We don't talk about Bruno.", "we dont talk about bruno", true},
		{"We don’t talk about Bruno!", "We don't talk about Bruno.", true},
		{"well known", "well-known", true},
		{"That, let's go.", "That—let's go", true},
		{"either or", "either/or", true},
		{"  spaced\tout  ", "spaced out", true},
		{"Understood. We don't talk about Bruno.", "We don't talk about Bruno.", false},
		{"wellknown", "well-known", false},
		{"that lets", "that—let's", true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, sameSpeech(tc.said, tc.text), "%q vs %q", tc.said, tc.text)
	}
}

// A message with a dash matches its transcript on the first try.
func TestCueOutOfBand_DashedMessageMatchesFirstTime(t *testing.T) {
	text := "Sorry—that's off-topic."
	sess := newOOBSpeaker("alloy", cueRamp(4800))
	sess.said = []string{"Sorry, that's off topic."}
	cue := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)}, sess)

	assert.Equal(t, cueFaded(cueRamp(4800), 24000), playedFor(t, cue, text))
	assert.Equal(t, 1, sess.calls(text))
}

// A render that fails falls to the next source with no second try.
func TestCueOutOfBand_ErrorFallsThrough(t *testing.T) {
	sess := newOOBSpeaker("alloy", cueRamp(4800))
	sess.err = errors.New("the out-of-band response ended cancelled")
	cue := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)}, sess)

	assert.Equal(t, earconPCM(24000), playedFor(t, cue, brunoText))
	assert.Equal(t, 1, sess.calls(brunoText))
}

// A render with no audible audio falls to the next source.
func TestCueOutOfBand_SilentRenderFallsThrough(t *testing.T) {
	sess := newOOBSpeaker("alloy", nil)
	cue := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)}, sess)

	assert.Equal(t, earconPCM(24000), playedFor(t, cue, brunoText))
	assert.Equal(t, 1, sess.calls(brunoText))
}

// Two sessions on one cache: the first renders and puts each text, the
// second plays it from the cache with no out-of-band request.
func TestCueOutOfBand_TwoSessionsRenderOnce(t *testing.T) {
	pcm := cueRamp(4800)
	cache := cueclip.NewMemoryCache(1 << 20)
	first := newOOBSpeaker("alloy", pcm)
	second := newOOBSpeaker("alloy", cueRamp(10))
	texts := []string{brunoText, "Another message."}

	cue1 := cueStageForSession(t, cueConfig{Cache: cache}, first)
	cue2 := cueStageForSession(t, cueConfig{Cache: cache}, second)
	for _, text := range texts {
		assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue1, text))
		assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue2, text))
		assert.Equal(t, 1, first.calls(text)+second.calls(text), "one request for %q", text)
	}
	assert.Zero(t, second.calls(brunoText))
}

// A session that joined another's render renders on its own session when
// the other's closed mid-render, instead of falling to the next source; a
// failure of the text itself is not rendered again.
func TestCueOutOfBand_LeaderClosingLetsAJoinerRenderItself(t *testing.T) {
	cases := []struct {
		name       string
		leaderErr  error
		wantJoiner int
	}{
		{"leader closed", fmt.Errorf("render: %w", providers.ErrOutOfBandSessionClosed), 1},
		{"render failed", errors.New("the out-of-band response ended failed"), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			joined := make(chan struct{}, 4)
			hook := func() { joined <- struct{}{} }
			clipFlightJoined.Store(&hook)
			t.Cleanup(func() { clipFlightJoined.Store(nil) })

			leader := newOOBSpeaker("alloy", cueRamp(4800))
			leader.gate = make(chan struct{})
			leader.err = tc.leaderErr
			ownPCM := cueRamp(2400)
			joiner := newOOBSpeaker("alloy", ownPCM)
			bank1 := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)}, leader).bank
			bank2 := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)}, joiner).bank
			text := "Joined, then on my own. " + tc.name

			f1 := bank1.render(context.Background(), text, nil)
			<-joined
			f2 := bank2.render(context.Background(), text, nil)
			<-joined
			close(leader.gate)
			<-f1.done
			<-f2.done

			require.NoError(t, f2.err)
			assert.Equal(t, tc.wantJoiner, joiner.calls(text))
			if tc.wantJoiner == 1 {
				assert.Equal(t, cueFaded(ownPCM, 24000), f2.clip.Audio.Samples, "its own render")
				assert.True(t, f2.keep)
			} else {
				assert.Equal(t, earconPCM(24000), f2.clip.Audio.Samples)
			}
			assert.Equal(t, earconPCM(24000), f1.clip.Audio.Samples, "the closed session plays the earcon")
		})
	}
}

// A session in another voice does not share the first one's clips.
func TestCueOutOfBand_OtherVoiceRendersItsOwn(t *testing.T) {
	cache := cueclip.NewMemoryCache(1 << 20)
	alloy := newOOBSpeaker("alloy", cueRamp(4800))
	verse := newOOBSpeaker("verse", cueRamp(2400))
	assert.Equal(t, cueFaded(cueRamp(4800), 24000),
		playedFor(t, cueStageForSession(t, cueConfig{Cache: cache}, alloy), brunoText))
	assert.Equal(t, cueFaded(cueRamp(2400), 24000),
		playedFor(t, cueStageForSession(t, cueConfig{Cache: cache}, verse), brunoText))
	assert.Equal(t, 1, verse.calls(brunoText))
}

// The render's cost, every try's, is one cost event on the session that
// rendered; a session that finds the clip in the cache reports none.
func TestCueOutOfBand_RenderCostIsReported(t *testing.T) {
	pcm := cueRamp(4800)
	cache := cueclip.NewMemoryCache(1 << 20)
	sess := newOOBSpeaker("alloy", pcm)
	sess.said = []string{"Understood. " + brunoText, brunoText}
	sess.cost = &types.CostInfo{InputTokens: 40, OutputTokens: 60, TotalCost: 0.25,
		Quantities: map[string]float64{"audio_output_token": 50, "output_token": 10}}

	bus1 := events.NewEventBus()
	got1 := ttsEvents(t, bus1)
	cue1 := cueStageForSessionWith(t, cueConfig{Cache: cache}, sess, events.NewEmitter(bus1, "run1", "s1", "c1"))
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue1, brunoText))

	bus2 := events.NewEventBus()
	got2 := ttsEvents(t, bus2)
	cue2 := cueStageForSessionWith(t, cueConfig{Cache: cache}, newOOBSpeaker("alloy", pcm),
		events.NewEmitter(bus2, "run2", "s2", "c2"))
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue2, brunoText))

	evs := got1()
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, "test-live", ev.Provider)
	assert.Equal(t, "tts", ev.Capability)
	assert.Equal(t, "cue", ev.Source)
	assert.Equal(t, len(brunoText), ev.Characters)
	assert.InDelta(t, 0.5, ev.Cost, 1e-12, "both tries are billed")
	assert.Equal(t, map[string]float64{"audio_output_token": 100, "output_token": 20}, ev.Quantities)
	assert.InDelta(t, cueFadedSeconds(pcm), ev.AudioSeconds, 1e-9)
	assert.Empty(t, got2(), "a cache hit is free")
}

// A session that does not report its output audio has no family to key its
// clips by: its out-of-band audio is not used.
func TestCueOutOfBand_NeedsTheSessionFamily(t *testing.T) {
	sess := newOOBSpeaker("alloy", cueRamp(4800))
	sess.MockStreamSession = providersmock.NewMockStreamSession().WithOutputAudio(
		providers.OutputAudio{Voice: "alloy", SampleRate: 24000})
	cue := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)}, sess)
	assert.Equal(t, earconPCM(24000), playedFor(t, cue, brunoText))
	assert.Zero(t, sess.calls(brunoText))
}

// A session whose render is cut while its flight is still running gives up
// at once; the flight runs on and keeps the clip in the cache for next time.
func TestCueOutOfBand_CancelledLeaderFlightKeepsTheClip(t *testing.T) {
	joined := make(chan struct{}, 4)
	hook := func() { joined <- struct{}{} }
	clipFlightJoined.Store(&hook)
	t.Cleanup(func() { clipFlightJoined.Store(nil) })

	pcm := cueRamp(4800)
	sess := newOOBSpeaker("alloy", pcm)
	sess.gate = make(chan struct{})
	cache := cueclip.NewMemoryCache(1 << 20)
	bank := cueStageForSession(t, cueConfig{Cache: cache}, sess).bank
	text := "Cut while the flight runs."

	ctx, cancel := context.WithCancel(context.Background())
	f := bank.render(ctx, text, nil)
	<-joined
	cancel()
	<-f.done
	require.ErrorIs(t, f.err, context.Canceled)
	assert.False(t, f.keep)

	close(sess.gate)
	key := cueclip.KeyFor(text, "alloy", "session:test-live", pcm24k)
	require.Eventually(t, func() bool {
		_, ok, err := cache.Get(context.Background(), key)
		return err == nil && ok
	}, 5*time.Second, time.Millisecond, "the flight keeps the clip")
	got, _, err := cache.Get(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, cueFaded(pcm, 24000), got.PCM)
	assert.Equal(t, 1, sess.calls(text))
}
