package statestore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestFoldAncillaryMetaCosts_TTS(t *testing.T) {
	total := &types.CostInfo{}
	meta := map[string]any{
		"tts_cost": map[string]any{
			"total_cost_usd": 0.00117,
			"capability":     "tts",
			"provider_name":  "openai",
			"quantities":     map[string]any{"character": float64(78)},
		},
	}

	foldAncillaryMetaCosts(total, meta)

	assert.InDelta(t, 0.00117, total.TotalCost, 1e-9)
	require.Len(t, total.Breakdown, 1)
	assert.Equal(t, "openai", total.Breakdown[0].Provider)
	assert.Equal(t, "tts", total.Breakdown[0].Capability)
	assert.Equal(t, "character", total.Breakdown[0].Unit)
	assert.Equal(t, float64(78), total.Breakdown[0].Quantity)
}

func TestFoldAncillaryMetaCosts_MultipleKeys(t *testing.T) {
	total := &types.CostInfo{}
	meta := map[string]any{
		"tts_cost": map[string]any{
			"total_cost_usd": 0.001,
			"quantities":     map[string]any{"character": float64(50)},
			"provider_name":  "openai",
			"capability":     "tts",
		},
		"self_play_cost": map[string]any{
			"total_cost_usd": 0.0005,
			"input_tokens":   100,
			"output_tokens":  50,
		},
	}

	foldAncillaryMetaCosts(total, meta)

	assert.InDelta(t, 0.0015, total.TotalCost, 1e-9)
	assert.Equal(t, 100, total.InputTokens)
	assert.Equal(t, 50, total.OutputTokens)
}

func TestFoldAncillaryMetaCosts_EmptyMeta(t *testing.T) {
	total := &types.CostInfo{TotalCost: 1.0}
	foldAncillaryMetaCosts(total, nil)
	assert.Equal(t, 1.0, total.TotalCost)

	foldAncillaryMetaCosts(total, map[string]any{})
	assert.Equal(t, 1.0, total.TotalCost)
}

func TestFoldAncillaryMetaCosts_NilTotal(t *testing.T) {
	// Should not panic
	foldAncillaryMetaCosts(nil, map[string]any{"tts_cost": map[string]any{}})
}

func TestFoldAncillaryMetaCosts_UnknownKeyIgnored(t *testing.T) {
	total := &types.CostInfo{}
	meta := map[string]any{
		"unknown_cost": map[string]any{"total_cost_usd": 1.0},
	}
	foldAncillaryMetaCosts(total, meta)
	assert.Equal(t, 0.0, total.TotalCost)
}

func TestFoldAncillaryMetaCosts_MalformedEntryIgnored(t *testing.T) {
	total := &types.CostInfo{}
	meta := map[string]any{
		"tts_cost": "not a map",
	}
	foldAncillaryMetaCosts(total, meta)
	assert.Equal(t, 0.0, total.TotalCost)
}

func TestCostInfoFromMeta_AllFields(t *testing.T) {
	m := map[string]any{
		"input_tokens":    float64(100),
		"output_tokens":   float64(50),
		"cached_tokens":   float64(10),
		"input_cost_usd":  0.001,
		"output_cost_usd": 0.002,
		"cached_cost_usd": 0.0001,
		"total_cost_usd":  0.0031,
		"provider_name":   "openai",
		"capability":      "tts",
		"quantities":      map[string]any{"character": float64(50)},
		"dimension_match": map[string]any{"voice": "alloy"},
	}

	c := costInfoFromMeta(m)

	assert.Equal(t, 100, c.InputTokens)
	assert.Equal(t, 50, c.OutputTokens)
	assert.Equal(t, 10, c.CachedTokens)
	assert.InDelta(t, 0.0031, c.TotalCost, 1e-9)
	assert.Equal(t, "openai", c.ProviderName)
	assert.Equal(t, "tts", c.Capability)
	assert.Equal(t, float64(50), c.Quantities["character"])
	assert.Equal(t, "alloy", c.DimensionMatch["voice"])
}

func TestMetaInt(t *testing.T) {
	assert.Equal(t, 5, metaInt(5))
	assert.Equal(t, 5, metaInt(int64(5)))
	assert.Equal(t, 5, metaInt(float64(5.0)))
	assert.Equal(t, 0, metaInt("not a number"))
	assert.Equal(t, 0, metaInt(nil))
}

func TestMetaFloat(t *testing.T) {
	assert.InDelta(t, 1.5, metaFloat(1.5), 1e-9)
	assert.InDelta(t, 1.5, metaFloat(float32(1.5)), 1e-6)
	assert.InDelta(t, 5.0, metaFloat(5), 1e-9)
	assert.InDelta(t, 5.0, metaFloat(int64(5)), 1e-9)
	assert.Equal(t, 0.0, metaFloat("not a number"))
	assert.Equal(t, 0.0, metaFloat(nil))
}
