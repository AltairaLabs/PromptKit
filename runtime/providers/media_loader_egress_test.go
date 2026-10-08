package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/httputil"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// newLocalMediaLoader builds a loader that may fetch from the loopback
// httptest servers these tests use. Production loaders refuse them.
func newLocalMediaLoader(cfg providers.MediaLoaderConfig) *providers.MediaLoader {
	cfg.AllowPrivateNetworks = true
	return providers.NewMediaLoader(cfg)
}

// A media URL can come from a remote client or a model. Fetching it from the
// host must not reach the host's own network: a loopback server stands in for
// a cloud metadata service here.
func TestMediaLoader_RefusesNonPublicURLsByDefault(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"AccessKeyId":"secret"}`))
	}))
	defer srv.Close()

	url := srv.URL + "/latest/meta-data/iam/security-credentials/role"
	media := &types.MediaContent{URL: &url, MIMEType: "text/plain"}

	_, err := providers.NewMediaLoader(providers.MediaLoaderConfig{}).GetBase64Data(context.Background(), media)
	require.Error(t, err)
	assert.True(t, errors.Is(err, httputil.ErrNonPublicDestination), "err = %v", err)
	assert.Zero(t, hits, "the server was reached")
}

// The opt-in is per provider, and reaches the provider's loader.
func TestBaseProvider_AllowPrivateNetworkMedia(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("png"))
	}))
	defer srv.Close()
	url := srv.URL
	media := &types.MediaContent{URL: &url, MIMEType: "image/png"}

	var b providers.BaseProvider
	_, err := b.MediaLoader().GetBase64Data(context.Background(), media)
	assert.True(t, errors.Is(err, httputil.ErrNonPublicDestination), "default: err = %v", err)

	var _ providers.PrivateNetworkMediaConfigurable = &b
	b.SetAllowPrivateNetworkMedia(true)
	_, err = b.MediaLoader().GetBase64Data(context.Background(), media)
	assert.NoError(t, err, "opted in")
}
