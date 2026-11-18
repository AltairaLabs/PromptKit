package stage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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
	defer s.mu.Unlock()
	s.texts = append(s.texts, text)
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
	cache := NewMemoryClipCache(1 << 20)
	cue := cueStageForSession(t, CueConfig{TTS: svc, Cache: cache}, sess)

	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, brunoText))
	assert.Equal(t, 1, sess.calls(brunoText))
	assert.Zero(t, svc.calls(brunoText))
	got, ok, err := cache.Get(context.Background(), ClipKeyFor(brunoText, "alloy", "session:test-live", pcm24k))
	require.NoError(t, err)
	require.True(t, ok, "kept under the session family's key")
	assert.Equal(t, ClipMeta{Text: brunoText, Voice: "alloy", Source: "session:test-live"}, got.Meta)
}

// Audio at another rate is resampled to the session's output rate.
func TestCueOutOfBand_IsResampled(t *testing.T) {
	pcm16 := cueRamp(3200)
	sess := newOOBSpeaker("alloy", pcm16)
	sess.rate = 16000
	cue := cueStageForSession(t, CueConfig{Cache: NewMemoryClipCache(1 << 20)}, sess)
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
			cue := cueStageForSession(t, CueConfig{TTS: svc, Cache: NewMemoryClipCache(1 << 20)}, sess)

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
	cue := cueStageForSession(t, CueConfig{Cache: NewMemoryClipCache(1 << 20), RenderTimeout: 100 * time.Millisecond,
		OnDemandBudget: time.Second}, sess)

	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, brunoText))
	assert.Equal(t, 2, sess.calls(brunoText))
}

// A render that fails falls to the next source with no second try.
func TestCueOutOfBand_ErrorFallsThrough(t *testing.T) {
	sess := newOOBSpeaker("alloy", cueRamp(4800))
	sess.err = errors.New("the out-of-band response ended cancelled")
	cue := cueStageForSession(t, CueConfig{Cache: NewMemoryClipCache(1 << 20)}, sess)

	assert.Equal(t, earconPCM(24000), playedFor(t, cue, brunoText))
	assert.Equal(t, 1, sess.calls(brunoText))
}

// A render with no audible audio falls to the next source.
func TestCueOutOfBand_SilentRenderFallsThrough(t *testing.T) {
	sess := newOOBSpeaker("alloy", nil)
	cue := cueStageForSession(t, CueConfig{Cache: NewMemoryClipCache(1 << 20)}, sess)

	assert.Equal(t, earconPCM(24000), playedFor(t, cue, brunoText))
	assert.Equal(t, 1, sess.calls(brunoText))
}

// Two sessions on one cache: the first renders and puts each text, the
// second plays it from the cache with no out-of-band request.
func TestCueOutOfBand_TwoSessionsRenderOnce(t *testing.T) {
	pcm := cueRamp(4800)
	cache := NewMemoryClipCache(1 << 20)
	first := newOOBSpeaker("alloy", pcm)
	second := newOOBSpeaker("alloy", cueRamp(10))
	texts := []string{brunoText, "Another message."}

	cue1 := cueStageForSession(t, CueConfig{Cache: cache}, first)
	cue2 := cueStageForSession(t, CueConfig{Cache: cache}, second)
	for _, text := range texts {
		assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue1, text))
		assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue2, text))
		assert.Equal(t, 1, first.calls(text)+second.calls(text), "one request for %q", text)
	}
	assert.Zero(t, second.calls(brunoText))
}

// A session in another voice does not share the first one's clips.
func TestCueOutOfBand_OtherVoiceRendersItsOwn(t *testing.T) {
	cache := NewMemoryClipCache(1 << 20)
	alloy := newOOBSpeaker("alloy", cueRamp(4800))
	verse := newOOBSpeaker("verse", cueRamp(2400))
	assert.Equal(t, cueFaded(cueRamp(4800), 24000),
		playedFor(t, cueStageForSession(t, CueConfig{Cache: cache}, alloy), brunoText))
	assert.Equal(t, cueFaded(cueRamp(2400), 24000),
		playedFor(t, cueStageForSession(t, CueConfig{Cache: cache}, verse), brunoText))
	assert.Equal(t, 1, verse.calls(brunoText))
}

// The render's cost, every try's, is one cost event on the session that
// rendered; a session that finds the clip in the cache reports none.
func TestCueOutOfBand_RenderCostIsReported(t *testing.T) {
	pcm := cueRamp(4800)
	cache := NewMemoryClipCache(1 << 20)
	sess := newOOBSpeaker("alloy", pcm)
	sess.said = []string{"Understood. " + brunoText, brunoText}
	sess.cost = &types.CostInfo{InputTokens: 40, OutputTokens: 60, TotalCost: 0.25,
		Quantities: map[string]float64{"audio_output_token": 50, "output_token": 10}}

	bus1 := events.NewEventBus()
	got1 := ttsEvents(t, bus1)
	cue1 := cueStageForSessionWith(t, CueConfig{Cache: cache}, sess, events.NewEmitter(bus1, "run1", "s1", "c1"))
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue1, brunoText))

	bus2 := events.NewEventBus()
	got2 := ttsEvents(t, bus2)
	cue2 := cueStageForSessionWith(t, CueConfig{Cache: cache}, newOOBSpeaker("alloy", pcm),
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
	cue := cueStageForSession(t, CueConfig{Cache: NewMemoryClipCache(1 << 20)}, sess)
	assert.Equal(t, earconPCM(24000), playedFor(t, cue, brunoText))
	assert.Zero(t, sess.calls(brunoText))
}
