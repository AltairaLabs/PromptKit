package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Authorizer supplies credentials for an HTTP MCP server and handles its
// authorization challenges (MCP basic/authorization).
//
// PromptKit runs no OAuth flow and stores no secrets. Discovering the
// authorization server (RFC 9728 protected resource metadata, RFC 8414),
// registering the client, obtaining the user's consent, validating the
// issuer, and storing and refreshing tokens belong to the host — the hosting
// runtime that owns secret storage and the user. The client calls the
// Authorizer at the two points the protocol defines, and bounds the retries.
type Authorizer interface {
	// Authorize adds credentials to an outgoing HTTP request: typically an
	// Authorization header, and a DPoP proof where one is used. It is
	// called for every request, including retries after a challenge.
	Authorize(ctx context.Context, req *http.Request) error

	// Challenge is called when the server rejects a request with 401, or
	// with 403 and error="insufficient_scope" (scope step-up). Return nil
	// once new credentials are ready: the request is built and sent again,
	// through Authorize. Return an error to fail the request.
	Challenge(ctx context.Context, challenge *AuthChallenge) error
}

// AuthChallenge describes a server's refusal of a request for lack of
// authorization.
type AuthChallenge struct {
	// Server is the server's name in the client configuration.
	Server string
	// ResourceURL is the URL of the request the server refused: the MCP
	// endpoint, which is the protected resource.
	ResourceURL string
	// Status is 401 (missing or invalid credentials) or 403 (insufficient
	// scope).
	Status int
	// Challenges are the parsed WWW-Authenticate challenges.
	Challenges []WWWAuthenticate
	// ResourceMetadata, Scope, Error and ErrorDescription are the parameters
	// of the Bearer challenge, if there is one. ResourceMetadata is the
	// protected resource metadata URL (RFC 9728); Scope is the scope the
	// server needs.
	ResourceMetadata string
	Scope            string
	Error            string
	ErrorDescription string
	// Header is the response's full header.
	Header http.Header
	// Attempt counts the challenges for this request, starting at 1.
	Attempt int
}

// WWWAuthenticate is one challenge from a WWW-Authenticate header
// (RFC 9110 §11.6.1).
type WWWAuthenticate struct {
	Scheme string
	Params map[string]string
}

// AuthError is returned when a request stays unauthorized: the Authorizer
// failed, or the server kept refusing after maxAuthChallenges challenges.
type AuthError struct {
	Server string
	Status int
	Err    error
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("mcp: server %s refused authorization (HTTP %d): %v", e.Server, e.Status, e.Err)
}

func (e *AuthError) Unwrap() error { return e.Err }

// maxAuthChallenges bounds the challenges answered for one request, so a
// server that keeps asking for more scope cannot hold the client in a loop.
const maxAuthChallenges = 3

// errAuthRetriesExhausted is wrapped in an AuthError when the server still
// refuses after maxAuthChallenges challenges.
var errAuthRetriesExhausted = errors.New("still unauthorized after answering its challenges")

// httpDoer sends a transport's HTTP requests, applying the Authorizer.
type httpDoer struct {
	client *http.Client
	auth   Authorizer
	server string
}

