package stage

import (
	"context"
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateCueFixtures = flag.Bool("update", false, "rewrite the clip fixtures under testdata/cues")

const cueFixtureDir = "testdata/cues"

// cueFixture is a clip checked in under testdata/cues: synthetic PCM (a ramp,
// so every byte differs from its neighbors and exact-byte assertions mean
// something), at the text, voice and source of the tests that use it.
type cueFixture struct {
	text, voice, source string
	rate, samples       int
}

func (f cueFixture) format() ClipFormat {
	return ClipFormat{Encoding: AudioFormatPCM16, SampleRate: f.rate, Channels: 1}
}
func (f cueFixture) key() ClipKey   { return ClipKeyFor(f.text, f.voice, f.source, f.format()) }
func (f cueFixture) pcm() []byte    { return cueRamp(f.samples) }
func (f cueFixture) clip() CueClip  { c := readyClip(f.pcm(), f.rate); c.Meta = f.meta(); return c }
func (f cueFixture) meta() ClipMeta { return ClipMeta{Text: f.text, Voice: f.voice, Source: f.source} }
func (f cueFixture) String() string { return f.text }
func cueFixtureCache(t *testing.T) ClipCache {
	t.Helper()
	c, err := NewDirClipCache(cueFixtureDir, DirCacheReadOnly())
	require.NoError(t, err)
	return c
}

var (
	// fixturePrewarmed is 100 ms at 24 kHz, in the fake TTS's voice.
	fixturePrewarmed = cueFixture{"Prewarmed.", "alloy", "tts:fake", 24000, 2400}
	// fixtureBruno is 250 ms at 24 kHz.
	fixtureBruno = cueFixture{brunoText, "alloy", "tts:fake", 24000, 6000}
	// fixtureBrunoNarrow is the same text at 16 kHz, a clip of another key.
	fixtureBrunoNarrow = cueFixture{brunoText, "alloy", "tts:fake", 16000, 4000}

	cueFixtures = []cueFixture{fixturePrewarmed, fixtureBruno, fixtureBrunoNarrow}
)

// With -update it rewrites testdata/cues from cueFixtures:
//
//	go -C runtime test ./pipeline/stage -run TestRegenerateCueFixtures -update
//
// Without it, the checked-in directory must hold exactly cueFixtures, byte for
// byte, so the fixtures cannot drift from what the tests expect.
func TestRegenerateCueFixtures(t *testing.T) {
	if *updateCueFixtures {
		require.NoError(t, os.RemoveAll(cueFixtureDir))
		w, err := NewDirClipCache(cueFixtureDir)
		require.NoError(t, err)
		for _, f := range cueFixtures {
			require.NoError(t, w.Put(context.Background(), f.key(), f.clip()))
		}
	}
	r := cueFixtureCache(t)
	var want []string
	for _, f := range cueFixtures {
		got, ok, err := r.Get(context.Background(), f.key())
		require.NoError(t, err)
		require.True(t, ok, "fixture %q is missing: regenerate with -update", f.text)
		assert.Equal(t, f.pcm(), got.Audio.Samples, f.text)
		assert.Equal(t, f.meta(), got.Meta, f.text)
		want = append(want, keyHex(f.key())+".pcm", keyHex(f.key())+".json")
	}
	entries, err := os.ReadDir(cueFixtureDir)
	require.NoError(t, err)
	var have []string
	for _, e := range entries {
		have = append(have, e.Name())
	}
	assert.ElementsMatch(t, want, have)
}
