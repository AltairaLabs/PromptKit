package stage

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// PrewarmClips renders each text once into the cache, under the key a session
// in that voice and format looks it up by: a session started afterwards plays
// the pre-warmed clip with no synthesis.
func TestPrewarmClips_SessionPlaysThePrewarmedClip(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	cache := NewMemoryClipCache(1 << 20)
	texts := []string{"Prewarmed one.", "Prewarmed two.", "Prewarmed one.", ""}

	require.NoError(t, PrewarmClips(context.Background(), cache, svc, "ALLOY", pcm24k, texts))

	assert.Equal(t, 1, svc.calls("Prewarmed one."))
	assert.Equal(t, 1, svc.calls("Prewarmed two."))
	assert.Zero(t, svc.calls(""), "an empty text is not rendered")
	assert.Equal(t, "alloy", svc.lastConfig().Voice, "the service voice matching the session voice")
	for _, text := range []string{"Prewarmed one.", "Prewarmed two."} {
		got, ok, err := cache.Get(context.Background(), ClipKeyFor(text, "alloy", "tts:fake", pcm24k))
		require.NoError(t, err)
		require.True(t, ok, "%q is cached", text)
		assert.Equal(t, cueFaded(pcm, 24000), got.Audio.Samples)
		assert.Equal(t, ClipMeta{Text: text, Voice: "alloy", Source: "tts:fake"}, got.Meta)
	}

	quiet := newFakeTTS(cueRamp(10))
	quiet.voices = svc.voices
	cue := cueStageForSession(t, CueConfig{TTS: quiet, Cache: cache}, spokenSession("alloy", 24000))
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, "Prewarmed two."))
	assert.Zero(t, quiet.calls("Prewarmed two."))
}

// A text the service fails is named in the error; the others are still cached.
func TestPrewarmClips_FailingTextIsNamedOthersCached(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	svc.audio = func(text string) ([]byte, error) {
		if text == "Will fail." {
			return nil, errors.New("synthesis failed")
		}
		return pcm, nil
	}
	cache := NewMemoryClipCache(1 << 20)

	err := PrewarmClips(context.Background(), cache, svc, "alloy", pcm24k, []string{"Will fail.", "Will pass."})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"Will fail."`)
	assert.Contains(t, err.Error(), "synthesis failed")
	assert.NotContains(t, err.Error(), "Will pass.")
	_, ok, _ := cache.Get(context.Background(), ClipKeyFor("Will pass.", "alloy", "tts:fake", pcm24k))
	assert.True(t, ok, "the other text is cached")
	_, ok, _ = cache.Get(context.Background(), ClipKeyFor("Will fail.", "alloy", "tts:fake", pcm24k))
	assert.False(t, ok)
}

// liveSessionTTS is a live session offered as a TTS service.
type liveSessionTTS struct {
	*fakeTTS
	*providersmock.MockStreamSession
}

// PrewarmClips needs a TTS service that renders PCM.
func TestPrewarmClips_RefusesWhatCannotRender(t *testing.T) {
	err := PrewarmClips(context.Background(), NewMemoryClipCache(1<<20), nil, "alloy", pcm24k, []string{"x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session-bound")

	live := liveSessionTTS{newFakeTTS(cueRamp(10)), providersmock.NewMockStreamSession()}
	err = PrewarmClips(context.Background(), NewMemoryClipCache(1<<20), live, "alloy", pcm24k, []string{"x"})
	require.ErrorIs(t, err, ErrSessionBoundSource)
	assert.Contains(t, err.Error(), "session-bound")
	assert.Zero(t, live.calls("x"))

	mp3 := newFakeTTS(cueRamp(10))
	mp3.formats = mp3.formats[:1]
	err = PrewarmClips(context.Background(), NewMemoryClipCache(1<<20), mp3, "alloy", pcm24k, []string{"x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "renders no PCM")
	assert.Zero(t, mp3.calls("x"))
}

// A directory pre-warmed with a TTS service is what a session with no TTS
// service plays from, read-only: the pre-warmed clip, in the session's voice,
// with no render. A text not pre-warmed, or another voice, plays the earcon.
func TestPrewarmClips_DirCacheServesASessionWithoutTTS(t *testing.T) {
	dir := t.TempDir()
	w, err := NewDirClipCache(dir)
	require.NoError(t, err)
	pcm := cueRamp(4800)
	require.NoError(t, PrewarmClips(context.Background(), w, newFakeTTS(pcm), "alloy", pcm24k,
		[]string{"Offline one."}))

	ro, err := NewDirClipCache(dir, DirCacheReadOnly())
	require.NoError(t, err)
	cue := cueStageForSession(t, CueConfig{Cache: ro}, spokenSession("Alloy", 24000))
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, "Offline one."))
	assert.Equal(t, EarconPCM(24000), playedFor(t, cue, "Not pre-warmed."))

	other := cueStageForSession(t, CueConfig{Cache: ro}, spokenSession("verse", 24000))
	assert.Equal(t, EarconPCM(24000), playedFor(t, other, "Offline one."), "another voice is another clip")
}
