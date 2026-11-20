package sdk

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// ownVoiceTTS is a TTS service with voices of its own, like ElevenLabs or
// Cartesia: it speaks in its default voice when asked for none and refuses a
// voice it does not have, such as OpenAI's "alloy". It records the voice of
// every synthesis it does.
type ownVoiceTTS struct {
	convMockTTSService

	mu     sync.Mutex
	voices []string // voice of each synthesis, "" for the service's default
}

const ownVoice = "own-voice-1"

func (o *ownVoiceTTS) SupportedVoices() []tts.Voice { return []tts.Voice{{ID: ownVoice, Name: "Own"}} }

func (o *ownVoiceTTS) Synthesize(ctx context.Context, text string, cfg tts.SynthesisConfig) (io.ReadCloser, error) {
	if cfg.Voice != "" && cfg.Voice != ownVoice {
		return nil, fmt.Errorf("a voice with voice_id %q was not found", cfg.Voice)
	}
	o.mu.Lock()
	o.voices = append(o.voices, cfg.Voice)
	o.mu.Unlock()
	return o.convMockTTSService.Synthesize(ctx, text, cfg)
}

func (o *ownVoiceTTS) spokenVoices() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.voices)
}

func (o *ownVoiceTTS) waitForSpoken(n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(o.spokenVoices()) >= n {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// openVADModeWithTTSConfig opens a VAD-mode duplex conversation on ttsSvc
// with cfg (nil for the defaults) and drains its Response().
func openVADModeWithTTSConfig(t *testing.T, ttsSvc tts.Service, cfg *VADModeConfig) *Conversation {
	t.Helper()
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(&turnRecordingProvider{}),
		WithSkipSchemaValidation(),
		WithVADMode(newScriptedSTT("what is the capital of france"), ttsSvc, cfg),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	responseCh, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for range responseCh { //nolint:revive // drained so the output stage never blocks
		}
	}()
	return conv
}

// A VAD session on a TTS service other than OpenAI's, opened with no voice,
// speaks its replies in that service's own default voice. It used to ask
// every service for OpenAI's "alloy", which ElevenLabs, Cartesia and Gemini
// TTS do not have, so every reply failed to synthesize and the caller heard
// nothing.
func TestVADMode_NoVoiceSpeaksInTheServicesDefaultVoice(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	for name, cfg := range map[string]*VADModeConfig{
		"nil config": nil,
		"no voice": {
			SilenceDuration: 300 * time.Millisecond, MinSpeechDuration: 100 * time.Millisecond,
			MaxTurnDuration: 5 * time.Second, SampleRate: perTurnTestSampleRate,
		},
	} {
		t.Run(name, func(t *testing.T) {
			ttsSvc := &ownVoiceTTS{convMockTTSService: *newConvMockTTSService()}
			conv := openVADModeWithTTSConfig(t, ttsSvc, cfg)

			speakOneUtterance(t, conv)
			// The default silence duration (800 ms) is longer than the
			// utterance's trailing silence.
			for i := 0; i < 30; i++ {
				require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
					MediaData: &providers.StreamMediaData{
						MIMEType: "audio/pcm", Data: pcmSilence(20*time.Millisecond, perTurnTestSampleRate)},
				}))
			}

			require.True(t, ttsSvc.waitForSpoken(1, 8*time.Second), "the reply must be spoken")
			assert.Equal(t, []string{""}, ttsSvc.spokenVoices(),
				"the service is asked for no voice, so it speaks in its own default")
		})
	}
}

// A voice named in the config is the voice the service is asked for.
func TestVADMode_ConfiguredVoiceIsTheVoiceSpoken(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	ttsSvc := &ownVoiceTTS{convMockTTSService: *newConvMockTTSService()}
	conv := openVADModeWithTTSConfig(t, ttsSvc, &VADModeConfig{
		SilenceDuration: 300 * time.Millisecond, MinSpeechDuration: 100 * time.Millisecond,
		MaxTurnDuration: 5 * time.Second, SampleRate: perTurnTestSampleRate, Voice: ownVoice,
	})

	speakOneUtterance(t, conv)

	require.True(t, ttsSvc.waitForSpoken(1, 8*time.Second), "the reply must be spoken")
	assert.Equal(t, []string{ownVoice}, ttsSvc.spokenVoices())
}
