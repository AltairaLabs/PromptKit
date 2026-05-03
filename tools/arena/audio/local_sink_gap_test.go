package audio

import (
	"encoding/binary"
	"testing"
	"time"
)

// TestLocalSink_NoGapsDuringRealtimePush verifies that pushing frames at
// realtime cadence produces a continuous left-channel stream — no silent
// runs longer than one frame's worth of samples. A gap longer than that
// is exactly what the user hears as choppiness.
//
// We push a non-zero (constant 5000) mono stream at chunk-duration
// intervals, then drain Read at audio rate and inspect the captured
// stereo bytes. If the LocalSink's per-direction queue is undersized,
// or oto's pull cadence misaligns with our push cadence, the captured
// left channel will contain runs of zeros (silence) where the sink
// failed to deliver a sample in time.
func TestLocalSink_NoGapsDuringRealtimePush(t *testing.T) {
	const rate = 24000
	const samplesPerFrame = 320 // 13.3 ms @ 24 kHz
	const frameInterval = time.Duration(samplesPerFrame) * time.Second / time.Duration(rate)
	const totalFrames = 30 // ~400 ms of audio

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

	// Drain Read at audio rate in a goroutine, accumulating left samples.
	captured := make([]int16, 0, totalFrames*samplesPerFrame*2)
	doneRead := make(chan struct{})
	stopRead := make(chan struct{})
	go func() {
		defer close(doneRead)
		// Read 5 ms worth of stereo samples per pull (matches a typical
		// oto pull). This is the rate at which Read is called.
		const pullSamples = rate / 200
		buf := make([]byte, pullSamples*4) // stereo s16le
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopRead:
				return
			case <-ticker.C:
				n, _ := fake.reader.Read(buf)
				for i := 0; i+3 < n; i += 4 {
					left := int16(binary.LittleEndian.Uint16(buf[i:]))
					captured = append(captured, left)
				}
			}
		}
	}()

	// Push a continuous non-zero stream at realtime cadence.
	frame := make([]int16, samplesPerFrame)
	for i := range frame {
		frame[i] = 5000
	}
	for f := 0; f < totalFrames; f++ {
		sink.Push(Frame{Direction: DirectionInput, Samples: frame})
		time.Sleep(frameInterval)
	}
	// Let the reader drain whatever is queued.
	time.Sleep(200 * time.Millisecond)
	close(stopRead)
	<-doneRead

	// Find the first non-zero sample (start of audio playback).
	startIdx := -1
	for i, v := range captured {
		if v != 0 {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		t.Fatal("no non-zero samples captured — sink delivered no audio at all")
	}

	// Find the last non-zero sample.
	endIdx := startIdx
	for i, v := range captured {
		if v != 0 {
			endIdx = i
		}
	}

	active := captured[startIdx : endIdx+1]
	expectedSamples := totalFrames * samplesPerFrame
	t.Logf("captured %d active samples (start..end), expected %d (totalFrames × samplesPerFrame)",
		len(active), expectedSamples)

	// Detect zero runs inside the active region. A single-sample
	// glitch is harmless; a run > 1 frame is an audible silent gap.
	const maxAllowedZeroRun = samplesPerFrame
	maxRun := 0
	maxRunStart := -1
	curRun := 0
	curRunStart := -1
	for i, v := range active {
		if v == 0 {
			if curRun == 0 {
				curRunStart = i
			}
			curRun++
			if curRun > maxRun {
				maxRun = curRun
				maxRunStart = curRunStart
			}
		} else {
			curRun = 0
		}
	}

	if maxRun > maxAllowedZeroRun {
		gapMs := float64(maxRun) * 1000.0 / float64(rate)
		t.Fatalf("detected silent gap of %d samples (%.1f ms) at active offset %d "+
			"(allowed: ≤%d samples ≈ %.1f ms / one frame). This means the "+
			"sink failed to deliver a sample in time — choppy playback.",
			maxRun, gapMs, maxRunStart,
			maxAllowedZeroRun, float64(maxAllowedZeroRun)*1000.0/float64(rate))
	}

	t.Logf("max silent run inside active region: %d samples (≤%d allowed)", maxRun, maxAllowedZeroRun)
}
