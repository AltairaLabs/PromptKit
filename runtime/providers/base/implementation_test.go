package base_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/stretchr/testify/assert"
)

type composer struct {
	*base.Implementation
}

func TestImplementation_DefaultsCompile(t *testing.T) {
	impl := base.NewImplementation("test-provider", base.ProviderTypeInference, nil)
	c := &composer{Implementation: impl}

	var _ base.Provider = c // compile-time check

	assert.Equal(t, "test-provider", c.Name())
	assert.Equal(t, base.ProviderTypeInference, c.Type())
	assert.Nil(t, c.Pricing())
	assert.NoError(t, c.Validate())
	assert.NoError(t, c.Init(context.Background()))
	assert.NoError(t, c.HealthCheck(context.Background()))
	assert.NoError(t, c.Close())
}

func TestImplementation_PricingSet(t *testing.T) {
	desc := &base.PricingDescriptor{Currency: "usd"}
	impl := base.NewImplementation("p", base.ProviderTypeImage, desc)
	assert.Same(t, desc, impl.Pricing())

	later := &base.PricingDescriptor{Currency: "eur"}
	impl.SetPricing(later)
	assert.Same(t, later, impl.Pricing())
}

func TestImplementation_MessageMetadataTemplate(t *testing.T) {
	impl := base.NewImplementation("p", base.ProviderTypeInference, nil)
	assert.Empty(t, impl.MessageMetadataTemplate(), "no template until one is set")

	impl.SetMessageMetadataTemplate("<context>{{selection}}</context>")
	assert.Equal(t, "<context>{{selection}}</context>", impl.MessageMetadataTemplate())
}

// A provider built as a raw literal has a nil *Implementation; reading its
// settings must degrade to "none" rather than panic.
func TestImplementation_NilReceiver(t *testing.T) {
	var impl *base.Implementation
	assert.Nil(t, impl.Pricing())
	assert.Empty(t, impl.MessageMetadataTemplate())
}
