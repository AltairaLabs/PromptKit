// Package audio provides audio processing utilities.
package audio

import (
	"encoding/binary"
	"fmt"
)

// Standard audio sample rates for common use cases.
const (
	SampleRate24kHz = 24000 // Common TTS output rate
	SampleRate16kHz = 16000 // Common STT/ASR input rate
)

// ResamplePCM16 resamples PCM16 audio data from one sample rate to another.
// Uses linear interpolation for reasonable quality resampling.
// Input and output are little-endian 16-bit signed PCM samples.
func ResamplePCM16(input []byte, fromRate, toRate int) ([]byte, error) {
	if fromRate <= 0 || toRate <= 0 {
		return nil, fmt.Errorf("invalid sample rates: from=%d, to=%d", fromRate, toRate)
	}

	if fromRate == toRate {
		// No resampling needed, return a copy
		result := make([]byte, len(input))
		copy(result, input)
		return result, nil
	}

	// Each sample is 2 bytes (16-bit)
	const bytesPerSample = 2
	if len(input)%bytesPerSample != 0 {
		return nil, fmt.Errorf("input length %d is not a multiple of %d bytes per sample", len(input), bytesPerSample)
	}

	numInputSamples := len(input) / bytesPerSample
	if numInputSamples == 0 {
		return []byte{}, nil
	}

	// Calculate output size
	numOutputSamples := int(float64(numInputSamples) * float64(toRate) / float64(fromRate))
	if numOutputSamples == 0 {
		return []byte{}, nil
	}

	// Convert input bytes to samples
	// Note: The uint16->int16 conversion is safe because PCM16 audio uses
	// the full int16 range (-32768 to 32767) stored as unsigned bytes.
	inputSamples := make([]int16, numInputSamples)
	for i := 0; i < numInputSamples; i++ {
		inputSamples[i] = int16(binary.LittleEndian.Uint16(input[i*bytesPerSample:])) //nolint:gosec // Safe PCM16 conversion
	}

	// Resample using linear interpolation
	outputSamples := make([]int16, numOutputSamples)
	ratio := float64(fromRate) / float64(toRate)

	for i := 0; i < numOutputSamples; i++ {
		// Calculate the position in the input
		srcPos := float64(i) * ratio
		srcIdx := int(srcPos)
		frac := srcPos - float64(srcIdx)

		if srcIdx >= numInputSamples-1 {
			// At or past the last sample, use the last sample
			outputSamples[i] = inputSamples[numInputSamples-1]
		} else {
			// Linear interpolation between two samples
			s0 := float64(inputSamples[srcIdx])
			s1 := float64(inputSamples[srcIdx+1])
			outputSamples[i] = int16(s0 + frac*(s1-s0))
		}
	}

	// Convert output samples to bytes
	// Note: The int16->uint16 conversion is safe because we're storing PCM16 samples
	// where the full int16 range maps to uint16 for byte encoding.
	output := make([]byte, numOutputSamples*bytesPerSample)
	for i := 0; i < numOutputSamples; i++ {
		//nolint:gosec // Safe PCM16 conversion
		binary.LittleEndian.PutUint16(output[i*bytesPerSample:], uint16(outputSamples[i]))
	}

	return output, nil
}

// Resample24kTo16k is a convenience function for the common case of
// resampling from 24kHz (TTS output) to 16kHz (Gemini input).
func Resample24kTo16k(input []byte) ([]byte, error) {
	return ResamplePCM16(input, SampleRate24kHz, SampleRate16kHz)
}
