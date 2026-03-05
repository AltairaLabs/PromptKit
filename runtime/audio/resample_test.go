package audio

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// BenchmarkResamplePCM16_Pooled benchmarks resampling with pooled buffers (current implementation).
func BenchmarkResamplePCM16_Pooled(b *testing.B) {
	// 20ms of audio at 24kHz = 480 samples = 960 bytes (typical real-time chunk)
	numSamples := 480
	input := make([]byte, numSamples*2)
	for i := 0; i < numSamples; i++ {
		binary.LittleEndian.PutUint16(input[i*2:], uint16(i%32768))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := ResamplePCM16(input, SampleRate24kHz, SampleRate16kHz)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkResamplePCM16_LargeChunk benchmarks resampling a larger audio chunk.
func BenchmarkResamplePCM16_LargeChunk(b *testing.B) {
	// 1 second of audio at 24kHz = 24000 samples = 48000 bytes
	numSamples := 24000
	input := make([]byte, numSamples*2)
	for i := 0; i < numSamples; i++ {
		binary.LittleEndian.PutUint16(input[i*2:], uint16(i%32768))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := ResamplePCM16(input, SampleRate24kHz, SampleRate16kHz)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestResamplePCM16_ConcurrentPoolSafety(t *testing.T) {
	// Verify that pooled buffers don't cause data corruption under concurrent use
	numSamples := 100
	input := make([]byte, numSamples*2)
	for i := 0; i < numSamples; i++ {
		binary.LittleEndian.PutUint16(input[i*2:], uint16(i*100))
	}

	const goroutines = 10
	errs := make(chan error, goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < 50; i++ {
				output, err := ResamplePCM16(input, SampleRate24kHz, SampleRate16kHz)
				if err != nil {
					errs <- err
					return
				}
				expectedSamples := int(float64(numSamples) * float64(SampleRate16kHz) / float64(SampleRate24kHz))
				if len(output)/2 != expectedSamples {
					errs <- fmt.Errorf("unexpected output samples: got %d, want %d", len(output)/2, expectedSamples)
					return
				}
			}
			errs <- nil
		}()
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent resample failed: %v", err)
		}
	}
}

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
