package base_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rateOf(d *base.PricingDescriptor, unit string) float64 {
	for _, it := range d.Items {
		if it.Unit == unit {
			return it.Rate
		}
	}
	return -1
}

func TestResolveLLMPricing_Order(t *testing.T) {
	cfg := &base.PricingDescriptor{Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.009}}}
	flat := base.FlatPricing{Input: 0.001, Output: 0.002} // per-1K
	table := map[string]*base.PricingDescriptor{
		"claude-sonnet-4-5": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.003}}},
	}

	// explicit config wins
	assert.Equal(t, 0.009, base.ResolveLLMPricing(cfg, flat, table, "claude-sonnet-4-5").Items[0].Rate)

	// flat config maps to per-unit (0.002/1000 output) when no descriptor
	got := base.ResolveLLMPricing(nil, flat, table, "claude-sonnet-4-5")
	assert.InDelta(t, 0.001/1000, rateOf(got, base.UnitInputToken), 1e-12)
	assert.InDelta(t, 0.002/1000, rateOf(got, base.UnitOutputToken), 1e-12)
	// flat config also prices reasoning at the output rate, so a thinking model
	// under flat config does not silently understate cost by the reasoning amount.
	assert.InDelta(t, 0.002/1000, rateOf(got, base.UnitReasoningToken), 1e-12)
	// flat config is one input and one output rate for every modality, so a
	// provider that splits audio, image or video out still prices them.
	assert.InDelta(t, 0.001/1000, rateOf(got, base.UnitAudioInputToken), 1e-12)
	assert.InDelta(t, 0.001/1000, rateOf(got, base.UnitImageInputToken), 1e-12)
	assert.InDelta(t, 0.001/1000, rateOf(got, base.UnitVideoInputToken), 1e-12)
	assert.InDelta(t, 0.002/1000, rateOf(got, base.UnitAudioOutputToken), 1e-12)

	// no config: table by model
	got2 := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "claude-sonnet-4-5")
	assert.Equal(t, 0.003, rateOf(got2, base.UnitInputToken))

	// unknown model with a table: an empty descriptor, so PriceUsage warns
	unknown := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "made-up-model")
	require.NotNil(t, unknown)
	assert.Empty(t, unknown.Items)

	// no table and no config (a self-hosted server): nil, priced quietly at $0
	assert.Nil(t, base.ResolveLLMPricing(nil, base.FlatPricing{}, nil, "any-model"))
}

// TestResolveLLMPricing_UnknownModelWarns checks the property the empty
// descriptor exists for: a paid provider's new model is reported, not billed
// as a silent $0.
func TestResolveLLMPricing_UnknownModelWarns(t *testing.T) {
	base.ResetUnpricedWarnings() // once-per-process warning; keep -count>1 repeatable
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	defer logger.SetOutput(nil)

	table := map[string]*base.PricingDescriptor{
		"known-model": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.001}}},
	}
	desc := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "unknown-model-warn-test")
	ci := base.PriceUsage(desc, "unknown-model-warn-test-provider", base.ProviderTypeInference,
		base.TokenUsage{Input: 100}, nil, 0)

	assert.Zero(t, ci.TotalCost)
	assert.Contains(t, buf.String(), "nonzero token unit has no pricing")
}

func TestResolveLLMPricing_NormalizeModelStripsVendorPrefix(t *testing.T) {
	table := map[string]*base.PricingDescriptor{
		"claude-sonnet-4-5": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.003}}},
	}
	got := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "anthropic/claude-sonnet-4-5")
	assert.Equal(t, 0.003, rateOf(got, base.UnitInputToken))
}

// A dated snapshot id ("<alias>-YYYY-MM-DD", or Anthropic's "<alias>-YYYYMMDD")
// is a pinned version of the alias, so it prices like the alias when the table
// carries no row of its own. A row for the snapshot itself still wins.
func TestResolveLLMPricing_DatedSnapshotResolvesToAlias(t *testing.T) {
	table := map[string]*base.PricingDescriptor{
		"gpt-5.5":            {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.005}}},
		"gpt-4o":             {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.0025}}},
		"gpt-4o-2024-05-13":  {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.005}}},
		"claude-sonnet-4-5":  {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.003}}},
		"model-with-no-date": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.7}}},
	}
	cases := []struct {
		model string
		want  float64
	}{
		{"gpt-5.5-2026-04-23", 0.005},
		{"openai/gpt-5.5-2026-04-23", 0.005},
		{"claude-sonnet-4-5-20250929", 0.003},
		{"gpt-4o-2024-08-06", 0.0025},
		{"gpt-4o-2024-05-13", 0.005}, // the snapshot's own row beats the alias
	}
	for _, c := range cases {
		got := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, c.model)
		assert.Equalf(t, c.want, rateOf(got, base.UnitInputToken), "model %s", c.model)
	}
}

// Only a date is stripped. Any other suffix names a different model with its
// own price ("-pro" bills 6x the base model), so it must not borrow a sibling's
// row: it resolves to the empty descriptor and is reported as unpriced.
func TestResolveLLMPricing_NonDateSuffixIsNotAFamilyMatch(t *testing.T) {
	table := map[string]*base.PricingDescriptor{
		"gpt-5.5": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.005}}},
	}
	for _, model := range []string{
		"gpt-5.5-pro", "gpt-5.5-mini", "gpt-5.5-0613", "gpt-5.5-2026-04", "gpt-5.5-1999-01-01x", "gpt-5.5-2026-13-45", "gpt-5.5-20261345", "gpt-5.5x2026-04-23",
	} {
		got := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, model)
		require.NotNilf(t, got, "model %s", model)
		assert.Emptyf(t, got.Items, "model %s borrowed a sibling's price", model)
	}
}

