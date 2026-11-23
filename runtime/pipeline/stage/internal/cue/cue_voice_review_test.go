package cue

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// testClock is a clock a test moves by hand.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// When the TTS fails for a text, that cue plays the earcon, but the earcon is
// not kept as the text's clip: within the backoff the text plays the earcon
// without asking the TTS again, and after it the TTS is tried again and its
// clip is kept.
func TestCueVoice_FailedSourceIsRetriedAfterBackoff(t *testing.T) {
	pcm := cueRamp(4800)
	var failing atomic.Bool
	failing.Store(true)
	svc := newFakeTTS(pcm)
	svc.audio = func(string) ([]byte, error) {
		if failing.Load() {
			return nil, errors.New("tts down")
		}
		return pcm, nil
	}
	cue := cueStageForSession(t, Config{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)},
		spokenSession("alloy", 24000))
	clock := &testClock{t: time.Unix(1000, 0)}
	cue.bank.now = clock.now
	text := "Retry me."

	assert.Equal(t, earconPCM(24000), playedFor(t, cue, text))
	assert.Equal(t, 1, svc.calls(text))
	_, _, held := cue.bank.get(text)
	assert.False(t, held, "the earcon is not kept as the text's clip")

	failing.Store(false)
	clock.advance(cueRetryBackoff - time.Second)
	assert.Equal(t, earconPCM(24000), playedFor(t, cue, text), "within the backoff")
	assert.Equal(t, 1, svc.calls(text), "the TTS is not asked within the backoff")

	clock.advance(2 * time.Second)
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, text))
	assert.Equal(t, 2, svc.calls(text))
	clip, isReady, held := cue.bank.get(text)
	require.True(t, held, "the TTS clip is kept")
	assert.True(t, isReady)
	assert.Equal(t, cueFaded(pcm, 24000), clip.Audio.Samples)
}

// Within one run of the stage, a text that played the earcon because its
// source failed plays its own clip once the source renders it after the
// backoff: the earcon it played in its place is not kept for the run either.
func TestCueVoice_RetriedClipPlaysLaterInTheSameRun(t *testing.T) {
	pcm := cueRamp(4800)
	var failing atomic.Bool
	failing.Store(true)
	svc := newFakeTTS(pcm)
	svc.audio = func(string) ([]byte, error) {
		if failing.Load() {
			return nil, errors.New("tts down")
		}
		return pcm, nil
	}
	cue := cueStageForSession(t, Config{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)},
		spokenSession("alloy", 24000))
	clock := &testClock{t: time.Unix(1000, 0)}
	cue.bank.now = clock.now
	text := "Retry me in this run."

	c := startChain(t, cue)
	c.in <- guardrailCueElem(text)
	assert.Equal(t, text, *c.next(t).Text)
	var first []byte
	for range 2 { // 200 ms of earcon in 100 ms chunks
		first = append(first, c.next(t).Audio.Samples...)
	}
	assert.Equal(t, earconPCM(24000), first, "the TTS failed: the earcon plays")

	failing.Store(false)
	clock.advance(cueRetryBackoff + time.Second)
	c.in <- guardrailCueElem(text)
	got := c.finish(t)
	assert.Equal(t, 2, svc.calls(text), "the TTS is asked again after the backoff")
	assert.Equal(t, cueFaded(pcm, 24000), cueAudioOf(got), "the TTS clip plays, not the earcon")
}

// unreportedSession is a session that does not report its output audio.
type unreportedSession struct{ providers.StreamInputSession }

// A session that does not report its output audio is not muted: configured
// clips play, and clips render as PCM16 at 24 kHz in the TTS service's
// default voice, with one warning.
func TestCueVoice_SessionWithoutReporterKeepsCueAudio(t *testing.T) {
	logs := captureCueLogs(t)
	pcm, static := cueRamp(4800), cueRamp(2400)
	svc := newFakeTTS(pcm)
	cue := cueStageForSession(t, Config{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20),
		Clips: map[string]Clip{"Static clip.": pcmClip(static, 24000)}},
		unreportedSession{providersmock.NewMockStreamSession()})

	assert.Equal(t, cueFaded(static, 24000), playedFor(t, cue, "Static clip."))
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, "Rendered for an unreported session."))
	cfg := svc.lastConfig()
	assert.Equal(t, "", cfg.Voice)
	assert.Equal(t, pcmAt24k, cfg.Format)
	assert.True(t, cue.bank.Voices("Anything."))
	assert.Equal(t, 1, strings.Count(logs.String(), "does not report its output audio"))
	assert.Zero(t, strings.Count(logs.String(), "no voice matching"))
}

