package sdk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
)

// fakeGeminiAPI answers every streamGenerateContent call with a one-word reply,
// recording whether any request carried inline media.
func fakeGeminiAPI(t *testing.T, sawMedia *atomic.Bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "inlineData") || strings.Contains(string(body), "inline_data") {
			sawMedia.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},` +
			`"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}]`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// sendImageURL sends one message carrying an image URL on a loopback server to
// a Gemini provider, which fetches URL media itself and inlines the bytes. It
// reports how often that server was reached, whether the image reached
// Gemini, and Send's error.
func sendImageURL(t *testing.T, opts ...Option) (hits int32, sentMedia bool, sendErr error) {
	t.Helper()
	var count atomic.Int32
	var sawMedia atomic.Bool
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer internal.Close()

	provider := gemini.NewProvider("gemini", "gemini-test", fakeGeminiAPI(t, &sawMedia).URL, providers.ProviderDefaults{}, false)
	opts = append([]Option{WithProvider(provider), WithSkipSchemaValidation()}, opts...)
	conv, err := Open("./testdata/packs/eval-test.pack.json", "assistant", opts...)
	require.NoError(t, err)
	defer func() { _ = conv.Close() }()

	_, sendErr = conv.Send(context.Background(), "describe this", WithImageURL(internal.URL+"/latest/meta-data/"))
	return count.Load(), sawMedia.Load(), sendErr
}

// A media URL in a message can come from a remote client. By default the
// provider must not fetch it from the host's own network, which is where the
// cloud metadata service lives; a loopback server stands in for it.
//
// The refusal is an error: the message is not sent without its image, which
// would have the model answer about media it never saw.
func TestPrivateNetworkMedia_RefusedByDefault(t *testing.T) {
	hits, sentMedia, err := sendImageURL(t)
	assert.Zero(t, hits, "the provider reached a non-public address")
	assert.False(t, sentMedia, "media from a non-public address reached the model")
	assert.ErrorContains(t, err, "image part")
}

func TestPrivateNetworkMedia_AllowedWhenTheHostOptsIn(t *testing.T) {
	hits, sentMedia, err := sendImageURL(t, WithUnsafePrivateNetworkMedia())
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits)
	assert.True(t, sentMedia)
}
