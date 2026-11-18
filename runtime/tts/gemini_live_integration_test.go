//go:build integration

package tts_test

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// TestGeminiTTSLive_SynthesizesPCMInTheVoiceAsked confirms the Gemini API
// accepts the request shape the service sends and that the reply unwraps to
// speech: PCM16 samples of a plausible length with real energy, the WAV
// header and trailing C2PA chunk gone. An unknown voice is refused, which
// shows the voice is read rather than ignored.
//
// Run:
//
//	GEMINI_API_KEY=... go test -tags integration ./runtime/tts/ -run TestGeminiTTSLive -v
func TestGeminiTTSLive_SynthesizesPCMInTheVoiceAsked(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	svc := tts.NewGemini(key)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rc, err := svc.Synthesize(ctx, "Please hold on, I can't help with that.",
		tts.SynthesisConfig{Voice: "Kore", Format: tts.FormatPCM16, Language: "en-US"})
	require.NoError(t, err)
	pcm, err := io.ReadAll(rc)
	require.NoError(t, err)
	_ = rc.Close()

	seconds := float64(len(pcm)) / (24000 * 2)
	t.Logf("Kore: %d bytes, %.2fs at 24 kHz, rms %.0f", len(pcm), seconds, rms(pcm))
	assert.Zero(t, len(pcm)%2, "whole 16-bit samples")
	assert.Greater(t, seconds, 1.0)
	assert.Less(t, seconds, 10.0)
	assert.Greater(t, rms(pcm), 300.0, "speech, not silence or noise-free padding")
	assert.NotContains(t, string(pcm), "C2PA", "the WAV's trailing manifest is not audio")
	assert.NotContains(t, string(pcm[:min(len(pcm), 64)]), "RIFF")

	stream, err := svc.SynthesizeTTS(ctx, base.TTSRequest{Text: "One moment.", Voice: "Charon", SampleRate: 16000})
	require.NoError(t, err)
	at16k, _, err := base.ReadAllAudio(stream)
	require.NoError(t, err)
	t.Logf("Charon at 16 kHz: %d bytes, %.2fs", len(at16k), float64(len(at16k))/(16000*2))
	assert.Greater(t, len(at16k), 16000) // over half a second

	_, err = svc.Synthesize(ctx, "Hi", tts.SynthesisConfig{Voice: "NotAVoice"})
	var se *tts.SynthesisError
	require.True(t, errors.As(err, &se), "an unknown voice is refused: %v", err)
	t.Logf("unknown voice: %v", err)
}

func rms(pcm []byte) float64 {
	var sum float64
	n := len(pcm) / 2
	for i := 0; i < n; i++ {
		s := float64(int16(uint16(pcm[2*i]) | uint16(pcm[2*i+1])<<8))
		sum += s * s
	}
	if n == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(n))
}
