package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const probeSecret = "AIzaSyDUMMYKEYVALUE1234567890abcdef"

// TestProviderTransportError_DoesNotLeakURLCredentials is the regression for
// the reported leak. Gemini carries its API key as a ?key= query parameter, and
// Go's *url.Error embeds the full URL, so a transport failure wrote the live
// credential into the log — repeatedly, since every wrapping layer reformats
// the same string.
func TestProviderTransportError_DoesNotLeakURLCredentials(t *testing.T) {
	cause := &url.Error{
		Op:  "Post",
		URL: "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:streamGenerateContent?key=" + probeSecret,
		Err: errors.New("read tcp 10.0.0.1:1->10.0.0.2:443: read: connection reset by peer"),
	}
	err := &ProviderTransportError{Cause: cause, Provider: "gemini"}

	got := err.Error()
	assert.NotContains(t, got, probeSecret, "the API key must not reach the error string")
	assert.Contains(t, got, "generativelanguage.googleapis.com",
		"the host must survive — the message is useless without it")
	assert.Contains(t, got, "connection reset by peer",
		"the underlying cause must survive")
}

// TestProviderHTTPError_DoesNotLeakURLCredentials covers the other surface:
// ProviderHTTPError formats its URL field directly.
func TestProviderHTTPError_DoesNotLeakURLCredentials(t *testing.T) {
	err := &ProviderHTTPError{
		StatusCode: 400,
		URL:        "https://generativelanguage.googleapis.com/v1beta/models/x:generateContent?key=" + probeSecret,
		Body:       `{"error":{"message":"bad"}}`,
		Provider:   "gemini",
	}

	got := err.Error()
	assert.NotContains(t, got, probeSecret)
	assert.Contains(t, got, "status 400")
	assert.Contains(t, got, "bad")
}

// TestRedactURLSecrets_Table pins the parameter names covered and, just as
// importantly, what is left alone.
func TestRedactURLSecrets_Table(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		leaks   string // must not appear
		retains string // must appear
	}{
		{
			name:    "key",
			in:      "https://h/v1/m:go?key=" + probeSecret,
			leaks:   probeSecret,
			retains: "https://h/v1/m:go?key=",
		},
		{
			name:    "api_key case-insensitive",
			in:      "https://h/p?API_KEY=" + probeSecret,
			leaks:   probeSecret,
			retains: "API_KEY=",
		},
		{
			name:    "access_token",
			in:      "https://h/p?access_token=" + probeSecret,
			leaks:   probeSecret,
			retains: "access_token=",
		},
		{
			name:    "signature",
			in:      "https://h/p?X-Amz-Signature=" + probeSecret,
			leaks:   probeSecret,
			retains: "Signature=",
		},
		{
			name:    "secret is redacted but neighbouring params survive",
			in:      "https://h/p?model=gemini-2.5-flash&key=" + probeSecret + "&alt=sse",
			leaks:   probeSecret,
			retains: "model=gemini-2.5-flash",
		},
		{
			name:    "trailing params after the secret survive",
			in:      "https://h/p?key=" + probeSecret + "&alt=sse",
			leaks:   probeSecret,
			retains: "alt=sse",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactURLSecrets(tc.in)
			assert.NotContains(t, got, tc.leaks)
			assert.Contains(t, got, tc.retains)
			assert.Contains(t, got, "REDACTED")
		})
	}
}

// TestRedactURLSecrets_PreservesInnocentParameters is the guard the previous
// version of this test only appeared to be.
//
// It used "keyboard=irrelevant" preceded by a SPACE, and the pattern requires a
// literal ? or & before the name — so the scrubber never considered it and the
// test could not fail. Meanwhile the real case it claimed to cover, a query
// parameter whose name merely CONTAINS a credential word, was being redacted:
// ?keyword=, ?monkey=, ?assignee= and ?design= all came back REDACTED, eating
// the diagnostic context this function promises to keep.
//
// These are query parameters, with the ?, so they are actually examined.
func TestRedactURLSecrets_PreservesInnocentParameters(t *testing.T) {
	innocent := []string{
		"https://h/p?keyword=gemini",
		"https://h/p?monkey=banana",
		"https://h/p?assignee=bob",
		"https://h/p?design=abc",
		"https://h/p?keyspace=default",
		"https://h/p?signal=green",
	}

	for _, in := range innocent {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, in, RedactURLSecrets(in),
				"a name that merely contains a credential word must not be redacted")
		})
	}
}

