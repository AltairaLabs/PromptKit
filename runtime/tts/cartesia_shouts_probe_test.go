//go:build cartesia_integration

package tts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestCartesiaShouts_ProbeVariants sweeps Cartesia's sonic-3 emotion
// vocabulary + volume range against the same phrase and writes each
// rendering to /tmp/cartesia_probe_<label>.pcm for A/B listening.
//
// Cartesia's primary set is documented as
// (neutral, calm, angry, content, sad, scared) with 60+ additional
// names supported — but the docs don't enumerate them. This test
// brute-forces the rage-adjacent candidates so we can pick the value
// that actually shouts on the Confident-Man voice and bake it into
// cartesiaEmotionForTag.
//
// Runs raw HTTP against Cartesia (bypassing our public API) so we can
// probe values that have no markup-tag mapping yet. Tag-gated:
//
//	CARTESIA_API_KEY=... go test -tags=cartesia_integration \
//	    -run TestCartesiaShouts_ProbeVariants -count=1 -v ./runtime/tts/...
func TestCartesiaShouts_ProbeVariants(t *testing.T) {
	apiKey := os.Getenv("CARTESIA_API_KEY")
	if apiKey == "" {
		t.Skip("CARTESIA_API_KEY not set; skipping live Cartesia probe")
	}

	const voiceID = "bf991597-6c13-47e4-8411-91ec2de5c466" // Confident Man
	const phrase = "Thirteen MONTHS and they fail?! I want my money back NOW!"

	variants := []struct {
		label   string
		emotion string  // empty = no generation_config
		volume  float64 // 0 = no volume override
	}{
		{"plain", "", 0},
		{"angry_v10", "angry", 1.0},
		{"angry_v16", "angry", 1.6},
		{"angry_v20", "angry", 2.0},
		{"enraged_v20", "enraged", 2.0},
		{"furious_v20", "furious", 2.0},
		{"yelling_v20", "yelling", 2.0},
		{"shouting_v20", "shouting", 2.0},
		{"aggressive_v20", "aggressive", 2.0},
		{"hostile_v20", "hostile", 2.0},
		{"angry_v20_speed_fast", "angry", 2.0},
	}

	type rendered struct {
		label string
		path  string
		bytes int
		rms   float64
		peak  int
		http  int
	}

	var results []rendered
	client := &http.Client{Timeout: 30 * time.Second}

	for _, v := range variants {
		body := map[string]interface{}{
			"model_id":   "sonic-3",
			"transcript": phrase,
			"voice":      map[string]string{"mode": "id", "id": voiceID},
			"language":   "en",
			"output_format": map[string]interface{}{
				"container":   "raw",
				"encoding":    "pcm_s16le",
				"sample_rate": 24000,
			},
		}

		if v.emotion != "" || v.volume != 0 {
			gen := map[string]interface{}{}
			if v.emotion != "" {
				gen["emotion"] = v.emotion
			}
			if v.volume != 0 {
				gen["volume"] = v.volume
			}
			if v.label == "angry_v20_speed_fast" {
				gen["speed"] = 1.3
			}
			body["generation_config"] = gen
		}

		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s: %v", v.label, err)
		}

		req, err := http.NewRequestWithContext(context.Background(),
			http.MethodPost,
			"https://api.cartesia.ai/tts/bytes",
			bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("NewRequest %s: %v", v.label, err)
		}
		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cartesia-Version", "2026-03-01")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do %s: %v", v.label, err)
		}
		audio, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", v.label, err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Logf("%-22s HTTP %d body=%s", v.label, resp.StatusCode, string(audio))
			results = append(results, rendered{label: v.label, http: resp.StatusCode, bytes: len(audio)})
			continue
		}

		path := fmt.Sprintf("/tmp/cartesia_probe_%s.pcm", v.label)
		if werr := os.WriteFile(path, audio, 0o600); werr != nil {
			t.Logf("write %s: %v", path, werr)
		}

		results = append(results, rendered{
			label: v.label,
			path:  path,
			bytes: len(audio),
			rms:   pcm16RMS(audio),
			peak:  pcm16Peak(audio),
			http:  resp.StatusCode,
		})
	}

	t.Log("=== probe results ===")
	for _, r := range results {
		if r.http != http.StatusOK {
			t.Logf("%-22s HTTP=%d  (rejected)", r.label, r.http)
			continue
		}
		t.Logf("%-22s HTTP=%d bytes=%-7d RMS=%-8.2f peak=%-5d  file=%s",
			r.label, r.http, r.bytes, r.rms, r.peak, r.path)
	}
	t.Log(`Convert to WAV with: for f in /tmp/cartesia_probe_*.pcm; do sox -r 24000 -e signed -b 16 -c 1 -t raw "$f" "${f}.wav"; done`)
}
