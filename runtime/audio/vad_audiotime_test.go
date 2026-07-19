package audio_test

import (
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// speechPCM returns 16-bit PCM sine at ~0.106 RMS, matching measured real speech
// levels, which the energy VADs treat as speech.
func speechPCM(samples int) []byte {
	b := make([]byte, samples*2)
	for i := range samples {
		v := int16(0.15 * 32767 * math.Sin(float64(i)*0.2))
		binary.LittleEndian.PutUint16(b[i*2:], uint16(v))
	}
	return b
}

// TestVADReachesSpeakingByAudioTimeNotWallClock covers VAD state transitions
// being timed by wall-clock.
//
// The state machine advances Starting -> Speaking once StartSecs has elapsed,
// but measures that against time.Now(). Delivery rate and audio duration are not
// the same thing: a file replayed faster than real time, a batch ingestion, or a
// test feeding buffered audio all elapse far less wall-clock than the audio they
// carry, so the VAD stalls in Starting and never reports Speaking.
//
// Consumers that key off VADStateSpeaking specifically — SilenceDetector sets
// userSpeaking only there, InterruptionHandler gates barge-in on it — then never
// fire. Transitions must be driven by the duration of audio analyzed.
func TestVADReachesSpeakingByAudioTimeNotWallClock(t *testing.T) {
	const sampleRate = 16000
	const chunkSamples = sampleRate / 10 // 100 ms

	cases := []struct {
		name string
		make func() (audio.VADAnalyzer, error)
	}{
		{name: "SimpleVAD", make: func() (audio.VADAnalyzer, error) {
			return audio.NewSimpleVAD(audio.DefaultVADParams())
		}},
		{name: "AdaptiveVAD", make: func() (audio.VADAnalyzer, error) {
			return audio.NewAdaptiveVAD(audio.DefaultVADParams())
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := tc.make()
			if err != nil {
				t.Fatalf("construct: %v", err)
			}

			ctx := context.Background()
			chunk := speechPCM(chunkSamples)

			// 2s of continuous speech, fed instantly. DefaultVADStartSecs is
			// 0.2s, so Speaking is due after ~2 chunks of audio time.
			for range 20 {
				if _, err := v.Analyze(ctx, chunk); err != nil {
					t.Fatalf("Analyze: %v", err)
				}
			}

			if got := v.State(); got != audio.VADStateSpeaking {
				t.Errorf("after 2s of continuous speech the VAD reports %v, want %v; "+
					"state transitions are timed by wall-clock, so an instant feed never advances them",
					got, audio.VADStateSpeaking)
			}
		})
	}
}

// TestVADReturnsToQuietByAudioTime is the mirror: the Speaking -> Stopping ->
// Quiet path must also advance on audio duration, or a VAD driven faster than
// real time latches in Speaking and never reports the end of a turn.
func TestVADReturnsToQuietByAudioTime(t *testing.T) {
	const sampleRate = 16000
	const chunkSamples = sampleRate / 10

	v, err := audio.NewSimpleVAD(audio.DefaultVADParams())
	if err != nil {
		t.Fatalf("NewSimpleVAD: %v", err)
	}

	ctx := context.Background()

	for range 20 { // 2s speech
		if _, err := v.Analyze(ctx, speechPCM(chunkSamples)); err != nil {
			t.Fatalf("Analyze speech: %v", err)
		}
	}
	// DefaultVADStopSecs is 0.8s; feed 2s of silence.
	for range 20 {
		if _, err := v.Analyze(ctx, make([]byte, chunkSamples*2)); err != nil {
			t.Fatalf("Analyze silence: %v", err)
		}
	}

	if got := v.State(); got != audio.VADStateQuiet {
		t.Errorf("after 2s of silence following speech the VAD reports %v, want %v; "+
			"the stop transition is timed by wall-clock", got, audio.VADStateQuiet)
	}
}
