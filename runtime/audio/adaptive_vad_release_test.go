package audio_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// feedSpeechThenTail runs 1s of speech at loudRMS, then tailChunks of tailRMS,
// and reports the chunk index (1-based, 100ms each) at which the VAD stopped
// reporting Speaking, or -1 if it never did.
func feedSpeechThenTail(t *testing.T, v audio.VADAnalyzer, loudRMS, tailRMS float64, tailChunks int) int {
	t.Helper()
	ctx := context.Background()

	for range 10 {
		if _, err := v.Analyze(ctx, pcmAtRMS(cmpChunkSamples, loudRMS)); err != nil {
			t.Fatalf("Analyze speech: %v", err)
		}
	}
	if v.State() != audio.VADStateSpeaking {
		t.Fatalf("VAD did not reach Speaking on 1s of %.2f RMS speech (got %v)", loudRMS, v.State())
	}

	for i := range tailChunks {
		var chunk []byte
		if tailRMS == 0 {
			chunk = make([]byte, cmpChunkSamples*2)
		} else {
			chunk = pcmAtRMS(cmpChunkSamples, tailRMS)
		}
		if _, err := v.Analyze(ctx, chunk); err != nil {
			t.Fatalf("Analyze tail: %v", err)
		}
		if v.State() != audio.VADStateSpeaking {
			return i + 1
		}
	}
	return -1
}

// TestAdaptiveVADReleasesPromptlyOnSilence covers release lag.
//
// AudioTurnStage counts Stopping and Quiet as silence, so its SilenceDuration
// budget only begins accruing once the VAD leaves Speaking. Every millisecond
// spent lingering is added to the end of every turn — latency the caller feels
// before a response starts, and, when it exceeds the gap between two
// utterances, the reason they merge into a single turn instead of segmenting.
//
// Measured at 600ms against SimpleVAD's 200ms, caused by a symmetric smoothing
// factor: the level estimate decays slowly from a high value regardless of how
// abruptly the audio stops.
func TestAdaptiveVADReleasesPromptlyOnSilence(t *testing.T) {
	v, err := audio.NewAdaptiveVAD(audio.DefaultVADParams())
	if err != nil {
		t.Fatalf("NewAdaptiveVAD: %v", err)
	}

	left := feedSpeechThenTail(t, v, 0.10, 0, 30)
	if left < 0 {
		t.Fatalf("VAD never left Speaking across 3s of true silence")
	}

	const maxChunks = 3 // 300ms
	if left > maxChunks {
		t.Errorf("VAD held Speaking for %dms after speech stopped, want <=%dms; "+
			"the lag is added to every turn boundary and merges utterances "+
			"separated by shorter pauses", left*100, maxChunks*100)
	}
}

// TestAdaptiveVADHoldsThroughQuietTail guards the property a faster release must
// not cost us.
//
// A trailing word measured at 0.021 RMS is real speech, not the end of a turn.
// Releasing on it closes the turn mid-utterance and orphans the word — the
// original "742 Evergreen Terrace, Springfield" defect. The detector must
// distinguish quiet speech from actual silence rather than simply reacting
// faster to everything.
func TestAdaptiveVADHoldsThroughQuietTail(t *testing.T) {
	v, err := audio.NewAdaptiveVAD(audio.DefaultVADParams())
	if err != nil {
		t.Fatalf("NewAdaptiveVAD: %v", err)
	}

	if left := feedSpeechThenTail(t, v, 0.10, 0.021, 10); left > 0 {
		t.Errorf("VAD left Speaking %dms into a 0.021 RMS speech tail; "+
			"quiet trailing speech must not read as end-of-turn", left*100)
	}
}