// countingCache counts Gets and Puts on a memory cache.
type countingCache struct {
	cueclip.Cache
	puts atomic.Int32
}

func (c *countingCache) Put(ctx context.Context, key cueclip.Key, clip cueclip.Clip) error {
	c.puts.Add(1)
	return c.Cache.Put(ctx, key, clip)
}

// A session that joins another's render with its own cache keeps the clip in
// its cache too; the render is billed to the session that ran it.
func TestCueVoice_JoinedRenderFillsTheJoinersCache(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	svc.gate = make(chan struct{})
	joined := make(chan struct{}, 4)
	hook := func() { joined <- struct{}{} }
	clipFlightJoined.Store(&hook)
	t.Cleanup(func() { clipFlightJoined.Store(nil) })

	cache1 := &countingCache{Cache: cueclip.NewMemoryCache(1 << 20)}
	cache2 := &countingCache{Cache: cueclip.NewMemoryCache(1 << 20)}
	bank1 := cueStageForSession(t, Config{TTS: svc, Cache: cache1}, spokenSession("alloy", 24000)).bank
	bank2 := cueStageForSession(t, Config{TTS: svc, Cache: cache2}, spokenSession("alloy", 24000)).bank
	text := "Joined render."

	f1 := bank1.render(context.Background(), text, nil)
	<-joined
	f2 := bank2.render(context.Background(), text, nil)
	<-joined
	close(svc.gate)
	<-f1.done
	<-f2.done

	require.NoError(t, f1.err)
	require.NoError(t, f2.err)
	assert.Equal(t, 1, svc.calls(text), "one render for both")
	key := cueclip.KeyFor(text, "alloy", "tts:fake", pcm24k)
	for i, c := range []*countingCache{cache1, cache2} {
		clip, ok, err := c.Get(context.Background(), key)
		require.NoError(t, err)
		require.True(t, ok, "cache %d holds the clip", i+1)
		assert.Equal(t, cueFaded(pcm, 24000), clip.PCM)
		assert.Equal(t, int32(1), c.puts.Load())
	}
}

// A clip already in a session's own cache is played from it without joining
// another session's render still running.
func TestCueVoice_OwnCacheIsReadBeforeJoining(t *testing.T) {
	pcm, warm := cueRamp(4800), cueRamp(2400)
	svc := newFakeTTS(pcm)
	svc.gate = make(chan struct{})
	t.Cleanup(func() { close(svc.gate) })
	joined := make(chan struct{}, 4)
	hook := func() { joined <- struct{}{} }
	clipFlightJoined.Store(&hook)
	t.Cleanup(func() { clipFlightJoined.Store(nil) })

	text := "Own cache first."
	bank1 := cueStageForSession(t, Config{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)},
		spokenSession("alloy", 24000)).bank
	cache2 := cueclip.NewMemoryCache(1 << 20)
	require.NoError(t, cache2.Put(context.Background(), cueclip.KeyFor(text, "alloy", "tts:fake", pcm24k),
		toCached(readyClip(warm, 24000))))
	bank2 := cueStageForSession(t, Config{TTS: svc, Cache: cache2}, spokenSession("alloy", 24000)).bank

	bank1.render(context.Background(), text, nil)
	<-joined // bank1's render is running, held at the gate
	f2 := bank2.render(context.Background(), text, nil)
	select {
	case <-f2.done:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "the second session waited for the first's render")
	}
	require.NoError(t, f2.err)
	assert.Equal(t, warm, f2.clip.Audio.Samples)
}
