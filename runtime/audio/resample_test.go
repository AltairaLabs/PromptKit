package audio

import (
	"encoding/binary"
	"testing"
)

func TestResamplePCM16_SameRate(t *testing.T) {
	// Create a simple sine wave pattern
	input := make([]byte, 100)
	for i := 0; i < 50; i++ {
		binary.LittleEndian.PutUint16(input[i*2:], uint16(i*100))
	}

	output, err := ResamplePCM16(input, 16000, 16000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(output) != len(input) {
		t.Errorf("expected output length %d, got %d", len(input), len(output))
	}
}

func TestResamplePCM16_Downsample(t *testing.T) {
	// 24kHz to 16kHz should reduce samples by 2/3
	// 100 samples at 24kHz -> ~67 samples at 16kHz
	numInputSamples := 100
	input := make([]byte, numInputSamples*2)
	for i := 0; i < numInputSamples; i++ {
		binary.LittleEndian.PutUint16(input[i*2:], uint16(i*100))
	}

	output, err := ResamplePCM16(input, 24000, 16000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedSamples := int(float64(numInputSamples) * 16000 / 24000)
	actualSamples := len(output) / 2
	if actualSamples != expectedSamples {
		t.Errorf("expected %d output samples, got %d", expectedSamples, actualSamples)
	}
}

func TestResamplePCM16_Upsample(t *testing.T) {
	// 16kHz to 24kHz should increase samples by 3/2
	// 100 samples at 16kHz -> 150 samples at 24kHz
	numInputSamples := 100
	input := make([]byte, numInputSamples*2)
	for i := 0; i < numInputSamples; i++ {
		binary.LittleEndian.PutUint16(input[i*2:], uint16(i*100))
	}

	output, err := ResamplePCM16(input, 16000, 24000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedSamples := int(float64(numInputSamples) * 24000 / 16000)
	actualSamples := len(output) / 2
	if actualSamples != expectedSamples {
		t.Errorf("expected %d output samples, got %d", expectedSamples, actualSamples)
	}
}

func TestResamplePCM16_InvalidInput(t *testing.T) {
	// Odd number of bytes should error
	input := make([]byte, 101)
	_, err := ResamplePCM16(input, 24000, 16000)
	if err == nil {
		t.Error("expected error for odd byte count")
	}
}

func TestResamplePCM16_InvalidRates(t *testing.T) {
	input := make([]byte, 100)

	_, err := ResamplePCM16(input, 0, 16000)
	if err == nil {
		t.Error("expected error for zero from rate")
	}

	_, err = ResamplePCM16(input, 16000, 0)
	if err == nil {
		t.Error("expected error for zero to rate")
	}
}

func TestResample24kTo16k(t *testing.T) {
	// 24000 samples at 24kHz = 1 second
	// After resampling to 16kHz = 16000 samples
	numInputSamples := 24000
	input := make([]byte, numInputSamples*2)
	for i := 0; i < numInputSamples; i++ {
		binary.LittleEndian.PutUint16(input[i*2:], uint16(i%32768))
	}

	output, err := Resample24kTo16k(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedSamples := 16000
	actualSamples := len(output) / 2
	if actualSamples != expectedSamples {
		t.Errorf("expected %d output samples, got %d", expectedSamples, actualSamples)
	}
}
