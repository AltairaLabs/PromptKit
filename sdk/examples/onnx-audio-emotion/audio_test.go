package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// makeWAV builds a minimal canonical PCM16 mono WAV for the given samples.
func makeWAV(t *testing.T, sampleRate int, channels int, bits int, pcm []int16) []byte {
	t.Helper()
	var buf bytes.Buffer
	dataLen := len(pcm) * 2
	byteRate := sampleRate * channels * (bits / 8)
	blockAlign := channels * (bits / 8)
	w := func(v any) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	buf.WriteString("RIFF")
	w(uint32(36 + dataLen)) //nolint:gosec
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	w(uint32(16))         // PCM fmt chunk size
	w(uint16(1))          // audio format = PCM
	w(uint16(channels))   //nolint:gosec
	w(uint32(sampleRate)) //nolint:gosec
	w(uint32(byteRate))   //nolint:gosec
	w(uint16(blockAlign)) //nolint:gosec
	w(uint16(bits))       //nolint:gosec
	buf.WriteString("data")
	w(uint32(dataLen)) //nolint:gosec
	for _, s := range pcm {
		w(s)
	}
	return buf.Bytes()
}

func TestDecodeWAV_Mono16k(t *testing.T) {
	pcm := []int16{0, 16384, -16384, 32767, -32768}
	wav := makeWAV(t, 16000, 1, 16, pcm)

	samples, rate, err := decodeWAV(wav)
	if err != nil {
		t.Fatalf("decodeWAV: %v", err)
	}
	if rate != 16000 {
		t.Fatalf("rate = %d, want 16000", rate)
	}
	if len(samples) != len(pcm) {
		t.Fatalf("len(samples) = %d, want %d", len(samples), len(pcm))
	}
	if math.Abs(float64(samples[1])-0.5) > 0.001 {
		t.Errorf("samples[1] = %f, want ~0.5", samples[1])
	}
	if math.Abs(float64(samples[2])-(-0.5)) > 0.001 {
		t.Errorf("samples[2] = %f, want ~-0.5", samples[2])
	}
}

func TestDecodeWAV_RejectsStereo(t *testing.T) {
	wav := makeWAV(t, 16000, 2, 16, []int16{0, 0})
	if _, _, err := decodeWAV(wav); err == nil {
		t.Fatal("expected error for stereo input, got nil")
	}
}

func TestDecodeWAV_RejectsWrongRate(t *testing.T) {
	wav := makeWAV(t, 44100, 1, 16, []int16{0, 0})
	if _, _, err := decodeWAV(wav); err == nil {
		t.Fatal("expected error for 44100 Hz input, got nil")
	}
}

func TestNormalize_ZeroMeanUnitVariance(t *testing.T) {
	out := normalize([]float32{1, 2, 3, 4, 5})
	var sum, sumsq float64
	for _, v := range out {
		sum += float64(v)
		sumsq += float64(v) * float64(v)
	}
	mean := sum / float64(len(out))
	variance := sumsq/float64(len(out)) - mean*mean
	if math.Abs(mean) > 1e-4 {
		t.Errorf("mean = %f, want ~0", mean)
	}
	if math.Abs(variance-1.0) > 1e-2 {
		t.Errorf("variance = %f, want ~1", variance)
	}
}
