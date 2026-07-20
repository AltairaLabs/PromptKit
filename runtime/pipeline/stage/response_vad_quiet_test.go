package stage_test

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// ttsPCM returns `samples` of sine at the given RMS, for 24kHz TTS output.
func ttsPCM(samples int, targetRMS float64) []byte {
	amp := targetRMS * math.Sqrt2
	b := make([]byte, samples*2)
	for i := range samples {
		v := int16(amp * 32767 * math.Sin(float64(i)*0.2))
		binary.LittleEndian.PutUint16(b[i*2:], uint16(v))
	}
	return b
}

// TestResponseVADStage_HoldsEndOfStreamThroughQuietPlayback covers the stage
// releasing EndOfStream while the assistant is still speaking.
//
// ResponseVADStage delays EndOfStream until the response audio stops, so a
// consumer does not treat a turn as finished mid-sentence. That depends
// entirely on its VAD recognizing playback as speech: if the audio reads as
// silence, the silence timer starts immediately and EndOfStream is released
// while audio is still streaming.
//
// A fixed-threshold VAD does not detect TTS at 0.02 RMS at all — measured NOT
// DETECTED against these very params — so any provider rendering at a low level
// ends its turns early.
func TestResponseVADStage_HoldsEndOfStreamThroughQuietPlayback(t *testing.T) {
	cfg := stage.DefaultResponseVADConfig()
	cfg.SampleRate = 24000
	cfg.SilenceDuration = 300 * time.Millisecond

	s, err := stage.NewResponseVADStage(cfg)
	if err != nil {
		t.Fatalf("NewResponseVADStage: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 64)
	go func() { _ = s.Process(ctx, input, output) }()

	const chunkSamples = 2400 // 100 ms @ 24kHz
	quietTTS := func() stage.StreamElement {
		return makeAudioElement(ttsPCM(chunkSamples, 0.02), 24000)
	}

	// Playback is real-time paced: this stage's silence window is wall-clock.
	go func() {
		for range 5 {
			input <- quietTTS()
			time.Sleep(100 * time.Millisecond)
		}
		// Provider signals end of response while audio is still streaming.
		input <- stage.StreamElement{EndOfStream: true}
		// Playback continues far past SilenceDuration (1.5s vs 300ms), so a
		// stage that starts its silence window immediately releases EndOfStream
		// well inside the observation period rather than marginally at its edge.
		for range 15 {
			input <- quietTTS()
			time.Sleep(100 * time.Millisecond)
		}
		close(input)
	}()

	// Watch for EndOfStream arriving while playback is still in flight.
	deadline := time.After(1400 * time.Millisecond)
	for {
		select {
		case elem, ok := <-output:
			if !ok {
				return
			}
			if elem.EndOfStream {
				t.Fatal("EndOfStream released while quiet TTS playback was still streaming; " +
					"the stage's VAD is not detecting low-level TTS as speech, so the " +
					"silence window starts immediately and turns end mid-sentence")
			}
		case <-deadline:
			return // held correctly for the duration of playback
		case <-ctx.Done():
			return
		}
	}
}
