package stage_test

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSTTStage(t *testing.T) {
	s := stage.NewSTTStage(&mockSTTService{}, stage.DefaultSTTStageConfig())

	assert.Equal(t, stage.StageTypeTransform, s.Type())
	assert.Equal(t, "stt", s.Name())
}

func TestDefaultSTTStageConfig(t *testing.T) {
	config := stage.DefaultSTTStageConfig()

	assert.Equal(t, "en", config.Language)
	assert.True(t, config.SkipEmpty)
	assert.NotZero(t, config.MinAudioBytes, "MinAudioBytes should have a default value")
}

func TestSTTStage_TranscribesAudio(t *testing.T) {
	mock := &mockSTTService{
		transcribeFunc: func(_ context.Context, _ []byte, _ stt.TranscriptionConfig) (string, error) {
			return "Hello from transcription", nil
		},
	}
	s := stage.NewSTTStage(mock, stage.DefaultSTTStageConfig())

	inputs := []stage.StreamElement{
		makeAudioElement(generateTestPCMAudio(32000), 16000),
	}
	results := runStage(t, s, inputs, 2*time.Second)

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Text)
	assert.Equal(t, "Hello from transcription", *results[0].Text)
}

func TestSTTStage_TranscriptionError(t *testing.T) {
	mock := &mockSTTService{
		transcribeFunc: func(_ context.Context, _ []byte, _ stt.TranscriptionConfig) (string, error) {
			return "", context.DeadlineExceeded
		},
	}
	s := stage.NewSTTStage(mock, stage.DefaultSTTStageConfig())

	inputs := []stage.StreamElement{
		makeAudioElement(generateTestPCMAudio(32000), 16000),
	}
	results := runStage(t, s, inputs, 2*time.Second)

	require.Len(t, results, 1)
	assert.NotNil(t, results[0].Error, "Expected error element for transcription failure")
}

func TestSTTStage_EmptyTranscription(t *testing.T) {
	mock := &mockSTTService{
		transcribeFunc: func(_ context.Context, _ []byte, _ stt.TranscriptionConfig) (string, error) {
			return "   ", nil // Whitespace only
		},
	}
	config := stage.DefaultSTTStageConfig()
	config.SkipEmpty = true
	s := stage.NewSTTStage(mock, config)

	inputs := []stage.StreamElement{
		makeAudioElement(generateTestPCMAudio(32000), 16000),
	}
	results := runStage(t, s, inputs, 2*time.Second)

	// Empty/whitespace transcription should not produce output
	for _, r := range results {
		assert.Nil(t, r.Text, "Whitespace transcription should be skipped")
	}
}

func TestSTTStage_SkipsSmallAudio(t *testing.T) {
	transcribeCalled := false
	mock := &mockSTTService{
		transcribeFunc: func(_ context.Context, _ []byte, _ stt.TranscriptionConfig) (string, error) {
			transcribeCalled = true
			return "should not be called", nil
		},
	}
	config := stage.DefaultSTTStageConfig()
	config.MinAudioBytes = 1000

	s := stage.NewSTTStage(mock, config)

	inputs := []stage.StreamElement{
		makeAudioElement(generateTestPCMAudio(100), 16000), // 100 bytes < 1000 min
	}
	results := runStage(t, s, inputs, 2*time.Second)

	assert.False(t, transcribeCalled, "Transcribe should not be called for small audio")
	for _, r := range results {
		assert.Nil(t, r.Text, "No text output expected for small audio")
	}
}

func TestSTTStage_PassesThroughNonAudio(t *testing.T) {
	s := stage.NewSTTStage(&mockSTTService{}, stage.DefaultSTTStageConfig())

	inputs := []stage.StreamElement{
		makeTextElement("Pass through text"),
	}
	results := runStage(t, s, inputs, 2*time.Second)

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Text)
	assert.Equal(t, "Pass through text", *results[0].Text)
}

func TestSTTStage_EndOfStream(t *testing.T) {
	transcribeCalled := false
	mock := &mockSTTService{
		transcribeFunc: func(_ context.Context, _ []byte, _ stt.TranscriptionConfig) (string, error) {
			transcribeCalled = true
			return "", nil
		},
	}
	s := stage.NewSTTStage(mock, stage.DefaultSTTStageConfig())

	inputs := []stage.StreamElement{
		makeEndOfStreamElement(),
	}
	results := runStage(t, s, inputs, 2*time.Second)

	require.Len(t, results, 1)
	assert.True(t, results[0].EndOfStream, "EndOfStream should be forwarded")
	assert.False(t, transcribeCalled, "Transcribe should not be called for EndOfStream")
}

func TestSTTStage_PreservesMetadata(t *testing.T) {
	mock := &mockSTTService{
		transcribeFunc: func(_ context.Context, _ []byte, _ stt.TranscriptionConfig) (string, error) {
			return "Transcribed text", nil
		},
	}
	s := stage.NewSTTStage(mock, stage.DefaultSTTStageConfig())

	turnID := "abc123"
	audioElem := stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    generateTestPCMAudio(32000),
			SampleRate: 16000,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
		Meta: stage.ElementMetadata{TurnID: &turnID},
	}

	results := runStage(t, s, []stage.StreamElement{audioElem}, 2*time.Second)

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Text)
	require.NotNil(t, results[0].Meta.TurnID)
	assert.Equal(t, "abc123", *results[0].Meta.TurnID)
}

func TestSTTStage_PassesLanguageConfig(t *testing.T) {
	var capturedConfig stt.TranscriptionConfig
	mock := &mockSTTService{
		transcribeFunc: func(_ context.Context, _ []byte, cfg stt.TranscriptionConfig) (string, error) {
			capturedConfig = cfg
			return "Hola", nil
		},
	}
	config := stage.DefaultSTTStageConfig()
	config.Language = "es"

	s := stage.NewSTTStage(mock, config)

	inputs := []stage.StreamElement{
		makeAudioElement(generateTestPCMAudio(32000), 16000),
	}
	runStage(t, s, inputs, 2*time.Second)

	assert.Equal(t, "es", capturedConfig.Language, "Language config should be passed to transcribe")
}
