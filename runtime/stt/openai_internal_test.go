package stt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateAudioSeconds_PCM(t *testing.T) {
	// 1 second at 16kHz, mono, 16-bit = 32_000 bytes.
	nc := &normalizedConfig{format: FormatPCM, sampleRate: 16000, channels: 1, bitDepth: 16}
	got := estimateAudioSeconds(make([]byte, 32000), "", nc)
	assert.InDelta(t, 1.0, got, 1e-9)
}

func TestEstimateAudioSeconds_WAV(t *testing.T) {
	nc := &normalizedConfig{format: FormatWAV, sampleRate: 16000, channels: 1, bitDepth: 16}
	got := estimateAudioSeconds(make([]byte, 16000), "", nc)
	assert.InDelta(t, 0.5, got, 1e-9)
}

func TestEstimateAudioSeconds_ZeroSampleRate(t *testing.T) {
	nc := &normalizedConfig{format: FormatPCM, sampleRate: 0, channels: 1, bitDepth: 16}
	assert.Equal(t, 0.0, estimateAudioSeconds(make([]byte, 1000), "", nc))
}

func TestEstimateAudioSeconds_ZeroChannels(t *testing.T) {
	nc := &normalizedConfig{format: FormatPCM, sampleRate: 16000, channels: 0, bitDepth: 16}
	assert.Equal(t, 0.0, estimateAudioSeconds(make([]byte, 1000), "", nc))
}

func TestEstimateAudioSeconds_NonPCM(t *testing.T) {
	nc := &normalizedConfig{format: FormatMP3, sampleRate: 16000, channels: 1, bitDepth: 16}
	assert.Equal(t, 0.0, estimateAudioSeconds(make([]byte, 1000), "audio/mpeg", nc))
}
