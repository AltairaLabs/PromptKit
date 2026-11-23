package cue

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// PrewarmClips renders each text once into the cache, under the key a session
// in that voice and format looks it up by: a session started afterwards plays
// the pre-warmed clip with no synthesis.
func TestPrewarmClips_SessionPlaysThePrewarmedClip(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	cache := cueclip.NewMemoryCache(1 << 20)
	texts := []string{"Prewarmed one.", "Prewarmed two.", "Prewarmed one.", ""}

	require.NoError(t, PrewarmClips(context.Background(), cache, svc, "ALLOY", pcm24k, texts))

	assert.Equal(t, 1, svc.calls("Prewarmed one."))
	assert.Equal(t, 1, svc.calls("Prewarmed two."))
	assert.Zero(t, svc.calls(""), "an empty text is not rendered")
	assert.Equal(t, "alloy", svc.lastConfig().Voice, "the service voice matching the session voice")
	for _, text := range []string{"Prewarmed one.", "Prewarmed two."} {
		got, ok, err := cache.Get(context.Background(), cueclip.KeyFor(text, "alloy", "tts:fake", pcm24k))
		require.NoError(t, err)
		require.True(t, ok, "%q is cached", text)
		assert.Equal(t, cueFaded(pcm, 24000), got.PCM)
		assert.Equal(t, cueclip.Meta{Text: text, Voice: "alloy", Source: "tts:fake"}, got.Meta)
	}

	quiet := newFakeTTS(cueRamp(10))
	quiet.voices = svc.voices
	cue := cueStageForSession(t, Config{TTS: quiet, Cache: cache}, spokenSession("alloy", 24000))
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
	cache := cueclip.NewMemoryCache(1 << 20)

	err := PrewarmClips(context.Background(), cache, svc, "alloy", pcm24k, []string{"Will fail.", "Will pass."})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"Will fail."`)
	assert.Contains(t, err.Error(), "synthesis failed")
	assert.NotContains(t, err.Error(), "Will pass.")
	_, ok, _ := cache.Get(context.Background(), cueclip.KeyFor("Will pass.", "alloy", "tts:fake", pcm24k))
	assert.True(t, ok, "the other text is cached")
	_, ok, _ = cache.Get(context.Background(), cueclip.KeyFor("Will fail.", "alloy", "tts:fake", pcm24k))
	assert.False(t, ok)
}

// liveSessionTTS is a live session offered as a TTS service.
type liveSessionTTS struct {
	*fakeTTS
	*providersmock.MockStreamSession
}

// PrewarmClips needs a TTS service that renders PCM.
func TestPrewarmClips_RefusesWhatCannotRender(t *testing.T) {
	err := PrewarmClips(context.Background(), cueclip.NewMemoryCache(1<<20), nil, "alloy", pcm24k, []string{"x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session-bound")

	live := liveSessionTTS{newFakeTTS(cueRamp(10)), providersmock.NewMockStreamSession()}
	err = PrewarmClips(context.Background(), cueclip.NewMemoryCache(1<<20), live, "alloy", pcm24k, []string{"x"})
	require.ErrorIs(t, err, ErrSessionBoundSource)
	assert.Contains(t, err.Error(), "session-bound")
	assert.Zero(t, live.calls("x"))

	mp3 := newFakeTTS(cueRamp(10))
	mp3.formats = mp3.formats[:1]
	err = PrewarmClips(context.Background(), cueclip.NewMemoryCache(1<<20), mp3, "alloy", pcm24k, []string{"x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "renders no PCM")
	assert.Zero(t, mp3.calls("x"))
}

// A directory pre-warmed with a TTS service is what a session with no TTS
// service plays from, read-only: the pre-warmed clip, in the session's voice,
// with no render. A text not pre-warmed, or another voice, plays the earcon.
func TestPrewarmClips_DirCacheServesASessionWithoutTTS(t *testing.T) {
	dir := t.TempDir()
	w, err := cueclip.NewDirCache(dir)
	require.NoError(t, err)
	pcm := cueRamp(4800)
	require.NoError(t, PrewarmClips(context.Background(), w, newFakeTTS(pcm), "alloy", pcm24k,
		[]string{"Offline one."}))

	ro, err := cueclip.NewDirCache(dir, cueclip.ReadOnly())
	require.NoError(t, err)
	cue := cueStageForSession(t, Config{Cache: ro}, spokenSession("Alloy", 24000))
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, "Offline one."))
	assert.Equal(t, earconPCM(24000), playedFor(t, cue, "Not pre-warmed."))

	other := cueStageForSession(t, Config{Cache: ro}, spokenSession("verse", 24000))
	assert.Equal(t, earconPCM(24000), playedFor(t, other, "Offline one."), "another voice is another clip")
}

// A service with no voice matching the requested one renders in its default
// voice: that clip serves a session with the service (keyed by the default
// voice), but is not kept for the requested voice, so a session in that voice
// with no TTS service plays the earcon rather than another voice's clip.
func TestPrewarmClips_UnmatchedVoiceIsNotKeptForThatVoice(t *testing.T) {
	logs := captureCueLogs(t)
	cache := cueclip.NewMemoryCache(1 << 20)
	pcm := cueRamp(4800)
	require.NoError(t, PrewarmClips(context.Background(), cache, newFakeTTS(pcm), "Puck", pcm24k,
		[]string{"Unmatched voice."}))

	_, ok, err := cache.Get(context.Background(), cueclip.KeyFor("Unmatched voice.", "", "tts:fake", pcm24k))
	require.NoError(t, err)
	assert.True(t, ok, "kept under the service's default voice")
	cue := cueStageForSession(t, Config{Cache: cache}, spokenSession("Puck", 24000))
	assert.Equal(t, earconPCM(24000), playedFor(t, cue, "Unmatched voice."))
	assert.Contains(t, logs.String(), "no voice matching")
	assert.Contains(t, logs.String(), "not kept for sessions without a TTS service")
}

// One TTS service per voice and format: a second service's different audio
// for a text already pre-warmed for that voice is refused, naming the voice,
// and the first service's clip stays.
func TestPrewarmClips_SecondServiceForAVoiceIsRefused(t *testing.T) {
	cache := cueclip.NewMemoryCache(1 << 20)
	first := cueRamp(4800)
	require.NoError(t, PrewarmClips(context.Background(), cache, newFakeTTS(first), "alloy", pcm24k,
		[]string{"Contested."}))

	other := &otherTTS{newFakeTTS(cueRamp(2400))}
	err := PrewarmClips(context.Background(), cache, other, "alloy", pcm24k, []string{"Contested."})
	require.ErrorIs(t, err, ErrVoicePrewarmedByAnotherService)
	assert.Contains(t, err.Error(), `"alloy"`)

	require.NoError(t, PrewarmClips(context.Background(), cache, newFakeTTS(first), "alloy", pcm24k,
		[]string{"Contested."}), "the same service again is fine")
	cue := cueStageForSession(t, Config{Cache: cache}, spokenSession("alloy", 24000))
	assert.Equal(t, cueFaded(first, 24000), playedFor(t, cue, "Contested."))
}

// otherTTS is fakeTTS under another service name.
type otherTTS struct{ *fakeTTS }

func (otherTTS) Name() string { return "other" }

// A session with a TTS service that renders no PCM does not play clips another
// service pre-warmed: it plays the earcon.
func TestPrewarmClips_SessionWithANonPCMServiceDoesNotUseThem(t *testing.T) {
	cache := cueclip.NewMemoryCache(1 << 20)
	require.NoError(t, PrewarmClips(context.Background(), cache, newFakeTTS(cueRamp(4800)), "alloy", pcm24k,
		[]string{"Not for MP3."}))
	mp3 := newFakeTTS(cueRamp(10))
	mp3.formats = mp3.formats[:1]

	cue := cueStageForSession(t, Config{TTS: mp3, Cache: cache}, spokenSession("alloy", 24000))
	assert.Equal(t, earconPCM(24000), playedFor(t, cue, "Not for MP3."))
}

// stubSource is a clip source of a given kind, for the source order.
type stubSource struct{ name string }

func (s stubSource) kind() string          { return s.name }
func (stubSource) voice() string           { return "" }
func (stubSource) keyText(t string) string { return t }
func (stubSource) render(context.Context, string, cueclip.Format) (Clip, *clipRenderCost, error) {
	return Clip{}, nil, errNoClipSource
}

// sourceKinds is the kinds of sources, in order.
func sourceKinds(sources []clipSource) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, s.kind())
	}
	return out
}

// The sources are tried in a fixed order: the session's own out-of-band audio,
// then a configured TTS service that renders PCM, then (with no TTS service
// configured) the clips pre-warmed for the session's voice, then the earcon.
func TestClipSourcesFor_Order(t *testing.T) {
	out := providers.OutputAudio{Voice: "alloy", SampleRate: 24000}
	oob := stubSource{name: "session:test"}
	mp3 := newFakeTTS(nil)
	mp3.formats = mp3.formats[:1]
	cases := []struct {
		name string
		oob  clipSource
		svc  tts.Service
		want []string
	}{
		{"oob and tts", oob, newFakeTTS(nil), []string{"session:test", "tts:fake", "earcon"}},
		{"oob, no tts", oob, nil, []string{"session:test", "prewarmed", "earcon"}},
		{"tts", nil, newFakeTTS(nil), []string{"tts:fake", "earcon"}},
		{"no tts", nil, nil, []string{"prewarmed", "earcon"}},
		{"tts without PCM", nil, mp3, []string{"earcon"}},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, sourceKinds(clipSourcesFor(tc.oob, tc.svc, out)), tc.name)
	}
}
