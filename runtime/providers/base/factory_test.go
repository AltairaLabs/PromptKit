package base_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeService struct{ id string }

func TestFactoryRegistry_RegisterAndCreate(t *testing.T) {
	r := base.NewFactoryRegistry[*fakeService]()
	r.Register("openai", func(spec base.CapabilitySpec) (*fakeService, error) {
		return &fakeService{id: spec.ID}, nil
	})

	got, err := r.Create(base.CapabilitySpec{ID: "test-id", Type: "openai"})
	require.NoError(t, err)
	assert.Equal(t, "test-id", got.id)
}

func TestFactoryRegistry_UnknownTypeReturnsError(t *testing.T) {
	r := base.NewFactoryRegistry[*fakeService]()
	_, err := r.Create(base.CapabilitySpec{Type: "missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider type")
}

func TestFactoryRegistry_FactoryErrorPropagates(t *testing.T) {
	r := base.NewFactoryRegistry[*fakeService]()
	r.Register("broken", func(_ base.CapabilitySpec) (*fakeService, error) {
		return nil, errors.New("boom")
	})
	_, err := r.Create(base.CapabilitySpec{Type: "broken"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestAPIKeyFromCredential_Nil(t *testing.T) {
	assert.Equal(t, "", base.APIKeyFromCredential(nil))
}

func TestAPIKeyFromCredential_APIKeyType(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("secret-123")
	assert.Equal(t, "secret-123", base.APIKeyFromCredential(cred))
}

func TestResolveCredential_NilConfigPassesThrough(t *testing.T) {
	// ResolveCredential delegates to credentials.Resolve; with a nil config
	// the resolver returns a NoOpCredential rather than an error.
	got, err := base.ResolveCredential(context.Background(), "openai", "", nil)
	require.NoError(t, err)
	assert.NotNil(t, got)
}
