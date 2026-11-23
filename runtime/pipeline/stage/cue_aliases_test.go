package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/cue"
)

// stage's PrewarmClips renders into the cache it is given, under the key a
// session in that voice looks the clip up by, as internal/cue's does.
func TestPrewarmClipsFacade_RendersIntoTheCache(t *testing.T) {
	pcm := cueRamp(4800)
	cache := cueclip.NewMemoryCache(1 << 20)
	require.NoError(t, PrewarmClips(context.Background(), cache, newFakeTTS(pcm), "Alloy", pcm24k,
		[]string{brunoText}))

	got, ok, err := cache.Get(context.Background(), cueclip.KeyFor(brunoText, "alloy", "tts:fake", pcm24k))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, cueFaded(pcm, cueTestRate), got.PCM)
	assert.Equal(t, cueclip.Meta{Text: brunoText, Voice: "alloy", Source: "tts:fake"}, got.Meta)
}

// With no TTS service, stage's PrewarmClips returns stage's
// ErrSessionBoundSource; stage's errors are internal/cue's, so errors.Is
// matches either.
func TestPrewarmClipsFacade_NoServiceIsSessionBound(t *testing.T) {
	err := PrewarmClips(context.Background(), nil, nil, "alloy", pcm24k, []string{brunoText})
	assert.Same(t, ErrSessionBoundSource, err)
	assert.Same(t, cue.ErrSessionBoundSource, ErrSessionBoundSource)
	assert.Same(t, cue.ErrVoicePrewarmedByAnotherService, ErrVoicePrewarmedByAnotherService)
}