// TestRedactURLSecrets_CoversCredentialNamesOnTokenBoundaries is the other
// half: the names that ARE credentials, including the four the substring
// pattern missed entirely (pwd, auth, code, sas).
func TestRedactURLSecrets_CoversCredentialNamesOnTokenBoundaries(t *testing.T) {
	names := []string{
		"key", "api_key", "API-KEY", "access_token", "refresh_token",
		"client_secret", "password", "passwd", "pwd", "auth", "authorization",
		"code", "sas", "X-Amz-Signature", "sig", "credential", "credentials",
	}

	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			in := "https://h/p?" + n + "=" + probeSecret
			got := RedactURLSecrets(in)
			assert.NotContains(t, got, probeSecret, "%s must be redacted", n)
			assert.Contains(t, got, n+"=", "the parameter NAME must survive")
		})
	}
}

// TestRedactURLSecrets_LeavesNonURLTextAlone keeps the original intent: prose
// error text must pass through untouched.
func TestRedactURLSecrets_LeavesNonURLTextAlone(t *testing.T) {
	in := "dial tcp 10.0.0.1:443: i/o timeout (model=gemini-2.5-flash, keyboard=irrelevant)"
	assert.Equal(t, in, RedactURLSecrets(in))
}

// TestRedactURLSecrets_RealWorldShape uses the exact error shape from the
// report, wrapped the way the pipeline wraps it.
func TestRedactURLSecrets_RealWorldShape(t *testing.T) {
	cause := &url.Error{
		Op:  "Post",
		URL: "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:streamGenerateContent?key=" + probeSecret,
		Err: errors.New("read: connection reset by peer"),
	}
	transport := &ProviderTransportError{Cause: cause, Provider: "gemini"}
	wrapped := fmt.Errorf("pipeline stage returned with error: %w",
		fmt.Errorf("failed to send request: %w", transport))

	require.NotContains(t, wrapped.Error(), probeSecret,
		"the key must not survive any wrapping layer")
	assert.True(t, strings.Contains(wrapped.Error(), "pipeline stage returned with error"))
}

// TestEmbeddingTransportErrorIsRedactable pins that the embedding path reaches
// the backstop at all.
//
// DoEmbeddingRequest used to wrap transport failures with a bare fmt.Errorf, so
// the raw *url.Error — full URL and query — was formatted straight into the
// message and the redaction never ran. Non-200s were already covered by
// ProviderHTTPError; this was the one embedding-side hole, and the next
// provider to put a credential in an embedding URL would have fallen through
// it silently.
func TestEmbeddingTransportErrorIsRedactable(t *testing.T) {
	cause := &url.Error{
		Op:  "Post",
		URL: "https://h/v1/models/m:embedContent?key=" + probeSecret,
		Err: errors.New("connection reset by peer"),
	}
	err := &ProviderTransportError{Cause: cause, Provider: "gemini-embedding"}

	assert.NotContains(t, err.Error(), probeSecret)
	assert.True(t, IsTransient(err),
		"an embedding transport failure must also classify as transient, like every other path")
}

// failingTransport makes HTTPClient.Do return a *url.Error carrying the request
// URL, which is what Go's transport really does on a connection failure.
type failingTransport struct{}

func (failingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, errors.New("connection reset by peer")
}

// TestDoEmbeddingRequest_WrapsTransportErrorsRedactably is the WIRING test for
// the embedding backstop.
//
// TestEmbeddingTransportErrorIsRedactable above only proves the error TYPE
// redacts; it passes even if DoEmbeddingRequest never constructs one. This
// drives the real call path with a failing transport, so reverting to a bare
// fmt.Errorf — which is what leaked the key in #1871 — fails here.
func TestDoEmbeddingRequest_WrapsTransportErrorsRedactably(t *testing.T) {
	b := &BaseEmbeddingProvider{
		ProviderID: "gemini-embedding",
		HTTPClient: &http.Client{Transport: failingTransport{}},
	}

	_, err := b.DoEmbeddingRequest(context.Background(), HTTPRequestConfig{
		URL:  "https://h/v1/models/m:embedContent?key=" + probeSecret,
		Body: []byte(`{}`),
	})

	require.Error(t, err)

	var transportErr *ProviderTransportError
	require.ErrorAs(t, err, &transportErr,
		"embedding transport failures must be a ProviderTransportError, or the "+
			"redaction backstop never runs on this path")
	assert.NotContains(t, err.Error(), probeSecret,
		"the URL credential must not survive into the error string")
}

// TestRedactURLSecrets_Empty covers the short-circuit. An empty error string is
// common enough (a nil cause formatted, a zero-value error) that the scrubber
// must not regex over it.
func TestRedactURLSecrets_Empty(t *testing.T) {
	assert.Equal(t, "", RedactURLSecrets(""))
}
