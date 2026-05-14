//go:build cartesia_integration

package tts_test

import (
	"context"
	"io"
	"math"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// TestCartesiaLive_GenerationConfigEmotion is the regression guard for
// the silent-API-drift bug we hit when Cartesia retired
// voice.__experimental_controls.emotion on sonic-2+. Mocked tests can
// only assert our outbound request shape; this test confirms Cartesia
// actually accepts the shape we send.
//
// Skipped unless CARTESIA_API_KEY is set in the environment. Run with:
//
//	CARTESIA_API_KEY=... go test -tags=integration -run TestCartesiaLive \
//	    ./runtime/tts/...
//
// The cost per run is tiny (a few seconds of synthesis on sonic-3) and
// does not require any other provider — unlike running the full arena
// demo which also pays for OpenAI Realtime per pass.
func TestCartesiaLive_GenerationConfigEmotion(t *testing.T) {
	apiKey := os.Getenv("CARTESIA_API_KEY")
	if apiKey == "" {
		t.Skip("CARTESIA_API_KEY not set; skipping live Cartesia test")
	}

	// "Confident Man" voice, the one the voice-refund-demo uses. Pin so
	// regressions surface against the actual production voice rather than
	// some Cartesia default that might track API drift differently.
	const voiceID = "bf991597-6c13-47e4-8411-91ec2de5c466"
	const phrase = "I want my money back right now."

	svc := tts.NewCartesia(apiKey)

	cfg := tts.SynthesisConfig{
		Voice:  voiceID,
		Format: tts.FormatPCM16,
	}

	t.Run("plain text synthesises", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		audio := mustSynth(t, ctx, svc, phrase, cfg)
		if len(audio) < 4000 {
			t.Errorf("plain synthesis returned only %d bytes; expected at least 4000 (≈80ms PCM16@24k)", len(audio))
		}
	})

	t.Run("shout tag synthesises and Cartesia accepts generation_config", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		audio := mustSynth(t, ctx, svc, "[shouts]"+phrase+"[/]", cfg)
		if len(audio) < 4000 {
			t.Errorf("shouts synthesis returned only %d bytes; expected at least 4000", len(audio))
		}
	})

	t.Run("shout audio differs audibly from plain audio", func(t *testing.T) {
		// Sonic-3 is non-deterministic so byte-level inequality between
		// renders is not a useful signal — even two plain renders differ.
		// What we need is a signal that Cartesia is *acting on* our
		// generation_config field. RMS-energy difference is robust here:
		// silently-ignored controls produce statistically-identical RMS
		// across reruns (within a few %); honoured controls produce a
		// distinct envelope.
		//
		// Direction of the RMS change is not asserted because "angry" on
		// sonic-3 can render as tense/restrained rage with quieter
		// average energy than neutral speech — what matters is that the
		// envelope is measurably different.
		//
		// Audio files are written to /tmp/cartesia_*.wav-ish so we can
		// A/B them by ear when calibrating. Format is raw PCM16 mono
		// 24kHz; rename .pcm and import into Audacity if needed.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		plain := mustSynth(t, ctx, svc, phrase, cfg)
		shout := mustSynth(t, ctx, svc, "[shouts]"+phrase+"[/]", cfg)

		if err := os.WriteFile("/tmp/cartesia_plain.pcm", plain, 0o600); err != nil {
			t.Logf("could not write /tmp/cartesia_plain.pcm: %v", err)
		}
		if err := os.WriteFile("/tmp/cartesia_shout.pcm", shout, 0o600); err != nil {
			t.Logf("could not write /tmp/cartesia_shout.pcm: %v", err)
		}

		plainRMS := pcm16RMS(plain)
		shoutRMS := pcm16RMS(shout)
		plainPeak := pcm16Peak(plain)
		shoutPeak := pcm16Peak(shout)
		ratio := shoutRMS / plainRMS

		t.Logf("plain: RMS=%.2f peak=%d bytes=%d", plainRMS, plainPeak, len(plain))
		t.Logf("shout: RMS=%.2f peak=%d bytes=%d", shoutRMS, shoutPeak, len(shout))
		t.Logf("RMS ratio (shout/plain) = %.2f", ratio)
		t.Logf("Listen: ffplay -f s16le -ar 24000 /tmp/cartesia_shout.pcm")

		// Cartesia is silently ignoring the field iff the RMS envelopes
		// are statistically indistinguishable. 10% is a generous floor —
		// content-driven variance between two distinct renders sits well
		// inside that band.
		drift := math.Abs(1.0 - ratio)
		if drift < 0.1 {
			t.Errorf("shout RMS is within 10%% of plain RMS (ratio=%.2f). "+
				"Cartesia is likely silently ignoring generation_config; "+
				"check API surface against current docs.", ratio)
		}
	})
}

// mustSynth synthesises text via the live Cartesia API and returns the
// raw PCM bytes. Fails the test on synth error or empty result.
func mustSynth(t *testing.T, ctx context.Context, svc *tts.CartesiaService, text string, cfg tts.SynthesisConfig) []byte {
	t.Helper()
	r, err := svc.Synthesize(ctx, text, cfg)
	if err != nil {
		t.Fatalf("Synthesize(%q): %v", text, err)
	}
	defer r.Close()
	audio, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read synthesised audio: %v", err)
	}
	if len(audio) == 0 {
		t.Fatalf("synthesis returned zero bytes for %q", text)
	}
	return audio
}

// pcm16RMS and pcm16Peak moved to pcm_helpers_test.go so the
// elevenlabs_integration build tag can share them.

// Compile-time assertion that base.WithModel is reachable for follow-up
// tests that want to pin sonic-3 vs sonic-3.5 etc. Currently unused, but
// keeps the import warm so editors don't strip it on the next refactor.
var _ = base.WithModel
