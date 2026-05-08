package base_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPricingDescriptor_JSONRoundTrip(t *testing.T) {
	original := &base.PricingDescriptor{
		Source:           base.PricingSourceInline,
		PricingCorrectAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Currency:         "usd",
		Items: []base.PriceItem{
			{Unit: "input_token", Rate: 0.0000025},
			{Unit: "image", Rate: 0.04, Dimensions: map[string]string{"size": "1024x1024", "quality": "standard"}},
			{Unit: "image", Rate: 0.08, Dimensions: map[string]string{"size": "1024x1024", "quality": "hd"}},
		},
	}
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var got base.PricingDescriptor
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, *original, got)
}

func TestInlinePricingResolver_ReturnsRegistered(t *testing.T) {
	desc := &base.PricingDescriptor{
		Source:   base.PricingSourceInline,
		Currency: "usd",
		Items:    []base.PriceItem{{Unit: "input_token", Rate: 0.000003}},
	}
	resolver := base.NewInlinePricingResolver()
	resolver.Register(base.PricingRef{
		Impl: "openai", Model: "gpt-4o", Capability: base.ProviderTypeInference,
	}, desc)

	got, err := resolver.Resolve(context.Background(), base.PricingRef{
		Impl: "openai", Model: "gpt-4o", Capability: base.ProviderTypeInference,
	})
	require.NoError(t, err)
	assert.Same(t, desc, got)
}

func TestInlinePricingResolver_UnknownReturnsError(t *testing.T) {
	resolver := base.NewInlinePricingResolver()
	_, err := resolver.Resolve(context.Background(), base.PricingRef{
		Impl: "missing", Model: "x", Capability: base.ProviderTypeInference,
	})
	assert.Error(t, err)
}

func TestPriceItem_NormalizePer1MAlias(t *testing.T) {
	raw := []byte(`{"per_1m_input_token": 2.50}`)
	var item base.PriceItem
	require.NoError(t, json.Unmarshal(raw, &item))
	assert.Equal(t, "input_token", item.Unit)
	assert.InDelta(t, 0.0000025, item.Rate, 1e-12)
}
