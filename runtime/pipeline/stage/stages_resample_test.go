package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAudioResampleStage(t *testing.T) {
	config := DefaultAudioResampleConfig()
	stage := NewAudioResampleStage(config)

	assert.NotNil(t, stage)
	assert.Equal(t, "audio-resample", stage.Name())
	assert.Equal(t, StageTypeTransform, stage.Type())
	assert.Equal(t, 16000, stage.GetConfig().TargetSampleRate)
}

func TestAudioResampleStage_Passthrough(t *testing.T) {
	// Test that audio at target rate passes through unchanged
	config := DefaultAudioResampleConfig()
	config.TargetSampleRate = 16000
	stage := NewAudioResampleStage(config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create test audio at target rate
	audioData := make([]byte, 3200) // 100ms of 16kHz mono audio
	for i := range audioData {
		audioData[i] = byte(i % 256)
	}

	input <- StreamElement{
		Audio: &AudioData{
			Samples:    audioData,
			SampleRate: 16000,
			Format:     AudioFormatPCM16,
		},
	}
	close(input)

	// Process
	go func() {
		stage.Process(ctx, input, output)
	}()

	// Verify output
	select {
	case elem := <-output:
		require.NotNil(t, elem.Audio)
		assert.Equal(t, audioData, elem.Audio.Samples, "Audio should pass through unchanged")
		assert.Equal(t, 16000, elem.Audio.SampleRate)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

func TestAudioResampleStage_Downsample(t *testing.T) {
	// Test downsampling from 24kHz to 16kHz
	config := DefaultAudioResampleConfig()
	config.TargetSampleRate = 16000
	stage := NewAudioResampleStage(config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create test audio at 24kHz (4800 samples = 100ms at 24kHz)
	// Each sample is 2 bytes for PCM16
	inputSamples := 4800
	audioData := make([]byte, inputSamples*2)
	for i := 0; i < inputSamples; i++ {
		// Simple pattern for testing
		audioData[i*2] = byte(i % 256)
		audioData[i*2+1] = byte((i / 256) % 256)
	}

	input <- StreamElement{
		Audio: &AudioData{
			Samples:    audioData,
			SampleRate: 24000,
			Format:     AudioFormatPCM16,
		},
	}
	close(input)

	// Process
	go func() {
		stage.Process(ctx, input, output)
	}()

	// Verify output
	select {
	case elem := <-output:
		require.NotNil(t, elem.Audio)
		// 24kHz -> 16kHz = 2/3 of the samples
		expectedSamples := int(float64(inputSamples) * 16000.0 / 24000.0)
		assert.Equal(t, expectedSamples*2, len(elem.Audio.Samples))
		assert.Equal(t, 16000, elem.Audio.SampleRate)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

func TestAudioResampleStage_Upsample(t *testing.T) {
	// Test upsampling from 16kHz to 24kHz
	config := AudioResampleConfig{
		TargetSampleRate:      24000,
		PassthroughIfSameRate: true,
	}
	stage := NewAudioResampleStage(config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create test audio at 16kHz (1600 samples = 100ms at 16kHz)
	inputSamples := 1600
	audioData := make([]byte, inputSamples*2)
	for i := 0; i < inputSamples; i++ {
		audioData[i*2] = byte(i % 256)
		audioData[i*2+1] = byte((i / 256) % 256)
	}

	input <- StreamElement{
		Audio: &AudioData{
			Samples:    audioData,
			SampleRate: 16000,
			Format:     AudioFormatPCM16,
		},
	}
	close(input)

	// Process
	go func() {
		stage.Process(ctx, input, output)
	}()

	// Verify output
	select {
	case elem := <-output:
		require.NotNil(t, elem.Audio)
		// 16kHz -> 24kHz = 1.5x the samples
		expectedSamples := int(float64(inputSamples) * 24000.0 / 16000.0)
		assert.Equal(t, expectedSamples*2, len(elem.Audio.Samples))
		assert.Equal(t, 24000, elem.Audio.SampleRate)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

func TestAudioResampleStage_NoAudio(t *testing.T) {
	// Test that elements without audio pass through
	config := DefaultAudioResampleConfig()
	stage := NewAudioResampleStage(config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	text := "Hello world"
	input <- StreamElement{
		Text: &text,
	}
	close(input)

	// Process
	go func() {
		stage.Process(ctx, input, output)
	}()

	// Verify output
	select {
	case elem := <-output:
		assert.Nil(t, elem.Audio)
		require.NotNil(t, elem.Text)
		assert.Equal(t, "Hello world", *elem.Text)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

func TestAudioResampleStage_UnsupportedFormat(t *testing.T) {
	// Test that non-PCM16 formats produce an error
	config := DefaultAudioResampleConfig()
	stage := NewAudioResampleStage(config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- StreamElement{
		Audio: &AudioData{
			Samples:    []byte{1, 2, 3, 4},
			SampleRate: 24000,
			Format:     AudioFormatOpus, // Unsupported
		},
	}
	close(input)

	// Process
	go func() {
		stage.Process(ctx, input, output)
	}()

	// Verify output has error
	select {
	case elem := <-output:
		assert.NotNil(t, elem.Error)
		assert.Contains(t, elem.Error.Error(), "PCM16")
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for output")
	}
}
