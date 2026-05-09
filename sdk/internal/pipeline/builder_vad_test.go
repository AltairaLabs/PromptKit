package pipeline

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSTTService implements base.STTProvider for testing
type mockSTTService struct{}

func (m *mockSTTService) Name() string                        { return "mock-stt" }
func (m *mockSTTService) Type() base.ProviderType             { return base.ProviderTypeSTT }
func (m *mockSTTService) Pricing() *base.PricingDescriptor    { return nil }
func (m *mockSTTService) Validate() error                     { return nil }
func (m *mockSTTService) Init(_ context.Context) error        { return nil }
func (m *mockSTTService) HealthCheck(_ context.Context) error { return nil }
func (m *mockSTTService) Close() error                        { return nil }

func (m *mockSTTService) Transcribe(_ context.Context, _ base.STTRequest) (base.STTResponse, error) {
	return base.STTResponse{Text: "test transcription"}, nil
}

// mockTTSService implements tts.Service for testing
type mockTTSService struct{}

func (m *mockTTSService) Name() string { return "mock-tts" }
func (m *mockTTSService) Synthesize(ctx context.Context, text string, config tts.SynthesisConfig) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("audio data")), nil
}
func (m *mockTTSService) SupportedVoices() []tts.Voice {
	return []tts.Voice{{ID: "voice1", Name: "Test Voice"}}
}
func (m *mockTTSService) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{tts.FormatMP3}
}

// mockVADAnalyzer implements audio.VADAnalyzer for testing
type mockVADAnalyzer struct{}

func (m *mockVADAnalyzer) Name() string { return "mock-vad" }
func (m *mockVADAnalyzer) Analyze(ctx context.Context, audioData []byte) (float64, error) {
	return 0.5, nil
}
func (m *mockVADAnalyzer) State() audio.VADState {
	return audio.VADStateQuiet
}
func (m *mockVADAnalyzer) OnStateChange() <-chan audio.VADEvent {
	ch := make(chan audio.VADEvent)
	close(ch)
	return ch
}
func (m *mockVADAnalyzer) Reset() {}

// mockTurnDetector implements audio.TurnDetector for testing
type mockTurnDetector struct{}

func (m *mockTurnDetector) Name() string { return "mock-turn" }
func (m *mockTurnDetector) ProcessAudio(ctx context.Context, audioData []byte) (bool, error) {
	return false, nil
}
func (m *mockTurnDetector) ProcessVADState(ctx context.Context, state audio.VADState) (bool, error) {
	return false, nil
}
func (m *mockTurnDetector) IsUserSpeaking() bool { return false }
func (m *mockTurnDetector) Reset()               {}

func TestBuildVADPipelineStages(t *testing.T) {
	t.Run("builds VAD pipeline with all stages", func(t *testing.T) {
		provider := mock.NewProvider("test", "test-model", false)
		sttService := &mockSTTService{}
		ttsService := &mockTTSService{}
		vadAnalyzer := &mockVADAnalyzer{}
		turnDetector := &mockTurnDetector{}

		vadConfig := stage.AudioTurnConfig{
			VAD:          vadAnalyzer,
			TurnDetector: turnDetector,
		}

		cfg := &Config{
			Provider:    provider,
			VADConfig:   &vadConfig,
			STTService:  sttService,
			TTSService:  ttsService,
			MaxTokens:   1000,
			Temperature: 0.7,
		}

		stages, err := buildVADPipelineStages(cfg)
		require.NoError(t, err)
		assert.Len(t, stages, 4) // AudioTurn, STT, Provider, TTS
	})

	t.Run("builds VAD pipeline without provider", func(t *testing.T) {
		sttService := &mockSTTService{}
		ttsService := &mockTTSService{}
		vadAnalyzer := &mockVADAnalyzer{}
		turnDetector := &mockTurnDetector{}

		vadConfig := stage.AudioTurnConfig{
			VAD:          vadAnalyzer,
			TurnDetector: turnDetector,
		}

		cfg := &Config{
			VADConfig:  &vadConfig,
			STTService: sttService,
			TTSService: ttsService,
		}

		stages, err := buildVADPipelineStages(cfg)
		require.NoError(t, err)
		assert.Len(t, stages, 3) // AudioTurn, STT, TTS (no Provider)
	})

	t.Run("uses custom STT and TTS configs", func(t *testing.T) {
		provider := mock.NewProvider("test", "test-model", false)
		sttService := &mockSTTService{}
		ttsService := &mockTTSService{}
		vadAnalyzer := &mockVADAnalyzer{}
		turnDetector := &mockTurnDetector{}

		vadConfig := stage.AudioTurnConfig{
			VAD:          vadAnalyzer,
			TurnDetector: turnDetector,
		}

		sttStageConfig := stage.STTStageConfig{
			Language: "en",
		}

		ttsStageConfig := stage.TTSStageWithInterruptionConfig{
			Voice: "custom-voice",
		}

		cfg := &Config{
			Provider:    provider,
			VADConfig:   &vadConfig,
			STTService:  sttService,
			STTConfig:   &sttStageConfig,
			TTSService:  ttsService,
			TTSConfig:   &ttsStageConfig,
			MaxTokens:   2000,
			Temperature: 0.5,
		}

		stages, err := buildVADPipelineStages(cfg)
		require.NoError(t, err)
		assert.Len(t, stages, 4)
	})

	t.Run("returns error on invalid AudioTurnStage config", func(t *testing.T) {
		sttService := &mockSTTService{}
		ttsService := &mockTTSService{}

		// Invalid config - nil VADConfig pointer dereference
		cfg := &Config{
			VADConfig:  nil, // This will cause error
			STTService: sttService,
			TTSService: ttsService,
		}

		// This should panic or error when trying to dereference nil VADConfig
		assert.Panics(t, func() {
			_, _ = buildVADPipelineStages(cfg)
		})
	})
}
