package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var pcm24k = ClipFormat{Encoding: AudioFormatPCM16, SampleRate: 24000, Channels: 1}

// A clip's key changes with anything that changes its audio: text, voice,
// source kind, encoding, rate or channels. Surrounding space and Unicode
// normalization do not change it.
func TestClipKeyFor(t *testing.T) {
	base := ClipKeyFor("Café.", "alloy", "tts:openai", pcm24k)
	differ := map[string]ClipKey{
		"text":        ClipKeyFor("Café!", "alloy", "tts:openai", pcm24k),
		"voice":       ClipKeyFor("Café.", "verse", "tts:openai", pcm24k),
		"source kind": ClipKeyFor("Café.", "alloy", "tts:elevenlabs", pcm24k),
		"encoding":    ClipKeyFor("Café.", "alloy", "tts:openai", ClipFormat{AudioFormatOpus, 24000, 1}),
		"rate":        ClipKeyFor("Café.", "alloy", "tts:openai", ClipFormat{AudioFormatPCM16, 16000, 1}),
		"channels":    ClipKeyFor("Café.", "alloy", "tts:openai", ClipFormat{AudioFormatPCM16, 24000, 2}),
		// Field boundaries are part of the key: moving a character from the
		// voice to the source kind is another clip.
		"boundary": ClipKeyFor("Café.", "allo", "ytts:openai", pcm24k),
	}
	for name, key := range differ {
		assert.NotEqual(t, base, key, name)
	}
	same := map[string]ClipKey{
		"surrounding space": ClipKeyFor("  Café.\n", "alloy", "tts:openai", pcm24k),
		"decomposed é":      ClipKeyFor("Café.", "alloy", "tts:openai", pcm24k),
	}
	for name, key := range same {
		assert.Equal(t, base, key, name)
	}
}

func bytesClip(n int, fill byte) CueClip {
	b := make([]byte, n)
	for i := range b {
		b[i] = fill
	}
	return cueClip(b, 24000)
}

func keyOf(name string) ClipKey { return ClipKeyFor(name, "", "test", pcm24k) }

// The memory cache keeps clips up to its byte bound, evicting the least
// recently used first.
func TestMemoryClipCache_EvictsByBytes(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryClipCache(1000)
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
func TestMemoryClipCache_Miss(t *testing.T) {
	got, ok, err := NewMemoryClipCache(10).Get(context.Background(), keyOf("none"))
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, CueClip{}, got)
}
