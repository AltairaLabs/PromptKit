package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/voice"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// The facade's wrappers of internal/voice's constructors: each default config
// is internal/voice's, and each stage is built under its own name, so a
// stage.X caller sees no difference from before the move (#2876).

func TestFacade_VoiceDefaultConfigs(t *testing.T) {
	assert.Equal(t, voice.DefaultAudioResampleConfig(), DefaultAudioResampleConfig())
	assert.Equal(t, voice.DefaultAudioTurnConfig(), DefaultAudioTurnConfig())
	assert.Equal(t, voice.DefaultSTTStageConfig(), DefaultSTTStageConfig())
	assert.Equal(t, voice.DefaultTTSStageWithInterruptionConfig(), DefaultTTSStageWithInterruptionConfig())
	assert.Equal(t, voice.DefaultResponseVADConfig(), DefaultResponseVADConfig())
	assert.Equal(t, voice.DefaultTTSConfig(), DefaultTTSConfig())
	assert.Equal(t, 16000, DefaultAudioResampleConfig().TargetSampleRate)
}

// facadeSTT answers every transcription with "hello".
type facadeSTT struct{}

func (facadeSTT) Name() string                      { return "facade-stt" }
func (facadeSTT) Type() base.ProviderType           { return base.ProviderTypeSTT }
func (facadeSTT) Pricing() *base.PricingDescriptor  { return nil }
func (facadeSTT) Validate() error                   { return nil }
func (facadeSTT) Init(context.Context) error        { return nil }
func (facadeSTT) HealthCheck(context.Context) error { return nil }
func (facadeSTT) Close() error                      { return nil }
func (facadeSTT) Transcribe(context.Context, base.STTRequest) (base.STTResponse, error) {
	return base.STTResponse{Text: "hello"}, nil
}

// facadeTTS is a TTSService that answers no audio.
type facadeTTS struct{}

func (facadeTTS) Synthesize(context.Context, string) ([]byte, error) { return nil, nil }
func (facadeTTS) MIMEType() string                                   { return "audio/pcm" }

func TestFacade_VoiceStagesKeepTheirName(t *testing.T) {
	assert.Equal(t, "audio-pacing", NewAudioPacingStage().Name())
	assert.Equal(t, "audio-pacing-output", NewNamedAudioPacingStage("audio-pacing-output").Name())

	resample := AudioResampleConfig{TargetSampleRate: 24000, PassthroughIfSameRate: true}
	rs := NewAudioResampleStage(resample)
	assert.Equal(t, resample, rs.GetConfig())
	assert.Equal(t, "audio-resample", rs.Name())

	turn, err := NewAudioTurnStage(DefaultAudioTurnConfig())
	require.NoError(t, err)
	assert.Equal(t, "audio_turn", turn.Name())
	assert.Equal(t, StageTypeAccumulate, turn.Type())

	assert.Equal(t, "stt", NewSTTStage(facadeSTT{}, DefaultSTTStageConfig()).Name())
	emitting := NewSTTStageWithEmitter(facadeSTT{}, DefaultSTTStageConfig(), events.NewEmitter(nil, "", "", ""))
	assert.Equal(t, "stt", emitting.Name())

	tts := NewTTSStageWithInterruption(newFakeTTS(nil), DefaultTTSStageWithInterruptionConfig())
	assert.Equal(t, "tts_interruptible", tts.Name())

	vad, err := NewResponseVADStage(DefaultResponseVADConfig())
	require.NoError(t, err)
	assert.Equal(t, "response_vad", vad.Name())

	assert.Equal(t, "tts", NewTTSStage(facadeTTS{}, DefaultTTSConfig()).Name())
}
