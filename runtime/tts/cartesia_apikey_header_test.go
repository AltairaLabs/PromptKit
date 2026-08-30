package tts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cartesiaProbeKey = "sk_car_DUMMYKEYVALUE1234567890"

// TestCartesia_APIKeyTravelsInHeaderNotURL is the audit fix from #1880.
//
// The key used to ride in the WebSocket URL as ?api_key=. Any dial failure puts
// that URL inside *url.Error, which is wrapped into a SynthesisError and
// logged — the same leak shape as the Gemini credential in #1871, in a provider
// nobody had checked.
//
// Verified live before changing it: this endpoint accepts X-API-Key, with the
// version left in the query.
func TestCartesia_APIKeyTravelsInHeaderNotURL(t *testing.T) {
	var gotURL string
	var gotHeader string

	// A plain HTTP server is enough: the dial fails the upgrade, but not before
	// the request line and headers have been sent, which is all this asserts.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		gotHeader = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	s := NewCartesia(cartesiaProbeKey)
	s.wsURL = "ws" + strings.TrimPrefix(srv.URL, "http")

	// The upgrade will fail; the request having been made is the point.
	_, _ = s.SynthesizeStream(context.Background(), "hello", SynthesisConfig{})

	require.NotEmpty(t, gotURL, "the dial must have reached the server")
	assert.NotContains(t, gotURL, cartesiaProbeKey,
		"the API key must not appear in the WebSocket URL")
	assert.NotContains(t, gotURL, "api_key",
		"no api_key query parameter at all")
	assert.Equal(t, cartesiaProbeKey, gotHeader,
		"the key travels in X-API-Key instead")
	assert.Contains(t, gotURL, "cartesia_version",
		"the wire version stays in the query — it is not a secret")
}
