package base_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBase struct {
	*base.Implementation
}

func newFakeBase(name string, t base.ProviderType) *fakeBase {
	return &fakeBase{Implementation: base.NewImplementation(name, t, nil)}
}

func TestBaseRegistry_RegisterAndGet(t *testing.T) {
	r := base.NewRegistry()
	p := newFakeBase("openai", base.ProviderTypeInference)
	require.NoError(t, r.Register(p))

	got, err := r.Get("openai", base.ProviderTypeInference)
	require.NoError(t, err)
	assert.Same(t, p, got)
}

func TestBaseRegistry_DuplicateRegistration_Errors(t *testing.T) {
	r := base.NewRegistry()
	require.NoError(t, r.Register(newFakeBase("openai", base.ProviderTypeInference)))
	err := r.Register(newFakeBase("openai", base.ProviderTypeInference))
	assert.Error(t, err)
}

func TestBaseRegistry_SameNameDifferentCapability_OK(t *testing.T) {
	r := base.NewRegistry()
	require.NoError(t, r.Register(newFakeBase("openai", base.ProviderTypeInference)))
	require.NoError(t, r.Register(newFakeBase("openai", base.ProviderTypeTTS)))

	inf, err := r.Get("openai", base.ProviderTypeInference)
	require.NoError(t, err)
	assert.Equal(t, base.ProviderTypeInference, inf.Type())

	tts, err := r.Get("openai", base.ProviderTypeTTS)
	require.NoError(t, err)
	assert.Equal(t, base.ProviderTypeTTS, tts.Type())
}

func TestBaseRegistry_GetMissing_Errors(t *testing.T) {
	r := base.NewRegistry()
	_, err := r.Get("nope", base.ProviderTypeInference)
	assert.Error(t, err)
}

func TestBaseRegistry_GetAll_FiltersByType(t *testing.T) {
	r := base.NewRegistry()
	require.NoError(t, r.Register(newFakeBase("a", base.ProviderTypeInference)))
	require.NoError(t, r.Register(newFakeBase("b", base.ProviderTypeInference)))
	require.NoError(t, r.Register(newFakeBase("c", base.ProviderTypeTTS)))

	inf := r.GetAll(base.ProviderTypeInference)
	assert.Len(t, inf, 2)
	tts := r.GetAll(base.ProviderTypeTTS)
	assert.Len(t, tts, 1)
}

func TestBaseRegistry_LifecycleHooks(t *testing.T) {
	r := base.NewRegistry()
	p := newFakeBase("p", base.ProviderTypeInference)
	require.NoError(t, r.Register(p))

	require.NoError(t, r.InitAll(context.Background()))
	require.NoError(t, r.CloseAll())
}

func TestBaseRegistry_PricingResolver_Default(t *testing.T) {
	r := base.NewRegistry()
	assert.NotNil(t, r.PricingResolver())
}

func TestBaseRegistry_PricingResolver_Override(t *testing.T) {
	r := base.NewRegistry()
	custom := base.NewInlinePricingResolver()
	r.SetPricingResolver(custom)
	assert.Same(t, custom, r.PricingResolver())
}

func TestBaseRegistry_Register_NilProvider_Errors(t *testing.T) {
	r := base.NewRegistry()
	err := r.Register(nil)
	assert.Error(t, err)
}

func TestBaseRegistry_GetTyped_Success(t *testing.T) {
	r := base.NewRegistry()
	p := newFakeBase("openai", base.ProviderTypeInference)
	require.NoError(t, r.Register(p))

	// fakeBase satisfies base.Provider, so GetTyped[base.Provider] should work.
	got, err := base.GetTyped[base.Provider](r, "openai", base.ProviderTypeInference)
	require.NoError(t, err)
	assert.Equal(t, "openai", got.Name())
}

func TestBaseRegistry_GetTyped_NotFound_Errors(t *testing.T) {
	r := base.NewRegistry()
	_, err := base.GetTyped[base.Provider](r, "missing", base.ProviderTypeInference)
	assert.Error(t, err)
}
