package inference_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterFactory_CreateFromSpec(t *testing.T) {
	inference.RegisterFactory("factory-test-t", func(spec inference.ProviderSpec) (inference.Provider, error) {
		return &stubProvider{id: spec.ID}, nil
	})

	got, err := inference.CreateFromSpec(inference.ProviderSpec{ID: "instance-1", Type: "factory-test-t"})
	require.NoError(t, err)
	assert.Equal(t, &stubProvider{id: "instance-1"}, got)
}

func TestCreateFromSpec_UnknownType_NamesType(t *testing.T) {
	_, err := inference.CreateFromSpec(inference.ProviderSpec{Type: "factory-test-unknown-type"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory-test-unknown-type")
}

func TestRegisteredTypes_Sorted(t *testing.T) {
	inference.RegisterFactory("factory-test-zeta", func(_ inference.ProviderSpec) (inference.Provider, error) {
		return &stubProvider{}, nil
	})
	inference.RegisterFactory("factory-test-alpha", func(_ inference.ProviderSpec) (inference.Provider, error) {
		return &stubProvider{}, nil
	})

	types := inference.RegisteredTypes()
	require.Contains(t, types, "factory-test-zeta")
	require.Contains(t, types, "factory-test-alpha")
	assert.True(t, isSorted(types), "RegisteredTypes must return a sorted slice, got %v", types)
}

func isSorted(ss []string) bool {
	for i := 1; i < len(ss); i++ {
		if ss[i-1] > ss[i] {
			return false
		}
	}
	return true
}

func TestResolveCredential_NilConfigPassesThrough(t *testing.T) {
	// ResolveCredential delegates to credentials.Resolve; with a nil config
	// the resolver returns a NoOpCredential (Type() == "none") rather than an
	// error.
	got, err := inference.ResolveCredential(context.Background(), "openai", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "none", got.Type())
}
