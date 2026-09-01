package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Gemini 3 replaced thinkingBudget with thinkingLevel. Sending the superseded
// field to a Gemini 3 model is accepted but does not reliably produce thought
// summaries: measured live on gemini-3.7-flash streaming, thinkingBudget
// yielded thought parts in 0 of 6 runs while thinkingLevel:"high" yielded them
// in 6 of 6. Gemini 2.5 is the mirror image — it rejects thinkingLevel with
// HTTP 400 — so the two cannot both be sent.

func TestApplyThinkingConfig_ReadsThinkingLevel(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"low", "low", "low"},
		{"medium", "medium", "medium"},
		{"high", "high", "high"},
		{"uppercase is normalized", "HIGH", "high"},
		{"unknown level ignored", "minimal", ""}, // rejected by the API with 400
		{"non-string ignored", 3, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Provider{}
			applyThinkingConfig(p, providers.ProviderSpec{
				AdditionalConfig: map[string]any{"thinking_level": tc.value},
			})
			if tc.want == "" {
				assert.Nil(t, p.thinkingLevel, "unrecognized level must be ignored, not sent")
				return
			}
			require.NotNil(t, p.thinkingLevel)
			assert.Equal(t, tc.want, *p.thinkingLevel)
		})
	}
}

// TestThinkingConfigFor_LevelWinsAndBudgetOmitted pins the mutual exclusion.
// Sending both is not merely redundant: Gemini 2.5 rejects thinkingLevel
// outright, so a config carrying both must resolve to exactly one field.
func TestThinkingConfigFor_LevelWinsAndBudgetOmitted(t *testing.T) {
	p := &Provider{}
	applyThinkingConfig(p, providers.ProviderSpec{
		AdditionalConfig: map[string]any{
			"thinking_level":   "high",
			"thinking_budget":  2048,
			"include_thoughts": true,
		},
	})

	cfg := p.geminiThinkingConfigFor(4096)
	require.NotNil(t, cfg)

	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	assert.Contains(t, string(raw), `"thinkingLevel":"high"`)
	assert.NotContains(t, string(raw), "thinkingBudget",
		"thinkingBudget must be omitted when a level is set; Gemini 3 ignores it and "+
			"sending both is ambiguous")
	assert.Contains(t, string(raw), `"includeThoughts":true`)
}

// TestThinkingConfigFor_BudgetStillWorksAlone keeps the Gemini 2.5 path intact:
// with no level configured the budget is sent exactly as before.
func TestThinkingConfigFor_BudgetStillWorksAlone(t *testing.T) {
	p := &Provider{}
	applyThinkingConfig(p, providers.ProviderSpec{
		AdditionalConfig: map[string]any{"thinking_budget": 2048, "include_thoughts": true},
	})

	cfg := p.geminiThinkingConfigFor(4096)
	require.NotNil(t, cfg)

	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"thinkingBudget":2048`)
	assert.NotContains(t, string(raw), "thinkingLevel",
		"thinkingLevel must be omitted for Gemini 2.5, which rejects it with HTTP 400")
}

// TestThinkingConfig_WireShape pins what actually reaches Gemini for each
// configuration, rather than the shape of an in-memory pointer.
//
// This is the assertion that matters: the two controls are generation-specific
// and mutually exclusive, and sending the wrong one is silently useless
// (Gemini 3 ignores a budget for summaries) or fatal (Gemini 2.5 rejects a
// level with HTTP 400).
func TestThinkingConfig_WireShape(t *testing.T) {
	cases := []struct {
		name        string
		cfg         map[string]any
		wantPresent []string
		wantAbsent  []string
	}{
		{
			name:        "gemini 3 style: level only",
			cfg:         map[string]any{"thinking_level": "high", "include_thoughts": true},
			wantPresent: []string{`"thinkingLevel":"high"`, `"includeThoughts":true`},
			wantAbsent:  []string{"thinkingBudget"},
		},
		{
			name:        "gemini 2.5 style: budget only",
			cfg:         map[string]any{"thinking_budget": 2048, "include_thoughts": true},
			wantPresent: []string{`"thinkingBudget":2048`, `"includeThoughts":true`},
			wantAbsent:  []string{"thinkingLevel"},
		},
		{
			name:        "both set: level wins, budget dropped",
			cfg:         map[string]any{"thinking_level": "low", "thinking_budget": 2048},
			wantPresent: []string{`"thinkingLevel":"low"`},
			wantAbsent:  []string{"thinkingBudget"},
		},
		{
			name:       "nothing configured: no thinkingConfig at all",
			cfg:        map[string]any{},
			wantAbsent: []string{"thinkingConfig", "thinkingLevel", "thinkingBudget"},
		},
		{
			name:       "unrecognized level is dropped, not forwarded",
			cfg:        map[string]any{"thinking_level": "minimal"},
			wantAbsent: []string{"thinkingLevel", "thinkingConfig"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Provider{}
			applyThinkingConfig(p, providers.ProviderSpec{AdditionalConfig: tc.cfg})

			// Marshal the generationConfig the provider would send, so the
			// assertion is about the wire and not an internal field.
			body := geminiGenConfig{
				MaxOutputTokens: 4096,
				ThinkingConfig:  p.geminiThinkingConfigFor(4096),
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)

			for _, want := range tc.wantPresent {
				assert.Containsf(t, string(raw), want, "generationConfig=%s", raw)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContainsf(t, string(raw), absent, "generationConfig=%s", raw)
			}
		})
	}
}
