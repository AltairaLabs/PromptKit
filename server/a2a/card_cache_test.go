package a2aserver

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// The served agent card carries HTTP caching headers and answers conditional
// requests (A2A 1.0 §8.6), and names every REQUIRED field (#2131).

// fetchCardRaw GETs path with the given request headers and returns the
// response with its body read.
func fetchCardRaw(t *testing.T, ts *httptest.Server, path string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, http.NoBody)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(body)
}

func contentETag(body string) string {
	sum := sha256.Sum256([]byte(body))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func TestCard_ServedWithoutWithCardNamesEveryRequiredField(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	resp, body := fetchCardRaw(t, ts, a2a.AgentCardPath, map[string]string{a2a.HeaderVersion: "1.0"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{
		"name": "",
		"description": "",
		"supportedInterfaces": [
			{"url": "`+ts.URL+`/a2a", "protocolBinding": "JSONRPC", "protocolVersion": "1.0"},
			{"url": "`+ts.URL+`/a2a", "protocolBinding": "JSONRPC", "protocolVersion": "0.3"}
		],
		"version": "",
		"capabilities": {"streaming": true},
		"defaultInputModes": [],
		"defaultOutputModes": [],
		"skills": []
	}`, body)
}

func TestCard_CachingHeadersAndConditionalGet(t *testing.T) {
	_, ts := newTestServer(nopOpener, WithCard(&a2a.AgentCard{Name: "agent", Description: "d", Version: "1"}))
	defer ts.Close()
	v1 := map[string]string{a2a.HeaderVersion: "1.0"}

	for _, path := range []string{a2a.AgentCardPath, a2a.LegacyAgentCardPath} {
		resp, body := fetchCardRaw(t, ts, path, v1)
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		assert.Equal(t, "max-age=300", resp.Header.Get("Cache-Control"), path)
		assert.Equal(t, a2a.HeaderVersion, resp.Header.Get("Vary"), "the body depends on the version header")
		assert.Equal(t, contentETag(body), resp.Header.Get("ETag"), path)
	}

	resp, body := fetchCardRaw(t, ts, a2a.AgentCardPath, v1)
	etag := resp.Header.Get("ETag")

	for _, match := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		notModified, empty := fetchCardRaw(t, ts, a2a.AgentCardPath,
			map[string]string{a2a.HeaderVersion: "1.0", "If-None-Match": match})
		assert.Equal(t, http.StatusNotModified, notModified.StatusCode, match)
		assert.Empty(t, empty, match)
		assert.Equal(t, etag, notModified.Header.Get("ETag"), match)
		assert.Equal(t, "max-age=300", notModified.Header.Get("Cache-Control"), match)
		assert.Equal(t, a2a.HeaderVersion, notModified.Header.Get("Vary"), match)
	}

	stale, staleBody := fetchCardRaw(t, ts, a2a.AgentCardPath,
		map[string]string{a2a.HeaderVersion: "1.0", "If-None-Match": `"other"`})
	assert.Equal(t, http.StatusOK, stale.StatusCode)
	assert.Equal(t, body, staleBody)

	// The 0.3 rendering is a different representation with its own tag, so
	// the 1.0 tag does not validate it.
	v03, v03Body := fetchCardRaw(t, ts, a2a.AgentCardPath, map[string]string{"If-None-Match": etag})
	assert.Equal(t, http.StatusOK, v03.StatusCode)
	assert.Equal(t, contentETag(v03Body), v03.Header.Get("ETag"))
	assert.NotEqual(t, etag, v03.Header.Get("ETag"))
}

// versionedCard is an AgentCardProvider whose card version can change.
type versionedCard struct {
	mu      sync.Mutex
	version string
}

func (c *versionedCard) AgentCard(*http.Request) (*a2a.AgentCard, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &a2a.AgentCard{Name: "agent", Version: c.version}, nil
}

func TestCard_ETagChangesWithTheCard(t *testing.T) {
	provider := &versionedCard{version: "1"}
	_, ts := newTestServer(nopOpener, WithCardProvider(provider))
	defer ts.Close()

	first, _ := fetchCardRaw(t, ts, a2a.AgentCardPath, nil)
	etag := first.Header.Get("ETag")

	provider.mu.Lock()
	provider.version = "2"
	provider.mu.Unlock()

	resp, body := fetchCardRaw(t, ts, a2a.AgentCardPath, map[string]string{"If-None-Match": etag})
	assert.Equal(t, http.StatusOK, resp.StatusCode, "a changed card is sent again")
	assert.Contains(t, body, `"version":"2"`)
	assert.Equal(t, contentETag(body), resp.Header.Get("ETag"))
	assert.NotEqual(t, etag, resp.Header.Get("ETag"))
}

func TestCard_WithCardMaxAge(t *testing.T) {
	for _, tc := range []struct {
		maxAge time.Duration
		want   string
	}{
		{time.Minute, "max-age=60"},
		{90 * time.Second, "max-age=90"},
		{0, "no-cache"},
		{-time.Second, "no-cache"},
	} {
		_, ts := newTestServer(nopOpener, WithCardMaxAge(tc.maxAge))
		resp, _ := fetchCardRaw(t, ts, a2a.AgentCardPath, nil)
		ts.Close()
		assert.Equal(t, tc.want, resp.Header.Get("Cache-Control"), tc.maxAge)
		assert.NotEmpty(t, resp.Header.Get("ETag"), "an uncached card is still validated by its tag")
	}
}
