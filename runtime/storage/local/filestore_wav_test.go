package local

import (
	"encoding/binary"
	"testing"
)

func TestWrapPCMInWAV(t *testing.T) {
	// Test data: 100 bytes of PCM audio
	pcmData := make([]byte, 100)
	for i := range pcmData {
		pcmData[i] = byte(i % 256)
	}

	// Call wrapPCMInWAV
	wavData := wrapPCMInWAV(pcmData, 24000, 16, 1)

	// Verify WAV header
	if len(wavData) != wavHeaderSize+len(pcmData) {
		t.Errorf("Expected WAV size %d, got %d", wavHeaderSize+len(pcmData), len(wavData))
	}

	// Check RIFF header
	if string(wavData[0:4]) != "RIFF" {
		t.Errorf("Expected RIFF header, got %s", string(wavData[0:4]))
	}

	// Check WAVE format
	if string(wavData[8:12]) != "WAVE" {
		t.Errorf("Expected WAVE format, got %s", string(wavData[8:12]))
	}

	// Check fmt chunk
	if string(wavData[12:16]) != "fmt " {
		t.Errorf("Expected 'fmt ' chunk, got %s", string(wavData[12:16]))
	}

	// Check audio format (PCM = 1)
	audioFormat := binary.LittleEndian.Uint16(wavData[20:22])
	if audioFormat != 1 {
		t.Errorf("Expected PCM format (1), got %d", audioFormat)
	}

	// Check sample rate
	sampleRate := binary.LittleEndian.Uint32(wavData[24:28])
	if sampleRate != 24000 {
		t.Errorf("Expected sample rate 24000, got %d", sampleRate)
	}

	// Check bits per sample
	bitsPerSample := binary.LittleEndian.Uint16(wavData[34:36])
	if bitsPerSample != 16 {
		t.Errorf("Expected 16 bits per sample, got %d", bitsPerSample)
	}

	// Check data chunk
	if string(wavData[36:40]) != "data" {
		t.Errorf("Expected 'data' chunk, got %s", string(wavData[36:40]))
	}

	// Check data size
	dataSize := binary.LittleEndian.Uint32(wavData[40:44])
	if dataSize != uint32(len(pcmData)) {
		t.Errorf("Expected data size %d, got %d", len(pcmData), dataSize)
	}

	// Verify PCM data is preserved
	for i := range pcmData {
		if wavData[wavHeaderSize+i] != pcmData[i] {
			t.Errorf("PCM data mismatch at byte %d", i)
			break
		}
	}
}

func TestIsPCMAudio(t *testing.T) {
	tests := []struct {
		mimeType string
		expected bool
	}{
		{audioMIMETypePCM, true},
		{audioMIMETypeL16, true},
		{"audio/wav", false},
		{"audio/mp3", false},
		{"audio/mpeg", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			result := isPCMAudio(tt.mimeType)
			if result != tt.expected {
				t.Errorf("isPCMAudio(%q) = %v, want %v", tt.mimeType, result, tt.expected)
			}
		})
	}
}

func TestWrapPCMInWAVDifferentSampleRates(t *testing.T) {
	tests := []struct {
		name          string
		sampleRate    int
		bitsPerSample int
		numChannels   int
	}{
		{"16kHz mono 16-bit", 16000, 16, 1},
		{"24kHz mono 16-bit", 24000, 16, 1},
		{"44.1kHz stereo 16-bit", 44100, 16, 2},
		{"48kHz mono 24-bit", 48000, 24, 1},
	}

	pcmData := make([]byte, 1000)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wavData := wrapPCMInWAV(pcmData, tt.sampleRate, tt.bitsPerSample, tt.numChannels)

			// Verify header integrity
			if string(wavData[0:4]) != "RIFF" {
				t.Error("Invalid RIFF header")
			}

			// Verify sample rate in header
			headerSampleRate := binary.LittleEndian.Uint32(wavData[24:28])
			if int(headerSampleRate) != tt.sampleRate {
				t.Errorf("Sample rate mismatch: expected %d, got %d", tt.sampleRate, headerSampleRate)
			}

			// Verify channels
			headerChannels := binary.LittleEndian.Uint16(wavData[22:24])
			if int(headerChannels) != tt.numChannels {
				t.Errorf("Channels mismatch: expected %d, got %d", tt.numChannels, headerChannels)
			}

			// Verify bits per sample
			headerBits := binary.LittleEndian.Uint16(wavData[34:36])
			if int(headerBits) != tt.bitsPerSample {
				t.Errorf("Bits per sample mismatch: expected %d, got %d", tt.bitsPerSample, headerBits)
			}
		})
	}
}
