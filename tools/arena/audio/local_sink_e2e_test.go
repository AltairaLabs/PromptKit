package audio

import (
	"testing"
	"time"
)

// TestLocalSink_LeftChannelMatchesPushedFrames is the regression test for
// "audio plays choppy through oto". A LocalSink fed only left-channel
// frames (the user-direction case) should emit exactly those samples on
// the left side of the stereo stream — interleaved with silence on the
// right — with no gaps or skipped samples.
//
// We reuse the synchronous fakeOtoContext from local_sink_test.go and
// pull Read ourselves at audio-rate cadence so timing artefacts (drops,
// reorders) surface as byte mismatches.
func TestLocalSink_LeftChannelMatchesPushedFrames(t *testing.T) {
	const rate = 24000
	fake := &fakeOtoContext{}
	sink, err := NewLocalSink(LocalSinkConfig{
		Rate: rate,
		newContext: func(_, _ int) (otoContext, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("NewLocalSink: %v", err)
	}
	defer sink.Close()

	// Push 200 ms of audio in 13.3 ms frames.
	const frames = 15
	const samplesPerFrame = 320
	pushed := make([]int16, 0, frames*samplesPerFrame)
	for f := 0; f < frames; f++ {
		samples := make([]int16, samplesPerFrame)
		for i := range samples {
			samples[i] = int16(1000 + f*100 + i) // distinguishable per-frame pattern
		}
		pushed = append(pushed, samples...)
		sink.Push(Frame{Direction: DirectionInput, Samples: samples})
	}

	// Drain everything via repeated Reads. 4 KB per Read → 1024 stereo
	// pairs → 1024 mono samples per channel. We bound by total expected
	// samples so a stuck Read can't hang the test.
	const stereoBufSize = 4096
	captured := make([]byte, 0, len(pushed)*4) // each mono sample → 4 stereo bytes
	deadline := time.Now().Add(2 * time.Second)
	for len(captured) < len(pushed)*4 && time.Now().Before(deadline) {
		buf := make([]byte, stereoBufSize)
		n, _ := fake.reader.Read(buf)
		if n == 0 {
			break
		}
		captured = append(captured, buf[:n]...)
	}

	if len(captured) < len(pushed)*4 {
		t.Fatalf("captured %d stereo bytes, expected at least %d (drained too few)",
			len(captured), len(pushed)*4)
	}

	leftSamples := decodeLeftChannel(captured[:len(pushed)*4])
	for i, want := range pushed {
		if leftSamples[i] != want {
			t.Fatalf("sample %d: got %d, want %d (gap or reorder around offset %d)",
				i, leftSamples[i], want, i)
		}
	}
}

// decodeLeftChannel pulls the left samples out of an interleaved stereo
// s16le buffer. Layout: [L0_lo, L0_hi, R0_lo, R0_hi, L1_lo, L1_hi, ...].
func decodeLeftChannel(stereo []byte) []int16 {
	const stereoSampleSize = 4 // 2 bytes × 2 channels
	out := make([]int16, len(stereo)/stereoSampleSize)
	for i := range out {
		off := i * stereoSampleSize
		out[i] = int16(stereo[off]) | int16(stereo[off+1])<<8
	}
	return out
}
