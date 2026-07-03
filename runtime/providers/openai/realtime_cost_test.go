package openai

import (
	"math"
	"testing"
)

// usageWith builds a UsageInfo with the given totals and per-type breakdown.
func usageWith(inTok, outTok, inAudio, inText, outAudio, outText int) *UsageInfo {
	u := &UsageInfo{
		TotalTokens:  inTok + outTok,
		InputTokens:  inTok,
		OutputTokens: outTok,
	}
	u.InputTokenDetails.AudioTokens = inAudio
	u.InputTokenDetails.TextTokens = inText
	u.OutputTokenDetails.AudioTokens = outAudio
	u.OutputTokenDetails.TextTokens = outText
	return u
}

const costEpsilon = 1e-9

func approxEqual(a, b float64) bool { return math.Abs(a-b) < costEpsilon }

func TestCalculateRealtimeCost(t *testing.T) {
	tests := []struct {
		name        string
		usage       *UsageInfo
		inOverride  float64
		outOverride float64
		wantNil     bool
		wantIn      float64
		wantOut     float64
	}{
		{
			name:    "nil usage returns nil",
			usage:   nil,
			wantNil: true,
		},
		{
			name:  "with breakdown uses per-type default rates",
			usage: usageWith(1000, 500, 800, 200, 400, 100),
			// in:  800/1000*0.032 + 200/1000*0.004 = 0.0256 + 0.0008
			// out: 400/1000*0.064 + 100/1000*0.016 = 0.0256 + 0.0016
			wantIn:  0.0264,
			wantOut: 0.0272,
		},
		{
			name:        "audio rate overrides apply to audio tokens only",
			usage:       usageWith(1000, 500, 800, 200, 400, 100),
			inOverride:  0.1,
			outOverride: 0.2,
			// in:  800/1000*0.1 + 200/1000*0.004 = 0.08 + 0.0008
			// out: 400/1000*0.2 + 100/1000*0.016 = 0.08 + 0.0016
			wantIn:  0.0808,
			wantOut: 0.0816,
		},
		{
			name: "missing breakdown treats all tokens as audio (no-undercount fallback)",
			// Neither audio nor text token details set — the ~8x mis-bill guard
			// bills the full token count at AUDIO rates, not $0 or text rates.
			usage: usageWith(1000, 500, 0, 0, 0, 0),
			// in:  1000/1000*0.032 = 0.032
			// out: 500/1000*0.064  = 0.032
			wantIn:  0.032,
			wantOut: 0.032,
		},
		{
			name:  "zero tokens is zero cost",
			usage: usageWith(0, 0, 0, 0, 0, 0),
			// fallback sets inAudio=0, outAudio=0 → all terms zero.
			wantIn:  0,
			wantOut: 0,
		},
		{
			name:  "text-only breakdown does not trigger the audio fallback",
			usage: usageWith(1000, 500, 0, 1000, 0, 500),
			// inText present ⇒ fallback skipped; billed entirely at text rates.
			// in:  1000/1000*0.004 = 0.004
			// out: 500/1000*0.016  = 0.008
			wantIn:  0.004,
			wantOut: 0.008,
		},
		{
			name:  "override of zero falls back to default audio rate",
			usage: usageWith(1000, 0, 1000, 0, 0, 0),
			// inOverride 0 ⇒ default 0.032 audio-in rate.
			wantIn:  0.032,
			wantOut: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateRealtimeCost(tt.usage, tt.inOverride, tt.outOverride)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil cost, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil cost")
			}
			if !approxEqual(got.InputCostUSD, tt.wantIn) {
				t.Errorf("InputCostUSD = %v, want %v", got.InputCostUSD, tt.wantIn)
			}
			if !approxEqual(got.OutputCostUSD, tt.wantOut) {
				t.Errorf("OutputCostUSD = %v, want %v", got.OutputCostUSD, tt.wantOut)
			}
			if !approxEqual(got.TotalCost, tt.wantIn+tt.wantOut) {
				t.Errorf("TotalCost = %v, want %v", got.TotalCost, tt.wantIn+tt.wantOut)
			}
			if got.InputTokens != tt.usage.InputTokens {
				t.Errorf("InputTokens = %d, want %d", got.InputTokens, tt.usage.InputTokens)
			}
			if got.OutputTokens != tt.usage.OutputTokens {
				t.Errorf("OutputTokens = %d, want %d", got.OutputTokens, tt.usage.OutputTokens)
			}
		})
	}
}

// TestCalculateRealtimeCost_AudioVsTextMisbill documents the core money risk:
// the same token count costs ~8x more billed as audio than as text. The
// missing-breakdown fallback deliberately errs toward the audio (higher) side.
func TestCalculateRealtimeCost_AudioVsTextMisbill(t *testing.T) {
	asAudio := calculateRealtimeCost(usageWith(1000, 0, 1000, 0, 0, 0), 0, 0)
	asText := calculateRealtimeCost(usageWith(1000, 0, 0, 1000, 0, 0), 0, 0)

	if asAudio.InputCostUSD <= asText.InputCostUSD {
		t.Fatalf("audio billing (%v) should exceed text billing (%v)",
			asAudio.InputCostUSD, asText.InputCostUSD)
	}
	ratio := asAudio.InputCostUSD / asText.InputCostUSD
	if !approxEqual(ratio, defaultAudioInputCostPer1K/defaultTextInputCostPer1K) {
		t.Errorf("audio/text ratio = %v, want %v", ratio,
			defaultAudioInputCostPer1K/defaultTextInputCostPer1K)
	}
}

// TestRealtimeSession_calculateCost_DelegatesOverrides verifies the thin method
// forwards the session's per-1K overrides to the pure function.
func TestRealtimeSession_calculateCost_DelegatesOverrides(t *testing.T) {
	s := &RealtimeSession{inputCostPer1K: 0.1, outputCostPer1K: 0.2}
	got := s.calculateCost(usageWith(1000, 500, 800, 200, 400, 100))
	if got == nil {
		t.Fatal("expected non-nil cost")
	}
	if !approxEqual(got.InputCostUSD, 0.0808) {
		t.Errorf("InputCostUSD = %v, want 0.0808 (overrides applied)", got.InputCostUSD)
	}
	if !approxEqual(got.OutputCostUSD, 0.0816) {
		t.Errorf("OutputCostUSD = %v, want 0.0816 (overrides applied)", got.OutputCostUSD)
	}

	if s.calculateCost(nil) != nil {
		t.Error("calculateCost(nil) should return nil")
	}
}
