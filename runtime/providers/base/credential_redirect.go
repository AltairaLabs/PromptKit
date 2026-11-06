package base

import (
	"net/http"

	"github.com/AltairaLabs/PromptKit/runtime/v2/httputil"
)

// WellKnownCredentialHeaders is httputil.WellKnownCredentialHeaders: the
// headers CredentialRedirectPolicy always drops off the origin.
var WellKnownCredentialHeaders = httputil.WellKnownCredentialHeaders

// CredentialRedirectPolicy is httputil.CredentialRedirectPolicy; see there.
// The policy lives in httputil so packages that do not otherwise depend on
// the providers (the a2a client) can use it.
func CredentialRedirectPolicy(
	names func() []string, next func(*http.Request, []*http.Request) error,
) func(*http.Request, []*http.Request) error {
	return httputil.CredentialRedirectPolicy(names, next)
}

// SameOriginAsFirstHop is httputil.SameOriginAsFirstHop; see there.
func SameOriginAsFirstHop(req *http.Request) bool {
	return httputil.SameOriginAsFirstHop(req)
}

// WithCredentialRedirectPolicy is httputil.WithCredentialRedirectPolicy; see
// there.
func WithCredentialRedirectPolicy(c *http.Client) *http.Client {
	return httputil.WithCredentialRedirectPolicy(c)
}
