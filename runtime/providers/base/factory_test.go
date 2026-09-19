package base_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
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

func TestFactoryRegistry_TypesListsRegisteredTypesSorted(t *testing.T) {
	r := base.NewFactoryRegistry[*fakeService]()
	// Registered out of order, and with enough entries that a map-iteration
	// order would be vanishingly unlikely to come out sorted by chance.
	for _, name := range []string{"openai", "cartesia", "elevenlabs", "polly", "deepgram", "azure"} {
		r.Register(name, func(_ base.CapabilitySpec) (*fakeService, error) { return &fakeService{}, nil })
	}

	assert.Equal(t, []string{"azure", "cartesia", "deepgram", "elevenlabs", "openai", "polly"}, r.Types())
}

func TestFactoryRegistry_TypesEmptyRegistry(t *testing.T) {
	assert.Empty(t, base.NewFactoryRegistry[*fakeService]().Types())
}

func TestFactoryRegistry_TypesReflectsLaterRegistration(t *testing.T) {
	r := base.NewFactoryRegistry[*fakeService]()
	r.Register("first", func(_ base.CapabilitySpec) (*fakeService, error) { return &fakeService{}, nil })
	require.Equal(t, []string{"first"}, r.Types())

	// Types must read the live map, not a snapshot taken at construction.
	r.Register("second", func(_ base.CapabilitySpec) (*fakeService, error) { return &fakeService{}, nil })
	assert.Equal(t, []string{"first", "second"}, r.Types())
}

func TestFactoryRegistry_TypesIsRaceSafeWithRegister(t *testing.T) {
	// Fails under -race if Types reads the map without holding the lock.
	r := base.NewFactoryRegistry[*fakeService]()
	f := func(_ base.CapabilitySpec) (*fakeService, error) { return &fakeService{}, nil }
	r.Register("seed", f)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 50 {
			r.Register("t"+strconv.Itoa(i), f)
		}
	}()
	go func() {
		defer wg.Done()
		for range 50 {
			assert.NotEmpty(t, r.Types())
		}
	}()
	wg.Wait()
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
