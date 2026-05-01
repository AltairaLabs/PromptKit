package audio

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

func TestMonitorTap_PassesElementsThrough(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	tap := NewMonitorTap(r, MonitorTapConfig{Position: stage.RecordingPositionOutput})

	in := make(chan stage.StreamElement, 1)
	out := make(chan stage.StreamElement, 1)
	done := make(chan error, 1)

	go func() {
		done <- tap.Process(context.Background(), in, out)
	}()

	elem := stage.StreamElement{Timestamp: time.Now()}
	in <- elem
	close(in)

	select {
	case got := <-out:
		if got.Timestamp != elem.Timestamp {
			t.Fatal("element not passed through correctly")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for element")
	}
	if err := <-done; err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
}

func TestMonitorTap_PublishesAudioToRouter(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	consumer := r.Subscribe("test", 10)

	tap := NewMonitorTap(r, MonitorTapConfig{Position: stage.RecordingPositionOutput})

	in := make(chan stage.StreamElement, 1)
	out := make(chan stage.StreamElement, 1)
	done := make(chan error, 1)

	go func() {
		done <- tap.Process(context.Background(), in, out)
	}()

	// Build a stream element with audio (2 mono samples s16le at 24 kHz)
	in <- buildAudioStreamElement(t, []byte{0x00, 0x10, 0x00, 0x20}, 24000, 1)
	close(in)

	// Drain output (we don't care what passed through, just that it did)
	<-out

	select {
	case frame := <-consumer:
		if frame.Direction != DirectionOutput {
			t.Fatalf("expected DirectionOutput, got %s", frame.Direction)
		}
		if len(frame.Samples) != 2 {
			t.Fatalf("expected 2 samples, got %d", len(frame.Samples))
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("router did not receive frame")
	}
	if err := <-done; err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
}

func TestMonitorTap_ResamplesOnRateMismatch(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	consumer := r.Subscribe("test", 10)

	tap := NewMonitorTap(r, MonitorTapConfig{Position: stage.RecordingPositionInput})

	in := make(chan stage.StreamElement, 1)
	out := make(chan stage.StreamElement, 1)
	done := make(chan error, 1)

	go func() {
		done <- tap.Process(context.Background(), in, out)
	}()

	// 4 samples at 16 kHz; resampled to 24 kHz, should produce ~6 samples.
	in <- buildAudioStreamElement(t, []byte{0x00, 0x10, 0x00, 0x20, 0x00, 0x30, 0x00, 0x40}, 16000, 1)
	close(in)
	<-out

	select {
	case frame := <-consumer:
		if frame.Direction != DirectionInput {
			t.Fatalf("expected DirectionInput, got %s", frame.Direction)
		}
		// Resampling 16k→24k inflates by 1.5x; 4 samples → ~6 samples.
		// Allow a tolerance for rounding edge cases.
		if len(frame.Samples) < 5 || len(frame.Samples) > 7 {
			t.Fatalf("expected ~6 resampled samples, got %d", len(frame.Samples))
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("router did not receive resampled frame")
	}
	if err := <-done; err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
}

func TestMonitorTap_NoAudioElementIsNoop(t *testing.T) {
	r := NewAudioRouter(Rate24k)
	defer r.Close()

	consumer := r.Subscribe("test", 10)

	tap := NewMonitorTap(r, MonitorTapConfig{Position: stage.RecordingPositionOutput})

	in := make(chan stage.StreamElement, 1)
	out := make(chan stage.StreamElement, 1)
	done := make(chan error, 1)

	go func() {
		done <- tap.Process(context.Background(), in, out)
	}()

	// Element with no Audio (e.g., text or message)
	in <- stage.StreamElement{Timestamp: time.Now()}
	close(in)
	<-out

	select {
	case f := <-consumer:
		t.Fatalf("expected no frame published; got %+v", f)
	case <-time.After(50 * time.Millisecond):
		// Expected: nothing published
	}
	if err := <-done; err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
}

// buildAudioStreamElement constructs a StreamElement carrying a mono s16le
// audio chunk. The actual audio type is stage.AudioData (see
// runtime/pipeline/stage/element.go).
func buildAudioStreamElement(t *testing.T, samples []byte, sampleRate, channels int) stage.StreamElement {
	t.Helper()
	return stage.StreamElement{
		Timestamp: time.Now(),
		Audio: &stage.AudioData{
			Samples:    samples,
			SampleRate: sampleRate,
			Channels:   channels,
			Format:     stage.AudioFormatPCM16,
		},
	}
}