// A paid provider's table missing the model is reported by name, once per
// model, so the operator knows which model needs a pricing: block — the
// per-unit warning alone does not say which model it was.
func TestResolveLLMPricing_UnknownModelWarnsOnceByName(t *testing.T) {
	base.ResetUnpricedWarnings()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	defer logger.SetOutput(nil)

	table := map[string]*base.PricingDescriptor{
		"known-model": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.001}}},
	}
	base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "vendor/brand-new-model-x1")
	base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "vendor/brand-new-model-x1")
	base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "known-model")

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "no built-in pricing for model"), out)
	assert.Contains(t, out, "brand-new-model-x1")
}

// A model the table knows, a config descriptor, or a table-less provider
// (self-hosted) is not "unknown", so none of them warn.
func TestResolveLLMPricing_KnownOrUntabledModelDoesNotWarn(t *testing.T) {
	base.ResetUnpricedWarnings()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	defer logger.SetOutput(nil)

	table := map[string]*base.PricingDescriptor{
		"known-model": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.001}}},
	}
	cfg := &base.PricingDescriptor{Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.009}}}
	base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "known-model-2025-01-01")
	base.ResolveLLMPricing(cfg, base.FlatPricing{}, table, "configured-unknown-model")
	base.ResolveLLMPricing(nil, base.FlatPricing{}, nil, "self-hosted-model")

	assert.NotContains(t, buf.String(), "no built-in pricing for model")
}

// A config descriptor that does not price cache_write_token bills written
// tokens at its own input rate, as configs did before providers reported cache
// writes separately; one that prices the unit keeps its rate. The caller's
// descriptor is not modified.
func TestResolveLLMPricing_ConfigWithoutCacheWriteBillsWritesAsInput(t *testing.T) {
	usage := base.TokenUsage{Input: 1000, CacheWrite: 4000, Output: 500}
	cfg := &base.PricingDescriptor{Items: []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: 2e-6},
		{Unit: base.UnitOutputToken, Rate: 8e-6},
	}}
	ci := base.PriceUsage(base.ResolveLLMPricing(cfg, base.FlatPricing{}, nil, "m"),
		"p", base.ProviderTypeInference, usage, nil, 0)
	assert.InDelta(t, 1000*2e-6+4000*2e-6+500*8e-6, ci.TotalCost, 1e-12)
	assert.InDelta(t, 5000*2e-6, ci.InputCostUSD, 1e-12)
	assert.Len(t, cfg.Items, 2, "the caller's descriptor was modified")

	own := &base.PricingDescriptor{Items: []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: 2e-6},
		{Unit: base.UnitCacheWriteToken, Rate: 2.5e-6},
		{Unit: base.UnitOutputToken, Rate: 8e-6},
	}}
	ci = base.PriceUsage(base.ResolveLLMPricing(own, base.FlatPricing{}, nil, "m"),
		"p", base.ProviderTypeInference, usage, nil, 0)
	assert.InDelta(t, 1000*2e-6+4000*2.5e-6+500*8e-6, ci.TotalCost, 1e-12)
}

// The fallback mirrors each input item with its dimensions, so a config tiered
// by dimension bills writes at the matching tier's input rate.
func TestResolveLLMPricing_ConfigCacheWriteFallbackKeepsDimensions(t *testing.T) {
	long := map[string]string{"prompt_size": "over_100k"}
	cfg := &base.PricingDescriptor{Items: []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: 1e-6},
		{Unit: base.UnitInputToken, Rate: 3e-6, Dimensions: long},
	}}
	desc := base.ResolveLLMPricing(cfg, base.FlatPricing{}, nil, "m")
	ci := base.PriceUsage(desc, "p", base.ProviderTypeInference, base.TokenUsage{CacheWrite: 1000}, long, 0)
	assert.InDelta(t, 1000*3e-6, ci.TotalCost, 1e-12)
	ci = base.PriceUsage(desc, "p", base.ProviderTypeInference, base.TokenUsage{CacheWrite: 1000}, nil, 0)
	assert.InDelta(t, 1000*1e-6, ci.TotalCost, 1e-12)
}

// A flat per-1K config has one input rate, which written tokens bill at too.
func TestResolveLLMPricing_FlatConfigBillsWritesAsInput(t *testing.T) {
	desc := base.ResolveLLMPricing(nil, base.FlatPricing{Input: 0.002, Output: 0.008}, nil, "m")
	ci := base.PriceUsage(desc, "p", base.ProviderTypeInference,
		base.TokenUsage{Input: 1000, CacheWrite: 4000, Output: 500}, nil, 0)
	assert.InDelta(t, 5000*0.002/1000+500*0.008/1000, ci.TotalCost, 1e-12)
}

// A config that prices cache_write_token keeps that rate in every tier: the
// input-rate fallback is not added beside it, where a tiered input item would
// otherwise produce a more specific write rate that wins.
func TestResolveLLMPricing_ConfigCacheWriteRateKeptInEveryTier(t *testing.T) {
	long := map[string]string{"prompt_size": "over_100k"}
	cfg := &base.PricingDescriptor{Items: []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: 1e-6},
		{Unit: base.UnitInputToken, Rate: 3e-6, Dimensions: long},
		{Unit: base.UnitCacheWriteToken, Rate: 2e-6},
	}}
	desc := base.ResolveLLMPricing(cfg, base.FlatPricing{}, nil, "m")
	ci := base.PriceUsage(desc, "p", base.ProviderTypeInference, base.TokenUsage{CacheWrite: 1000}, long, 0)
	assert.InDelta(t, 1000*2e-6, ci.TotalCost, 1e-12)
}
