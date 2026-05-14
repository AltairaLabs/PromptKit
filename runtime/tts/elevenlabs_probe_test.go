//go:build elevenlabs_integration

package tts_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// TestElevenLabsLive_ExpressiveTagsProbe is a calibration harness for
// ElevenLabs v3 inline tag rendering. The complaint is "totally
// unconvincing emotion on the anxious-recipient voice" — this test
// sweeps voice × tag combinations and dumps each rendering so we can
// pick the pair that actually carries the persona.
//
// Tagged separately so it doesn't run in the default suite. Invoke:
//
//	ELEVENLABS_API_KEY=... go test -tags=elevenlabs_integration \
//	    -run TestElevenLabsLive -count=1 -v ./runtime/tts/...
func TestElevenLabsLive_ExpressiveTagsProbe(t *testing.T) {
	apiKey := os.Getenv("ELEVENLABS_API_KEY")
	if apiKey == "" {
		t.Skip("ELEVENLABS_API_KEY not set; skipping live ElevenLabs probe")
	}

	const phrase = "I can't find the parcel anywhere, and the birthday is tomorrow."

	// Voices used by the demo (Bella) plus a few alternates with more
	// expressive range. Voice IDs are from runtime/tts/elevenlabs.go
	// SupportedVoices().
	voices := []struct {
		id    string
		label string
	}{
		{"ErXwobaYiN019PkySvjV", "Antoni_male"},
		{"TxGEqnHWrfWFTfGW9XjX", "Josh_male"},
		{"VR6AewLTigWG4xSOukaG", "Arnold_male"},
		{"pNInz6obpgDQGcFmaJgB", "Adam_male"},
		{"yoZ06aMxZJJ28mfd3POQ", "Sam_male"},
	}

	// Tag variants — plain first, then increasing expressive intensity.
	// All wrap the same charged phrase so audio differences isolate the
	// tag effect rather than the content.
	tagVariants := []struct {
		label string
		text  string
	}{
		{"plain", phrase},
		{"whispers", "[whispers]" + phrase + "[/]"},
		{"sighs", "[sighs]" + phrase + "[/]"},
		{"sad", "[sad]" + phrase + "[/]"},
		{"shouts", "[shouts]" + phrase + "[/]"},
	}

	type rendered struct {
		voice    string
		variant  string
		path     string
		bytes    int
		rms      float64
		peak     int
		duration float64
	}

	var results []rendered

	for _, v := range voices {
		svc := tts.NewElevenLabs(apiKey, base.WithModel("eleven_v3"))

		for _, tv := range tagVariants {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			r, err := svc.Synthesize(ctx, tv.text, tts.SynthesisConfig{
				Voice:  v.id,
				Format: tts.FormatPCM16,
			})
			if err != nil {
				cancel()
				t.Logf("Synthesize voice=%s variant=%s: %v", v.label, tv.label, err)
				continue
			}
			audio, err := io.ReadAll(r)
			_ = r.Close()
			cancel()
			if err != nil {
				t.Logf("read voice=%s variant=%s: %v", v.label, tv.label, err)
				continue
			}

			path := fmt.Sprintf("/tmp/eleven_%s_%s.pcm", v.label, tv.label)
			if werr := os.WriteFile(path, audio, 0o600); werr != nil {
				t.Logf("write %s: %v", path, werr)
			}

			results = append(results, rendered{
				voice:    v.label,
				variant:  tv.label,
				path:     path,
				bytes:    len(audio),
				rms:      pcm16RMS(audio),
				peak:     pcm16Peak(audio),
				duration: float64(len(audio)) / 2 / 24000, // assumes mono 24k PCM16
			})
		}
	}

	t.Log("=== ElevenLabs probe results ===")
	for _, r := range results {
		t.Logf("%-22s %-10s bytes=%-7d RMS=%-8.2f peak=%-5d dur=%-5.2fs  %s",
			r.voice, r.variant, r.bytes, r.rms, r.peak, r.duration, r.path)
	}
	t.Log(`Convert to WAV: for f in /tmp/eleven_*.pcm; do sox -r 24000 -e signed -b 16 -c 1 -t raw "$f" "${f}.wav"; done`)
}
