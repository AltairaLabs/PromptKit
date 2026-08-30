package gemini

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const urlProbeKey = "AIzaSyDUMMYKEYVALUE1234567890abcdef"

func aiStudioProvider() *Provider {
	return &Provider{
		platform: "",
		baseURL:  "https://generativelanguage.googleapis.com/v1beta",
		apiKey:   urlProbeKey,
		model:    "gemini-2.5-flash",
	}
}

// TestAPIKeyNeverAppearsInAnyURL is the source fix for the credential leak.
//
// The key used to travel as a ?key= query parameter, so Go's *url.Error carried
// it into every transport-failure log. Error-string redaction is a backstop;
// this is the actual defence — a credential that is not in the URL cannot leak
// through one.
func TestAPIKeyNeverAppearsInAnyURL(t *testing.T) {
	p := aiStudioProvider()

	urls := map[string]string{
		"generateContent":       p.generateContentURL("generateContent"),
		"streamGenerateContent": p.generateContentURL("streamGenerateContent"),
		"interactions":          p.interactionsURL(),
		"cachedContentsCreate":  p.cachedContentsCreateURL(),
		"cachedContentDelete":   p.cachedContentDeleteURL("cachedContents/abc"),
	}

	for name, u := range urls {
		t.Run(name, func(t *testing.T) {
			assert.NotContains(t, u, urlProbeKey, "the API key must not be in the URL")
			assert.NotContains(t, u, "key=", "no key query parameter at all")
			assert.Contains(t, u, "generativelanguage.googleapis.com",
				"the endpoint itself must be unchanged")
		})
	}
}

// TestApplyAuth_SetsAPIKeyHeaderForAIStudio pins where the credential goes
// instead. websocket_manager.go already used this header; the REST paths did
// not.
func TestApplyAuth_SetsAPIKeyHeaderForAIStudio(t *testing.T) {
	p := aiStudioProvider()

	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodPost, p.generateContentURL("generateContent"), http.NoBody)
	require.NoError(t, err)

	require.NoError(t, p.applyAuth(context.Background(), req))

	assert.Equal(t, urlProbeKey, req.Header.Get("x-goog-api-key"),
		"AI Studio authenticates by header now")
	assert.Empty(t, req.Header.Get("Authorization"),
		"AI Studio must not send a Bearer token")
}

// TestApplyAuth_VertexUnaffected guards the other platform: Vertex uses an
// OAuth Bearer token from the credential chain and never an API key.
func TestApplyAuth_VertexUnaffected(t *testing.T) {
	p := &Provider{
		platform: vertexPlatform,
		baseURL:  "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/publishers/google/models",
		model:    "gemini-2.5-flash",
		apiKey:   urlProbeKey,
	}

	u := p.generateContentURL("generateContent")
	assert.NotContains(t, u, urlProbeKey)
	assert.False(t, strings.Contains(u, "key="))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, u, http.NoBody)
	require.NoError(t, err)
	// No credential wired: applyAuth is a no-op and must not fall back to the
	// API-key header on Vertex.
	require.NoError(t, p.applyAuth(context.Background(), req))
	assert.Empty(t, req.Header.Get("x-goog-api-key"),
		"Vertex must never authenticate with the AI Studio API key")
}
