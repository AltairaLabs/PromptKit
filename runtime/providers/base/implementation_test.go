package base_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
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
}
