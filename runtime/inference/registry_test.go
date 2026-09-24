package inference_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider is a minimal inference.Provider that records the id it was
// constructed with, so a test can confirm the registry returned the actual
// registered instance rather than a copy or the wrong entry.
type stubProvider struct{ id string }

func (s *stubProvider) Infer(_ context.Context, _ inference.Request) (inference.Response, error) {
	return inference.Response{Model: s.id}, nil
}

func TestRegistry_FirstRegistrationBecomesDefault(t *testing.T) {
	r := inference.NewRegistry()
	a := &stubProvider{id: "a"}
	b := &stubProvider{id: "b"}
	require.NoError(t, r.Register("a", a))
	require.NoError(t, r.Register("b", b))

	got, err := r.Get("")
	require.NoError(t, err)
	assert.Same(t, a, got)
}

func TestRegistry_GetByID(t *testing.T) {
	r := inference.NewRegistry()
	a := &stubProvider{id: "a"}
	b := &stubProvider{id: "b"}
	require.NoError(t, r.Register("a", a))
	require.NoError(t, r.Register("b", b))

	got, err := r.Get("b")
	require.NoError(t, err)
	assert.Same(t, b, got)
}

func TestRegistry_GetMissing_ErrorsNamingID(t *testing.T) {
	r := inference.NewRegistry()
	require.NoError(t, r.Register("a", &stubProvider{id: "a"}))

	_, err := r.Get("missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestRegistry_DuplicateRegister_Errors(t *testing.T) {
	r := inference.NewRegistry()
	require.NoError(t, r.Register("a", &stubProvider{id: "a"}))

	err := r.Register("a", &stubProvider{id: "a-2"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a")
}

func TestRegistry_SetDefault(t *testing.T) {
	r := inference.NewRegistry()
	a := &stubProvider{id: "a"}
	b := &stubProvider{id: "b"}
	require.NoError(t, r.Register("a", a))
	require.NoError(t, r.Register("b", b))

	require.NoError(t, r.SetDefault("b"))
	got, err := r.Get("")
	require.NoError(t, err)
	assert.Same(t, b, got)
}

func TestRegistry_SetDefault_Missing_Errors(t *testing.T) {
	r := inference.NewRegistry()
	require.NoError(t, r.Register("a", &stubProvider{id: "a"}))

	err := r.SetDefault("missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestRegistry_IDs(t *testing.T) {
	r := inference.NewRegistry()
	require.NoError(t, r.Register("b", &stubProvider{id: "b"}))
	require.NoError(t, r.Register("a", &stubProvider{id: "a"}))

	assert.ElementsMatch(t, []string{"a", "b"}, r.IDs())
}

// TestFromContext_NoRegistryAttached covers both a nil ctx and a ctx that
// was never passed through WithRegistry: neither carries a Registry, so both
// must come back nil. A FromContext that panicked on nil, or that returned a
// non-nil zero-value Registry instead of nil, would fail this the same way
// t.Error/t.Fatal would — using native assertions here (rather than
// testify's Nil/NotNil) so the check isn't a no-op against a broken
// implementation.
func TestFromContext_NoRegistryAttached(t *testing.T) {
	if got := inference.FromContext(nil); got != nil {
		t.Errorf("FromContext(nil) = %v, want nil", got)
	}
	if got := inference.FromContext(context.Background()); got != nil {
		t.Errorf("FromContext(context with no registry) = %v, want nil", got)
	}
}

func TestWithRegistry_RoundTrip(t *testing.T) {
	r := inference.NewRegistry()
	ctx := inference.WithRegistry(context.Background(), r)

	assert.Same(t, r, inference.FromContext(ctx))
}
