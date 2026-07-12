package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// wantSampleRate is the only sample rate this example accepts. SER models
// expect 16 kHz mono; the classify AudioOptions contract makes delivering
// audio at the target rate the caller's responsibility, so we validate
// rather than resample.
const wantSampleRate = 16000

// decodeWAV parses a canonical little-endian PCM16 mono WAV and returns
// its samples scaled to [-1, 1) plus the declared sample rate. It rejects
// non-PCM, non-mono, non-16-bit, or non-16 kHz input with a clear error —
// the example does not resample or downmix.
func decodeWAV(b []byte) ([]float32, int, error) {
	if len(b) < 44 || !bytes.Equal(b[0:4], []byte("RIFF")) || !bytes.Equal(b[8:12], []byte("WAVE")) {
		return nil, 0, fmt.Errorf("not a RIFF/WAVE file")
	}
	var (
		audioFormat uint16
		channels    uint16
		sampleRate  uint32
		bits        uint16
		data        []byte
	)
	// Walk chunks starting after the 12-byte RIFF/WAVE header.
	off := 12
	for off+8 <= len(b) {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		body := off + 8
		if body+size > len(b) {
			return nil, 0, fmt.Errorf("chunk %q overruns file", id)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, fmt.Errorf("fmt chunk too small (%d bytes)", size)
			}
			audioFormat = binary.LittleEndian.Uint16(b[body : body+2])
			channels = binary.LittleEndian.Uint16(b[body+2 : body+4])
			sampleRate = binary.LittleEndian.Uint32(b[body+4 : body+8])
			bits = binary.LittleEndian.Uint16(b[body+14 : body+16])
		case "data":
			data = b[body : body+size]
		}
		off = body + size + (size & 1) // chunks are word-aligned
	}
	if audioFormat != 1 {
		return nil, 0, fmt.Errorf("unsupported WAV format %d (want PCM=1)", audioFormat)
	}
	if channels != 1 {
		return nil, 0, fmt.Errorf("unsupported channel count %d (want mono)", channels)
	}
	if bits != 16 {
		return nil, 0, fmt.Errorf("unsupported bit depth %d (want 16)", bits)
	}
	if int(sampleRate) != wantSampleRate {
		return nil, 0, fmt.Errorf("unsupported sample rate %d (want %d)", sampleRate, wantSampleRate)
	}
	if data == nil {
		return nil, 0, fmt.Errorf("no data chunk")
	}
	n := len(data) / 2
	samples := make([]float32, n)
	for i := range n {
		s := int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
		samples[i] = float32(s) / 32768.0
	}
	return samples, int(sampleRate), nil
}

// normalize applies zero-mean/unit-variance normalization — the transform
// HuggingFace's Wav2Vec2FeatureExtractor performs with do_normalize=True.
func normalize(samples []float32) []float32 {
	if len(samples) == 0 {
		return samples
	}
	var sum float64
	for _, v := range samples {
		sum += float64(v)
	}
	mean := sum / float64(len(samples))
	var varsum float64
	for _, v := range samples {
		d := float64(v) - mean
		varsum += d * d
	}
	std := math.Sqrt(varsum/float64(len(samples)) + 1e-7)
	out := make([]float32, len(samples))
	for i, v := range samples {
		out[i] = float32((float64(v) - mean) / std)
	}
	return out
}
