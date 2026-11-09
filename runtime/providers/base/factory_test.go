package base_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

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

// ResolveCredential passes the provider type and config dir through to the
// resolver: a relative credential_file is read from cfgDir, and the provider
// type picks the header the key is sent in.
func TestResolveCredential_PassesProviderTypeAndConfigDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "key.txt"), []byte("sk-from-file\n"), 0o600))

	got, err := base.ResolveCredential(context.Background(), "claude", dir,
		&credentials.CredentialConfig{CredentialFile: "key.txt"})
	require.NoError(t, err)
	assert.Equal(t, "sk-from-file", base.APIKeyFromCredential(got))

	req := httptest.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", http.NoBody)
	require.NoError(t, got.Apply(context.Background(), req))
	assert.Equal(t, "sk-from-file", req.Header.Get("X-API-Key"))
	assert.Empty(t, req.Header.Get("Authorization"))
}

// With no config and no default env var set, the credential sends nothing.
func TestResolveCredential_NoConfigNoEnvSendsNoAuth(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_TOKEN", "")

	got, err := base.ResolveCredential(context.Background(), "openai", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "none", got.Type())

	req := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", http.NoBody)
	require.NoError(t, got.Apply(context.Background(), req))
	assert.Empty(t, req.Header)
}

// tunableService records the tuning applied to it.
type tunableService struct {
	got     base.HTTPTuning
	applied bool
	err     error
}

func (s *tunableService) ApplyHTTPTuning(t base.HTTPTuning) error {
	s.got, s.applied = t, true
	return s.err
}

func TestFactoryRegistry_TuningRejectedForNonTunable(t *testing.T) {
	r := base.NewFactoryRegistry[*fakeService]()
	r.Register("plain", func(base.CapabilitySpec) (*fakeService, error) { return &fakeService{}, nil })
	_, err := r.Create(base.CapabilitySpec{
		Type: "plain", Tuning: base.HTTPTuning{Headers: map[string]string{"X": "y"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(),
		`provider type "plain" does not support headers, request_timeout, http_transport or rate_limit for this role`)
}

func TestFactoryRegistry_ZeroTuningOnNonTunableSucceeds(t *testing.T) {
	r := base.NewFactoryRegistry[*fakeService]()
	r.Register("plain", func(base.CapabilitySpec) (*fakeService, error) { return &fakeService{id: "p"}, nil })
	got, err := r.Create(base.CapabilitySpec{Type: "plain"})
	require.NoError(t, err)
	assert.Equal(t, "p", got.id)
}

func TestFactoryRegistry_TuningAppliedToTunable(t *testing.T) {
	r := base.NewFactoryRegistry[*tunableService]()
	r.Register("tun", func(base.CapabilitySpec) (*tunableService, error) { return &tunableService{}, nil })
	tuning := base.HTTPTuning{RequestTimeout: 5 * time.Second}
	got, err := r.Create(base.CapabilitySpec{Type: "tun", Tuning: tuning})
	require.NoError(t, err)
	assert.True(t, got.applied)
	assert.Equal(t, 5*time.Second, got.got.RequestTimeout)
}

func TestFactoryRegistry_TuningErrorPropagates(t *testing.T) {
	r := base.NewFactoryRegistry[*tunableService]()
	boom := errors.New("boom")
	r.Register("tun", func(base.CapabilitySpec) (*tunableService, error) { return &tunableService{err: boom}, nil })
	_, err := r.Create(base.CapabilitySpec{Type: "tun", Tuning: base.HTTPTuning{RequestTimeout: time.Second}})
	require.ErrorIs(t, err, boom)
}
