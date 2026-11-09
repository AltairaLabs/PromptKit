package base_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
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

// lifecycleBase records Init/Close calls and can fail either.
type lifecycleBase struct {
	*base.Implementation
	inits, closes int
	initErr       error
	closeErr      error
}

func (l *lifecycleBase) Init(context.Context) error { l.inits++; return l.initErr }
func (l *lifecycleBase) Close() error              { l.closes++; return l.closeErr }

func newLifecycleBase(name string) *lifecycleBase {
	return &lifecycleBase{Implementation: base.NewImplementation(name, base.ProviderTypeInference, nil)}
}

func TestBaseRegistry_InitAllAndCloseAllReachEveryProvider(t *testing.T) {
	r := base.NewRegistry()
	a, b := newLifecycleBase("a"), newLifecycleBase("b")
	require.NoError(t, r.Register(a))
	require.NoError(t, r.Register(b))

	initErr := r.InitAll(context.Background())
	closeErr := r.CloseAll()

	assert.Equal(t, [2]error{nil, nil}, [2]error{initErr, closeErr})
	assert.Equal(t, 1, a.inits, "a initialized once")
	assert.Equal(t, 1, b.inits, "b initialized once")
	assert.Equal(t, 1, a.closes, "a closed once")
	assert.Equal(t, 1, b.closes, "b closed once")
}

func TestBaseRegistry_InitAllReturnsTheFailingProvider(t *testing.T) {
	r := base.NewRegistry()
	boom := errors.New("boom")
	p := newLifecycleBase("bad")
	p.initErr = boom
	require.NoError(t, r.Register(p))

	err := r.InitAll(context.Background())
	require.ErrorIs(t, err, boom)
	assert.Equal(t, `init "bad" (inference): boom`, err.Error())
}

// CloseAll keeps closing after a failure, so one bad provider cannot leak the
// others' connections.
func TestBaseRegistry_CloseAllClosesEveryProviderDespiteAnError(t *testing.T) {
	r := base.NewRegistry()
	boom := errors.New("boom")
	bad, good := newLifecycleBase("bad"), newLifecycleBase("good")
	bad.closeErr = boom
	require.NoError(t, r.Register(bad))
	require.NoError(t, r.Register(good))

	err := r.CloseAll()
	require.ErrorIs(t, err, boom)
	assert.Equal(t, `close "bad" (inference): boom`, err.Error())
	assert.Equal(t, 1, good.closes)
	assert.Equal(t, 1, bad.closes)
}

// The default resolver invents no prices: an unregistered model resolves to an
// error, not to a zero-cost descriptor.
func TestBaseRegistry_PricingResolver_DefaultInventsNoPrices(t *testing.T) {
	r := base.NewRegistry()
	ref := base.PricingRef{Impl: "openai", Model: "gpt-x", Capability: base.ProviderTypeInference}
	desc, err := r.PricingResolver().Resolve(context.Background(), ref)
	assert.Nil(t, desc)
	require.EqualError(t, err, "no inline pricing registered for impl=openai model=gpt-x capability=inference")
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

func TestBaseRegistry_ReplaceOverwritesDuplicate(t *testing.T) {
	r := base.NewRegistry()
	require.NoError(t, r.Register(newFakeBase("openai", base.ProviderTypeInference)))
	replacement := newFakeBase("openai", base.ProviderTypeInference)
	require.NoError(t, r.Replace(replacement))

	got, err := r.Get("openai", base.ProviderTypeInference)
	require.NoError(t, err)
	assert.Same(t, replacement, got)
	assert.Len(t, r.GetAll(base.ProviderTypeInference), 1)
	assert.Error(t, r.Replace(nil))
}

func TestBaseRegistry_Unregister(t *testing.T) {
	r := base.NewRegistry()
	require.NoError(t, r.Register(newFakeBase("openai", base.ProviderTypeInference)))
	require.NoError(t, r.Register(newFakeBase("openai", base.ProviderTypeTTS)))

	assert.True(t, r.Unregister("openai", base.ProviderTypeInference))
	assert.False(t, r.Unregister("openai", base.ProviderTypeInference), "already removed")
	_, err := r.Get("openai", base.ProviderTypeInference)
	assert.Error(t, err)
	_, err = r.Get("openai", base.ProviderTypeTTS)
	assert.NoError(t, err, "only the named capability is removed")
}
