package cue

import (
	"context"
	"encoding/hex"
	"flag"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"

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

func (f cueFixture) format() cueclip.Format {
	return cueclip.Format{SampleRate: f.rate, Channels: 1}
}
func (f cueFixture) key() cueclip.Key { return cueclip.KeyFor(f.text, f.voice, f.source, f.format()) }
func (f cueFixture) pcm() []byte      { return cueRamp(f.samples) }
func (f cueFixture) clip() Clip       { c := readyClip(f.pcm(), f.rate); c.Meta = f.meta(); return c }
func (f cueFixture) meta() cueclip.Meta {
	return cueclip.Meta{Text: f.text, Voice: f.voice, Source: f.source}
}
func (f cueFixture) String() string { return f.text }

// pcm24k is the format of a clip a 24 kHz session plays.
var pcm24k = cueclip.Format{SampleRate: 24000, Channels: 1}

// keyHex is the name the directory cache gives key's files.
func keyHex(key cueclip.Key) string { return hex.EncodeToString(key[:]) }

func cueFixtureCache(t *testing.T) cueclip.Cache {
	t.Helper()
	c, err := cueclip.NewDirCache(cueFixtureDir, cueclip.ReadOnly())
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
//	go -C runtime test ./pipeline/stage/internal/cue -run TestRegenerateCueFixtures -update
//
// Without it, the checked-in directory must hold exactly cueFixtures, byte for
// byte, so the fixtures cannot drift from what the tests expect.
func TestRegenerateCueFixtures(t *testing.T) {
	if *updateCueFixtures {
		require.NoError(t, os.RemoveAll(cueFixtureDir))
		w, err := cueclip.NewDirCache(cueFixtureDir)
		require.NoError(t, err)
		for _, f := range cueFixtures {
			require.NoError(t, w.Put(context.Background(), f.key(), toCached(f.clip())))
		}
	}
	r := cueFixtureCache(t)
	var want []string
	for _, f := range cueFixtures {
		got, ok, err := r.Get(context.Background(), f.key())
		require.NoError(t, err)
		require.True(t, ok, "fixture %q is missing: regenerate with -update", f.text)
		assert.Equal(t, f.pcm(), got.PCM, f.text)
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
