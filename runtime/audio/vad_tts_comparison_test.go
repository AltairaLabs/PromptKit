package audio_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// ResponseVADStage's params, mirrored from runtime/pipeline/stage/stages_speech.go.
// They differ substantially from the microphone defaults: a lower confidence
// bar and min volume, much faster start, faster stop, and a 24kHz rate.
func responseVADParams() audio.VADParams {
	p := audio.DefaultVADParams()
	p.Confidence = 0.3
	p.StartSecs = 0.05
	p.StopSecs = 0.3
	p.MinVolume = 0.005
	p.SampleRate = 24000
	return p
}

// TestResponseVADComparison reports SimpleVAD vs AdaptiveVAD under the params
// and signal characteristics of TTS OUTPUT detection, which is a different
// problem from microphone input.
//
// ResponseVADStage watches the assistant's own synthesized audio to confirm a
// response has finished. That signal is normalized and predictable — the failure
// modes that make a fixed threshold wrong for a microphone (quiet speakers,
// trailing syllables below the bar) largely do not arise. What matters here is
// reliable detection of playback and prompt release when it stops.
//
// Reports rather than asserts: this exists to decide whether the mic-side
// default change should extend to this stage, not to freeze behavior.
func TestResponseVADComparison(t *testing.T) {
	const chunkSamples = 2400 // 100 ms @ 24kHz

	// TTS output is normalized and sits high; include a quieter case for a
	// provider that renders at a lower level.
	levels := []struct {
		name string
		rms  float64
	}{
		{name: "loud TTS 0.20", rms: 0.20},
		{name: "normal TTS 0.10", rms: 0.10},
		{name: "quiet TTS 0.05", rms: 0.05},
		{name: "very quiet TTS 0.02", rms: 0.02},
	}

	t.Logf("DETECTION of TTS playback (chunks to Speaking, 100ms each)")
	t.Logf("%-24s | %-22s | %-22s", "level", "SimpleVAD", "AdaptiveVAD")
	for _, lv := range levels {
		chunks := repeatChunks(func() []byte { return pcmAtRMS(chunkSamples, lv.rms) }, 20)

		sv, err := audio.NewSimpleVAD(responseVADParams())
		if err != nil {
			t.Fatalf("NewSimpleVAD: %v", err)
		}
		av, err := audio.NewAdaptiveVAD(responseVADParams())
		if err != nil {
			t.Fatalf("NewAdaptiveVAD: %v", err)
		}

		s := runVAD(t, sv, chunks)
		a := runVAD(t, av, chunks)

		f := func(r vadRun) string {
			if !r.reachedSpeaking {
				return "NOT DETECTED"
			}
			return "detected @ " + itoa(r.chunkAtSpeaking*100) + "ms"
		}
		t.Logf("%-24s | %-22s | %-22s", lv.name, f(s), f(a))
	}

	t.Logf("")
	t.Logf("RELEASE after playback stops (playback 1s @0.10, then silence)")

	measureRelease := func(v audio.VADAnalyzer) string {
		ctx := context.Background()
		for range 10 {
			if _, err := v.Analyze(ctx, pcmAtRMS(chunkSamples, 0.10)); err != nil {
				t.Fatalf("Analyze: %v", err)
			}
		}
		silent := make([]byte, chunkSamples*2)
		for i := range 30 {
			if _, err := v.Analyze(ctx, silent); err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if v.State() == audio.VADStateQuiet {
				return "quiet @ " + itoa((i+1)*100) + "ms"
			}
		}
		return "never reached quiet"
	}

	sv, err := audio.NewSimpleVAD(responseVADParams())
	if err != nil {
		t.Fatalf("NewSimpleVAD: %v", err)
	}
	av, err := audio.NewAdaptiveVAD(responseVADParams())
	if err != nil {
		t.Fatalf("NewAdaptiveVAD: %v", err)
	}
	t.Logf("  SimpleVAD   %s", measureRelease(sv))
	t.Logf("  AdaptiveVAD %s", measureRelease(av))
}
