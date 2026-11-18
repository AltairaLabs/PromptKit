package sdk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	rtprompt "github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// writeTwoPromptPack is a pack of two prompts: "main" bans "Bruno" with
// brunoMessage; "other" bans "Mirabel" with no message of its own, so it
// blocks with the default message.
func writeTwoPromptPack(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "two.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"name": "two",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "You are a helpful assistant.",
				"validators": [
					{"type": "banned_words", "enabled": true, "message": "We don't talk about Bruno.",
					 "params": {"words": ["Bruno"]}}
				]
			},
			"other": {
				"system_template": "You are another assistant.",
				"validators": [
					{"type": "banned_words", "enabled": true, "params": {"words": ["Mirabel"]}}
				]
			}
		}
	}`), 0o600))
	return path
}

// PrewarmCueClips renders every prompt's guardrail messages and the default
// blocked message, each once. A conversation opened afterwards on the same
// cache plays the pre-warmed clip on a cut and never asks the service.
func TestPrewarmCueClips_ConversationPlaysThePrewarmedClip(t *testing.T) {
	speech := newCueTTS(cueRate / 4)
	cache := stage.NewMemoryClipCache(1 << 20)
	packPath := writeTwoPromptPack(t)

	require.NoError(t, PrewarmCueClips(context.Background(), packPath, speech, "alloy",
		WithSkipSchemaValidation(), WithCueClipCache(cache)))

	assert.Equal(t, 1, speech.calls(brunoMessage))
	assert.Equal(t, 1, speech.calls(rtprompt.DefaultBlockedMessage))
	speech.mu.Lock()
	assert.Len(t, speech.texts, 2, "nothing else is rendered: %q", speech.texts)
	speech.mu.Unlock()

	s := openCueSession(t, packPath, spokenProvider(), WithTTS(speech), WithCueClipCache(cache))
	want := speech.played(brunoMessage)
	r := s.cutBruno(t, len(want))

	assert.Equal(t, want, r.cue, "the pre-warmed bytes")
	assert.Equal(t, 1, speech.calls(brunoMessage), "the service is not called during the conversation")
	assert.Equal(t, 1, speech.calls(rtprompt.DefaultBlockedMessage))
}

// A directory pre-warmed with a TTS service serves a conversation opened on it
// read-only with no TTS service: the cut plays the same bytes.
func TestPrewarmCueClips_DirCacheServesAConversationWithoutTTS(t *testing.T) {
	dir := t.TempDir()
	w, err := stage.NewDirClipCache(dir)
	require.NoError(t, err)
	speech := newCueTTS(cueRate / 4)
	packPath := writeBrunoPack(t)
	require.NoError(t, PrewarmCueClips(context.Background(), packPath, speech, "alloy",
		WithSkipSchemaValidation(), WithCueClipCache(w)))

	ro, err := stage.NewDirClipCache(dir, stage.DirCacheReadOnly())
	require.NoError(t, err)
	s := openCueSession(t, packPath, spokenProvider(), WithCueClipCache(ro))
	want := speech.played(brunoMessage)
	r := s.cutBruno(t, len(want))

	assert.Equal(t, want, r.cue)
	assert.Equal(t, 1, speech.calls(brunoMessage))
}

// defaultCacheTTS is cueTTS under a name of its own.
type defaultCacheTTS struct {
	*cueTTS
	name string
}

func (s defaultCacheTTS) Name() string { return s.name }

// defaultCacheRuns numbers the runs of a test that uses the process-wide
// default cache, which outlives a run (-count=N).
var defaultCacheRuns atomic.Int64

// Without a cache option the clips go to the process-wide default cache that
// conversations use by default.
func TestPrewarmCueClips_DefaultCache(t *testing.T) {
	// A service name no other test or earlier run renders with, so the
	// shared default cache holds nothing of it yet.
	speech := defaultCacheTTS{newCueTTS(cueRate / 4),
		fmt.Sprintf("cue-prewarm-default-%d", defaultCacheRuns.Add(1))}
	packPath := writeBrunoPack(t)
	require.NoError(t, PrewarmCueClips(context.Background(), packPath, speech, "alloy", WithSkipSchemaValidation()))

	s := openCueSession(t, packPath, spokenProvider(), WithTTS(speech))
	want := speech.played(brunoMessage)
	r := s.cutBruno(t, len(want))
	assert.Equal(t, want, r.cue)
	assert.Equal(t, 1, speech.calls(brunoMessage))
}

// A session-bound source cannot be pre-warmed.
func TestPrewarmCueClips_SessionBoundSourceIsRefused(t *testing.T) {
	err := PrewarmCueClips(context.Background(), writeBrunoPack(t), nil, "alloy", WithSkipSchemaValidation())
	require.ErrorIs(t, err, stage.ErrSessionBoundSource)
	assert.Contains(t, err.Error(), "session-bound")

	live := struct {
		*cueTTS
		*mock.MockStreamSession
	}{newCueTTS(10), mock.NewMockStreamSession()}
	err = PrewarmCueClips(context.Background(), writeBrunoPack(t), live, "alloy", WithSkipSchemaValidation())
	require.ErrorIs(t, err, stage.ErrSessionBoundSource)
	assert.Zero(t, live.calls(brunoMessage))
}

// A pack that cannot be loaded is an error naming it.
func TestPrewarmCueClips_MissingPack(t *testing.T) {
	err := PrewarmCueClips(context.Background(), filepath.Join(t.TempDir(), "none.pack.json"), newCueTTS(10), "alloy")
	require.ErrorIs(t, err, ErrPackNotFound)
}

// A guardrail given as an option (WithGuardrail) has its message pre-warmed
// too.
func TestPrewarmCueClips_HostGuardrailMessage(t *testing.T) {
	speech := newCueTTS(cueRate / 4)
	const hostMessage = "Let's keep this about the weather."
	cache := stage.NewMemoryClipCache(1 << 20)
	require.NoError(t, PrewarmCueClips(context.Background(), writeBrunoPack(t), speech, "alloy",
		WithSkipSchemaValidation(), WithCueClipCache(cache),
		WithGuardrail(guardrails.Output("banned_words", map[string]any{"words": []any{"rain"}},
			guardrails.WithMessage(hostMessage)))))

	clip, ok, err := cache.Get(context.Background(), stage.ClipKeyFor(hostMessage, "alloy", "tts:"+speech.Name(),
		stage.ClipFormat{Encoding: stage.AudioFormatPCM16, SampleRate: cueRate, Channels: 1}))
	require.NoError(t, err)
	require.True(t, ok, "the host guardrail's message is cached")
	assert.Equal(t, speech.played(hostMessage), clip.Audio.Samples)
	assert.Equal(t, 1, speech.calls(brunoMessage))
}
