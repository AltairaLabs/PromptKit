package cueclip

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var pcm24k = Format{SampleRate: 24000, Channels: 1}

// A clip's key changes with anything that changes its audio: text, voice,
// source kind, rate or channels. Surrounding space and Unicode
// normalization do not change it.
func TestKeyFor(t *testing.T) {
	base := KeyFor("Café.", "alloy", "tts:openai", pcm24k)
	differ := map[string]Key{
		"text":        KeyFor("Café!", "alloy", "tts:openai", pcm24k),
		"voice":       KeyFor("Café.", "verse", "tts:openai", pcm24k),
		"source kind": KeyFor("Café.", "alloy", "tts:elevenlabs", pcm24k),
		"rate":        KeyFor("Café.", "alloy", "tts:openai", Format{16000, 1}),
		"channels":    KeyFor("Café.", "alloy", "tts:openai", Format{24000, 2}),
		// Field boundaries are part of the key: moving a character from the
		// voice to the source kind is another clip.
		"boundary": KeyFor("Café.", "allo", "ytts:openai", pcm24k),
	}
	for name, key := range differ {
		assert.NotEqual(t, base, key, name)
	}
	same := map[string]Key{
		"surrounding space": KeyFor("  Café.\n", "alloy", "tts:openai", pcm24k),
		"decomposed é":      KeyFor("Café.", "alloy", "tts:openai", pcm24k),
	}
	for name, key := range same {
		assert.Equal(t, base, key, name)
	}
}

func bytesClip(n int, fill byte) Clip {
	b := make([]byte, n)
	for i := range b {
		b[i] = fill
	}
	return Clip{PCM: b, Format: pcm24k}
}

func keyOf(name string) Key { return KeyFor(name, "", "test", pcm24k) }

// The memory cache keeps clips up to its byte bound, evicting the least
// recently used first.
func TestMemoryCache_EvictsByBytes(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(1000)
	require.NoError(t, c.Put(ctx, keyOf("a"), bytesClip(400, 'a')))
	require.NoError(t, c.Put(ctx, keyOf("b"), bytesClip(400, 'b')))
	_, ok, err := c.Get(ctx, keyOf("a")) // a is now the most recently used
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, c.Put(ctx, keyOf("c"), bytesClip(400, 'c'))) // 1200 bytes: b goes

	present := func(name string) bool {
		_, ok, err := c.Get(ctx, keyOf(name))
		require.NoError(t, err)
		return ok
	}
	assert.False(t, present("b"))
	got, ok, err := c.Get(ctx, keyOf("a"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, bytesClip(400, 'a'), got)
	assert.True(t, present("c"))

	// Replacing a clip counts its new size only.
	require.NoError(t, c.Put(ctx, keyOf("c"), bytesClip(600, 'C')))
	assert.True(t, present("a"), "400 + 600 is within the bound")
	require.NoError(t, c.Put(ctx, keyOf("c"), bytesClip(700, 'C')))
	assert.True(t, present("c"))
	assert.False(t, present("a"), "400 + 700 is over the bound: the oldest goes")

	// A clip over the whole bound is not kept, and evicts nothing.
	require.NoError(t, c.Put(ctx, keyOf("big"), bytesClip(1001, 'x')))
	assert.False(t, present("big"))
	got, ok, err = c.Get(ctx, keyOf("c"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, bytesClip(700, 'C'), got)
}

// A miss is (zero, false, nil).
func TestMemoryCache_Miss(t *testing.T) {
	got, ok, err := NewMemoryCache(10).Get(context.Background(), keyOf("none"))
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, Clip{}, got)
}

// A key is the same bytes from release to release, so a directory of clips
// rendered earlier keeps playing.
func TestKeyFor_IsStable(t *testing.T) {
	key := KeyFor("We don't talk about Bruno.", "alloy", "tts:openai", pcm24k)
	assert.Equal(t, "a8fff64bd5aff98bd873a4e2383c21c4f5bd78fb22725af123e908ace16fb615", hex.EncodeToString(key[:]))
}
