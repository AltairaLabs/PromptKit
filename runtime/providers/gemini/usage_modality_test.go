package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

func TestUsageCounts_Tokens(t *testing.T) {
	cases := []struct {
		name string
		in   usageCounts
		want base.TokenUsage
	}{
		{
			name: "no breakdown prices everything as text",
			in:   usageCounts{prompt: 1000, cached: 200, response: 500, thoughts: 30},
			want: base.TokenUsage{Input: 800, CacheRead: 200, Output: 500, Reasoning: 30},
		},
		{
			name: "audio, image and video split out of input and output",
			in: usageCounts{
				prompt: 1000, response: 600,
				promptDetails: []ModalityTokenCount{
					{Modality: "TEXT", TokenCount: 100}, {Modality: "AUDIO", TokenCount: 600},
					{Modality: "IMAGE", TokenCount: 200}, {Modality: "VIDEO", TokenCount: 100},
				},
				output: []ModalityTokenCount{{Modality: "TEXT", TokenCount: 150}, {Modality: "AUDIO", TokenCount: 450}},
			},
			want: base.TokenUsage{
				Input: 100, AudioInput: 600, ImageInput: 200, VideoInput: 100, Output: 150, AudioOutput: 450,
			},
		},
		{
			name: "generated images split out of text output",
			in: usageCounts{
				prompt: 50, response: 1200, thoughts: 80,
				output: []ModalityTokenCount{{Modality: "TEXT", TokenCount: 80}, {Modality: "IMAGE", TokenCount: 1120}},
			},
			want: base.TokenUsage{Input: 50, Output: 80, ImageOutput: 1120, Reasoning: 80},
		},
		{
			name: "cached tokens leave their own modality, not text",
			in: usageCounts{
				prompt: 1000, cached: 400,
				promptDetails: []ModalityTokenCount{{Modality: "TEXT", TokenCount: 200}, {Modality: "AUDIO", TokenCount: 800}},
				cacheDetails:  []ModalityTokenCount{{Modality: "AUDIO", TokenCount: 400}},
			},
			want: base.TokenUsage{Input: 200, CacheRead: 400, AudioInput: 400},
		},
		{
			name: "tool-use prompt tokens bill as input",
			in: usageCounts{
				prompt: 100, toolUse: 50,
				toolUseDetails: []ModalityTokenCount{{Modality: "TEXT", TokenCount: 50}},
			},
			want: base.TokenUsage{Input: 150},
		},
		{
			name: "an inconsistent breakdown never produces a negative count",
			in: usageCounts{
				prompt: 100, response: 10,
				promptDetails: []ModalityTokenCount{{Modality: "AUDIO", TokenCount: 300}},
				output:        []ModalityTokenCount{{Modality: "AUDIO", TokenCount: 40}},
			},
			want: base.TokenUsage{AudioInput: 300, AudioOutput: 40},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.in.tokens())
		})
	}
}

// TestGeminiUsageToTokens_SplitsModalities checks the generateContent path
// reads the same breakdown the Live path does: audio input on 2.5 Flash costs
// more than three times its text rate.
func TestGeminiUsageToTokens_SplitsModalities(t *testing.T) {
	p := newTestGeminiProvider(t, "gemini-2.5-flash")
	ci := p.costFromUsage(geminiUsage{
		PromptTokenCount:     1_000_000,
		CandidatesTokenCount: 0,
		PromptTokensDetails:  []ModalityTokenCount{{Modality: "AUDIO", TokenCount: 1_000_000}},
	})
	assert.InDelta(t, 1.00, ci.InputCostUSD, 1e-9)
}
