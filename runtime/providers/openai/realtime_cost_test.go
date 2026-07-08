package openai

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
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

// TestRealtimeCost_PricesCachedTokens is the regression test for finding H:
// the old bespoke calculateRealtimeCost had no cache handling at all, so
// InputTokenDetails.CachedTokens was silently dropped — a cache hit was
// billed as if it were full-price fresh text input. realtimeCostInfo must
// surface it as its own priced cache_read_token quantity.
func TestRealtimeCost_PricesCachedTokens(t *testing.T) {
	u := &UsageInfo{
		InputTokens: 1000, OutputTokens: 500,
	}
	u.InputTokenDetails.AudioTokens = 700
	u.InputTokenDetails.TextTokens = 300
	u.InputTokenDetails.CachedTokens = 200 // previously dropped (H)
	u.OutputTokenDetails.AudioTokens = 400
	u.OutputTokenDetails.TextTokens = 100

	ci := realtimeCostInfo(u, 0, 0)

	assert.Equal(t, 200.0, ci.Quantities[base.UnitCacheReadToken])
	assert.Equal(t, 700.0, ci.Quantities[base.UnitAudioInputToken])
	assert.Equal(t, 400.0, ci.Quantities[base.UnitAudioOutputToken])
	assert.InDelta(t, ci.TotalCost, ci.InputCostUSD+ci.OutputCostUSD+ci.CachedCostUSD, 1e-9)
}

// TestRealtimeCost_NilUsageReturnsNil verifies the nil-usage short-circuit.
func TestRealtimeCost_NilUsageReturnsNil(t *testing.T) {
	if realtimeCostInfo(nil, 0, 0) != nil {
		t.Fatal("expected nil cost for nil usage")
	}
}

// TestRealtimeUsageToTokens_MapsBreakdown verifies the per-modality mapping,
// the cache-is-a-subset-of-text clamp, and the "breakdown missing ⇒ treat
// all tokens as audio" fallback (over-reporting text at audio rates is
// preferable to silently under-billing a realtime session).
func TestRealtimeUsageToTokens_MapsBreakdown(t *testing.T) {
	tests := []struct {
		name   string
		usage  *UsageInfo
		want   base.TokenUsage
		cached int
	}{
		{
			name:  "full breakdown, no cache",
			usage: usageWith(1000, 500, 800, 200, 400, 100),
			want:  base.TokenUsage{Input: 200, AudioInput: 800, Output: 100, AudioOutput: 400},
		},
		{
			name:   "cache is a subset of text input",
			usage:  usageWith(1000, 500, 700, 300, 400, 100),
			cached: 200,
			want:   base.TokenUsage{Input: 100, CacheRead: 200, AudioInput: 700, Output: 100, AudioOutput: 400},
		},
		{
			name:   "cache clamped to text tokens if wire ever over-reports",
			usage:  usageWith(1000, 500, 700, 300, 400, 100),
			cached: 999,
			want:   base.TokenUsage{Input: 0, CacheRead: 300, AudioInput: 700, Output: 100, AudioOutput: 400},
		},
		{
			name:  "missing breakdown treats all tokens as audio",
			usage: usageWith(1000, 500, 0, 0, 0, 0),
			want:  base.TokenUsage{AudioInput: 1000, AudioOutput: 500},
		},
		{
			name:  "zero tokens is zero usage",
			usage: usageWith(0, 0, 0, 0, 0, 0),
			want:  base.TokenUsage{},
		},
		{
			name:  "text-only breakdown does not trigger the audio fallback",
			usage: usageWith(1000, 500, 0, 1000, 0, 500),
			want:  base.TokenUsage{Input: 1000, Output: 500},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.usage.InputTokenDetails.CachedTokens = tt.cached
			got := realtimeUsageToTokens(tt.usage)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRealtimeCost_DollarAmounts pins USD amounts across the audio/text/
// override/fallback matrix — the same scenarios the pre-unit-engine
// calculateRealtimeCost covered — so the migration to base.PriceUsage is
// verified to preserve them exactly (no cache tokens in play here; that is
// covered separately by TestRealtimeCost_PricesCachedTokens).
func TestRealtimeCost_DollarAmounts(t *testing.T) {
	tests := []struct {
		name        string
		usage       *UsageInfo
		inOverride  float64
		outOverride float64
		wantIn      float64
		wantOut     float64
	}{
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
			got := realtimeCostInfo(tt.usage, tt.inOverride, tt.outOverride)
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
		})
	}
}

// TestRealtimeCost_AudioVsTextMisbill documents the core money risk: the
// same token count costs ~8x more billed as audio than as text. The
// missing-breakdown fallback deliberately errs toward the audio (higher) side.
func TestRealtimeCost_AudioVsTextMisbill(t *testing.T) {
	asAudio := realtimeCostInfo(usageWith(1000, 0, 1000, 0, 0, 0), 0, 0)
	asText := realtimeCostInfo(usageWith(1000, 0, 0, 1000, 0, 0), 0, 0)

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
// forwards the session's per-1K overrides to realtimeCostInfo.
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