// do sends a request. On a challenge it asks the Authorizer for new
// credentials and sends a fresh copy of the request, up to
// maxAuthChallenges times. Without an Authorizer the response is returned
// as-is. tmpl is never sent itself; each attempt sends a clone, so
// credentials from one attempt do not leak into the next.
func (d *httpDoer) do(tmpl *http.Request) (*http.Response, error) {
	ctx := tmpl.Context()
	for attempt := 1; ; attempt++ {
		req, err := cloneRequest(tmpl)
		if err != nil {
			return nil, err
		}
		if d.auth != nil {
			if aerr := d.auth.Authorize(ctx, req); aerr != nil {
				return nil, &AuthError{Server: d.server, Err: aerr}
			}
		}
		resp, err := d.client.Do(req)
		if err != nil || d.auth == nil || !isAuthChallenge(resp) {
			return resp, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		_ = resp.Body.Close()
		if attempt > maxAuthChallenges {
			return nil, &AuthError{Server: d.server, Status: resp.StatusCode, Err: errAuthRetriesExhausted}
		}
		challenge := newAuthChallenge(d.server, req.URL.String(), resp, attempt)
		if cerr := d.auth.Challenge(ctx, challenge); cerr != nil {
			return nil, &AuthError{Server: d.server, Status: resp.StatusCode, Err: cerr}
		}
	}
}

// cloneRequest copies a request, with a fresh body.
func cloneRequest(tmpl *http.Request) (*http.Request, error) {
	req := tmpl.Clone(tmpl.Context())
	if tmpl.GetBody != nil {
		body, err := tmpl.GetBody()
		if err != nil {
			return nil, err
		}
		req.Body = body
	}
	return req, nil
}

// isAuthChallenge reports whether a response asks for (more)
// authorization: 401, or 403 with error="insufficient_scope".
func isAuthChallenge(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return true
	case http.StatusForbidden:
		for _, c := range parseWWWAuthenticate(resp.Header.Values("WWW-Authenticate")) {
			if c.Params["error"] == "insufficient_scope" {
				return true
			}
		}
	}
	return false
}

func newAuthChallenge(server, resourceURL string, resp *http.Response, attempt int) *AuthChallenge {
	c := &AuthChallenge{
		Server:      server,
		ResourceURL: resourceURL,
		Status:      resp.StatusCode,
		Challenges:  parseWWWAuthenticate(resp.Header.Values("WWW-Authenticate")),
		Header:      resp.Header.Clone(),
		Attempt:     attempt,
	}
	for _, ch := range c.Challenges {
		if !strings.EqualFold(ch.Scheme, "Bearer") {
			continue
		}
		c.ResourceMetadata = ch.Params["resource_metadata"]
		c.Scope = ch.Params["scope"]
		c.Error = ch.Params["error"]
		c.ErrorDescription = ch.Params["error_description"]
		break
	}
	return c
}

// parseWWWAuthenticate parses WWW-Authenticate header values into
// challenges: a scheme followed by comma-separated auth-params
// (name=token or name="quoted string"). A header may carry several
// challenges; a token not followed by "=" starts the next one.
func parseWWWAuthenticate(values []string) []WWWAuthenticate {
	var out []WWWAuthenticate
	for _, v := range values {
		p := &challengeParser{s: v}
		for {
			p.skip(", \t")
			scheme := p.token()
			if scheme == "" {
				break
			}
			ch := WWWAuthenticate{Scheme: scheme, Params: map[string]string{}}
			for {
				mark := p.i
				p.skip(", \t")
				name := p.token()
				p.skip(" \t")
				if name == "" || !p.consume('=') {
					p.i = mark // a scheme (or the end): the next challenge
					break
				}
				p.skip(" \t")
				ch.Params[strings.ToLower(name)] = p.value()
			}
			out = append(out, ch)
		}
	}
	return out
}

type challengeParser struct {
	s string
	i int
}

func (p *challengeParser) skip(chars string) {
	for p.i < len(p.s) && strings.IndexByte(chars, p.s[p.i]) >= 0 {
		p.i++
	}
}

func (p *challengeParser) consume(c byte) bool {
	if p.i < len(p.s) && p.s[p.i] == c {
		p.i++
		return true
	}
	return false
}

// token reads an RFC 9110 token (and token68 characters, for schemes whose
// credentials use them).
func (p *challengeParser) token() string {
	start := p.i
	for p.i < len(p.s) && (isTokenChar(p.s[p.i]) || p.s[p.i] == '/') {
		p.i++
	}
	return p.s[start:p.i]
}

// value reads a token or a quoted string, unescaping quoted pairs.
func (p *challengeParser) value() string {
	if !p.consume('"') {
		return p.token()
	}
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		switch {
		case c == '\\' && p.i < len(p.s):
			b.WriteByte(p.s[p.i])
			p.i++
		case c == '"':
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
