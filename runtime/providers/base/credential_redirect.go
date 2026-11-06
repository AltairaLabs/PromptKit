package base

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// maxRedirects matches http.Client's default redirect limit.
const maxRedirects = 10

// errTooManyRedirects matches the error http.Client's default policy returns.
var errTooManyRedirects = errors.New("stopped after 10 redirects")

// WellKnownCredentialHeaders are the request headers that carry a credential
// for the providers PromptKit ships: bearer and API-key headers, and the
// headers a SigV4 signature sets. CredentialRedirectPolicy drops them on every
// redirect off the origin, whatever credential set them.
var WellKnownCredentialHeaders = []string{
	"Authorization",
	"Proxy-Authorization",
	"X-Api-Key",
	"Api-Key",
	"X-Goog-Api-Key",
	"Xi-Api-Key",
	"X-Amz-Date",
	"X-Amz-Security-Token",
	"X-Amz-Content-Sha256",
}

// CredentialRedirectPolicy returns an http.Client CheckRedirect function that
// keeps a credential on its origin. On a redirect to a scheme or host other
// than the first request's (see SameOriginAsFirstHop), it deletes every
// header in WellKnownCredentialHeaders and in names() from the hop before it is
// sent. http.Client copies every header but Authorization and cookies to a
// redirect hop, and keeps Authorization on a subdomain, so without this an
// X-API-Key or a SigV4 security token follows a redirect to any host.
//
// names is called only for a redirect off the origin, so a credential that
// must look itself up to name its headers does so only then. A nil names
// strips only the well-known headers.
//
// After stripping it applies next, or http.Client's default policy (stop
// after 10 redirects) when next is nil.
func CredentialRedirectPolicy(
	names func() []string, next func(*http.Request, []*http.Request) error,
) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
			stripCredentialHeaders(req.Header, names)
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= maxRedirects {
			return errTooManyRedirects
		}
		return nil
	}
}

// stripCredentialHeaders deletes the well-known credential headers and those
// names returns (when names is non-nil) from h.
func stripCredentialHeaders(h http.Header, names func() []string) {
	for _, name := range WellKnownCredentialHeaders {
		h.Del(name)
	}
	if names == nil {
		return
	}
	for _, name := range names() {
		h.Del(name)
	}
}

// SameOriginAsFirstHop reports whether req goes to the scheme and host of the
// request that started its redirect chain. A request that is not a redirect
// hop (req.Response nil) is its own first hop. A default port spelled out
// (https://h:443) is the same host as one left implicit.
func SameOriginAsFirstHop(req *http.Request) bool {
	first := req
	for first.Response != nil && first.Response.Request != nil {
		first = first.Response.Request
	}
	return sameOrigin(first.URL, req.URL)
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(canonicalHost(a), canonicalHost(b))
}

// canonicalHost returns u's host with its port, adding the scheme's default
// port when u names none, so https://h and https://h:443 compare equal.
func canonicalHost(u *url.URL) string {
	if u.Port() != "" {
		return u.Host
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host + ":443"
	case "http":
		return u.Host + ":80"
	}
	return u.Host
}
