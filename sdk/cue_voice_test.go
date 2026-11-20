package sdk

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
)

// A realtime session's cue is spoken by the service given to WithCueVoice:
// the caller hears exactly its audio for the guardrail's message.
func TestWithCueVoice_VoicesTheGuardrailMessage(t *testing.T) {
	speech := newCueTTS(cueRate / 4)
	s := openCueSession(t, writeBrunoPack(t), spokenProvider(),
		WithCueVoice(speech, CueClipCache(cueclip.NewMemoryCache(1<<20))))

	r := s.cutBruno(t, len(speech.played(brunoMessage)))

	assert.Equal(t, speech.played(brunoMessage), r.cue)
	assert.Equal(t, 1, speech.calls(brunoMessage))
}

// WithTTS is the VAD reply voice. On a realtime session it voices no cue: the
// caller hears the earcon, the service is never asked, and Open warns once per
// process that the service is unused there, naming WithCueVoice.
func TestWithTTS_OnRealtimeDoesNotVoiceCues(t *testing.T) {
	ttsOnRealtimeWarned.Store(false)
	t.Cleanup(func() { ttsOnRealtimeWarned.Store(false) })
	logs := captureLogs(t)
	speech := newCueTTS(cueRate / 4)

	for i := range 2 {
		s := openCueSession(t, writeBrunoPack(t), spokenProvider(), WithTTS(speech),
			WithCueVoice(nil, CueClipCache(cueclip.NewMemoryCache(1<<20))))
		r := s.cutBruno(t, len(earcon(cueRate)))
		assert.Equal(t, earcon(cueRate), r.cue, "conversation %d plays the earcon", i+1)
		require.NoError(t, s.conv.Close())
	}

	assert.Zero(t, speech.calls(brunoMessage), "WithTTS's service is never asked for a cue")
	assert.Equal(t, 1, strings.Count(logs.String(),
		"the TTS service speaks VAD-mode replies and has no effect on a realtime session; "+
			"use WithCueVoice to voice its guardrail messages"),
		"warned once: %s", logs.String())
}

// WithCueVoice and WithTTS together on a realtime session: the cue voice
// speaks, and the TTS service stays unused.
func TestWithCueVoice_WinsOverWithTTSOnRealtime(t *testing.T) {
	reply := newCueTTS(cueRate / 2)
	cue := newCueTTS(cueRate / 4)
	s := openCueSession(t, writeBrunoPack(t), spokenProvider(), WithTTS(reply),
		WithCueVoice(cue, CueClipCache(cueclip.NewMemoryCache(1<<20))))

	r := s.cutBruno(t, len(cue.played(brunoMessage)))

	assert.Equal(t, cue.played(brunoMessage), r.cue)
	assert.Zero(t, reply.calls(brunoMessage))
}

// A VAD-mode session uses its TTS service to speak replies, so opening one
// with it warns of nothing.
func TestWithTTS_VADSessionDoesNotWarn(t *testing.T) {
	ttsOnRealtimeWarned.Store(false)
	t.Cleanup(func() { ttsOnRealtimeWarned.Store(false) })
	logs := captureLogs(t)

	conv, err := OpenDuplex(writeIngestionTestPack(t), "main", WithProvider(&recordingTextProvider{}),
		WithSkipSchemaValidation(), WithTTS(newCueTTS(cueRate)),
		WithVADMode(&convMockSTTService{}, nil, nil))
	require.NoError(t, err)
	require.NoError(t, conv.Close())

	assert.NotContains(t, logs.String(), "has no effect on a realtime session")
	assert.False(t, ttsOnRealtimeWarned.Load(), "the warning is still to come for a realtime session")
}
