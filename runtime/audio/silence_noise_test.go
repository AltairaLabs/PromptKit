package audio_test

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// TestSilenceDetectorDoesNotCompleteTurnOnUnconfirmedSpeech covers a single
// noise chunk being enough to fire a turn.
//
// Reaching VADStateStarting takes only one chunk above the threshold — a cough,
// a door slam, a knock on the desk. Turn completion is gated on hadSpeech, so if
// Starting arms that flag then a blip followed by silence fires turnCallback and
// ships noise-only audio to STT as if it were an utterance.
//
// Starting must count as speaking for IsUserSpeaking (a turn detector that
// reports not-speaking during the Starting window ends turns mid-word), but it
// must not by itself arm turn completion. Only confirmed speech should do that.
func TestSilenceDetectorDoesNotCompleteTurnOnUnconfirmedSpeech(t *testing.T) {
	ctx := context.Background()
	d := audio.NewSilenceDetector(10 * time.Millisecond)

	// A blip: one chunk crosses the threshold, never confirmed as speech.
	if _, err := d.ProcessVADState(ctx, audio.VADStateStarting); err != nil {
		t.Fatalf("ProcessVADState(Starting): %v", err)
	}
	// It falls away again.
	if _, err := d.ProcessVADState(ctx, audio.VADStateQuiet); err != nil {
		t.Fatalf("ProcessVADState(Quiet): %v", err)
	}

	time.Sleep(20 * time.Millisecond) // let the silence threshold elapse

	done, err := d.ProcessVADState(ctx, audio.VADStateQuiet)
	if err != nil {
		t.Fatalf("ProcessVADState(Quiet): %v", err)
	}
	if done {
		t.Error("a single unconfirmed noise chunk completed a turn; " +
			"Starting must not arm turn completion on its own")
	}
}

// TestSilenceDetectorStillCompletesTurnAfterConfirmedSpeech is the counterpart:
// narrowing what arms turn completion must not stop real speech from ending a
// turn.
func TestSilenceDetectorStillCompletesTurnAfterConfirmedSpeech(t *testing.T) {
	ctx := context.Background()
	d := audio.NewSilenceDetector(10 * time.Millisecond)

	for _, st := range []audio.VADState{audio.VADStateStarting, audio.VADStateSpeaking} {
		if _, err := d.ProcessVADState(ctx, st); err != nil {
			t.Fatalf("ProcessVADState(%v): %v", st, err)
		}
	}
	if _, err := d.ProcessVADState(ctx, audio.VADStateStopping); err != nil {
		t.Fatalf("ProcessVADState(Stopping): %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	done, err := d.ProcessVADState(ctx, audio.VADStateQuiet)
	if err != nil {
		t.Fatalf("ProcessVADState(Quiet): %v", err)
	}
	if !done {
		t.Error("confirmed speech followed by silence did not complete the turn")
	}
}

// TestSilenceDetectorReportsSpeakingDuringStarting pins the half of the Starting
// change that must be kept: IsUserSpeaking has to be true during the Starting
// window, or a turn detector reports not-speaking over audio the pipeline has
// already accepted as speech and cuts the turn mid-word.
func TestSilenceDetectorReportsSpeakingDuringStarting(t *testing.T) {
	ctx := context.Background()
	d := audio.NewSilenceDetector(800 * time.Millisecond)

	if _, err := d.ProcessVADState(ctx, audio.VADStateStarting); err != nil {
		t.Fatalf("ProcessVADState(Starting): %v", err)
	}

	if !d.IsUserSpeaking() {
		t.Error("detector reports not-speaking during the VAD Starting window; " +
			"the turn detector's vote would end the turn mid-utterance")
	}
}
