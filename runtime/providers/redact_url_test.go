package providers

import (
	"errors"
	"fmt"
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

// TestRedactURLSecrets_LeavesInnocentTextAlone guards against a scrubber that
// mangles ordinary error messages.
func TestRedactURLSecrets_LeavesInnocentTextAlone(t *testing.T) {
	in := "dial tcp 10.0.0.1:443: i/o timeout (model=gemini-2.5-flash, keyboard=irrelevant)"
	assert.Equal(t, in, RedactURLSecrets(in))
}

// TestRedactURLSecrets_Empty covers the short-circuit. An empty error string
// is common enough (a nil cause formatted, a zero-value error) that the
// scrubber must not allocate or regex over it.
func TestRedactURLSecrets_Empty(t *testing.T) {
	assert.Equal(t, "", RedactURLSecrets(""))
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
