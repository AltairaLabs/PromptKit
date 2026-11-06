package base

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/time/rate"
)

// ErrRateLimitPastDeadline reports that a request's turn under the provider's
// rate limit comes after its context deadline, so the request was not sent. It
// wraps context.DeadlineExceeded, so retry loops treat it as a deadline and
// do not retry it.
var ErrRateLimitPastDeadline = errors.New("provider rate limit: the next request slot is past the context deadline")

// ErrRequestAuthorization wraps an error from RateLimitTransport.Authorize:
// the request's credential could not be applied, so it was not sent. Retry
// loops do not retry it, and providers do not class it as transient: a
// credential that cannot be fetched or signed is not an infrastructure blip.
var ErrRequestAuthorization = errors.New("provider credential could not be applied")

// NewRequestLimiter returns the token bucket for a provider's rate_limit: a
// sustained requestsPerSecond with room for burst requests at once. A
// requestsPerSecond of zero or less returns nil, which means unlimited. A burst
// below 1 is raised to 1, so a rate without a burst spaces requests evenly
// rather than refusing them all.
func NewRequestLimiter(requestsPerSecond float64, burst int) *rate.Limiter {
	if requestsPerSecond <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	return rate.NewLimiter(rate.Limit(requestsPerSecond), burst)
}

// RateLimitTransport is a RoundTripper that waits for a token from Limiter
// before handing each request to Next (http.DefaultTransport when nil). The
// wait ends early when the request's context is done: a canceled request
// returns context.Canceled, and one whose turn falls past its deadline returns
// ErrRateLimitPastDeadline. Neither is sent. A nil Limiter does not wait.
//
// Timeout, when positive, bounds each request from the moment its wait ends,
// exactly as http.Client.Timeout would (through the response body), so time
// spent waiting for a token does not count against it. ApplyRateLimit moves a
// client's Timeout here: http.Client.Timeout puts a deadline on the request
// before any RoundTripper runs, which would make the wait eat the timeout and
// refuse every request whose turn is further off than the timeout.
//
// Authorize, when set, applies the sender's credential to a copy of each
// request after its wait ends and before it is sent, so a SigV4 signature or a
// bearer token is never older than the wait. It runs once per request: the
// sender does not apply the credential while building a request that goes
// through this transport. An Authorize error fails the request without sending
// it.
//
// A redirect hop is a request of its own, with its own wait. http.Client
// builds a hop's headers from the original request, which carries no
// credential, so a hop to the same scheme and host as the original request is
// authorized after its wait like any other. A hop to another scheme or host is
// never authorized, so the credential does not follow a redirect off the
// origin.
type RateLimitTransport struct {
	Next      http.RoundTripper
	Limiter   TokenWaiter
	Timeout   time.Duration
	Authorize func(ctx context.Context, req *http.Request) error
}

// TokenWaiter is what RateLimitTransport waits on before each request: it
// returns once the request may be sent, or with an error when the request's
// context ends first. *rate.Limiter implements it.
type TokenWaiter interface {
	Wait(ctx context.Context) error
}

// RoundTrip implements http.RoundTripper.
func (t *RateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if hasLimiter(t.Limiter) {
		if err := waitForToken(req.Context(), t.Limiter); err != nil {
			closeBody(req)
			return nil, err
		}
	}
	if t.Authorize != nil && SameOriginAsFirstHop(req) {
		// Clone before applying: the RoundTripper contract forbids modifying
		// the caller's request.
		authed := req.Clone(req.Context())
		if err := t.Authorize(authed.Context(), authed); err != nil {
			closeBody(req)
			return nil, fmt.Errorf("%w: %w", ErrRequestAuthorization, err)
		}
		req = authed
	}
	next := t.Next
	if next == nil {
		next = http.DefaultTransport
	}
	if t.Timeout <= 0 {
		return next.RoundTrip(req)
	}
	// An inner client applies the timeout with http.Client's own semantics
	// (the same timeout error, still treated as transient by retry loops).
	// Redirects are left to the outer client, so each hop waits its turn.
	timed := &http.Client{Transport: next, Timeout: t.Timeout, CheckRedirect: useLastResponse}
	resp, err := timed.Do(req)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// The outer client wraps the error in its own *url.Error.
		return nil, urlErr.Err
	}
	return resp, err
}

func useLastResponse(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func closeBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}

// hasLimiter reports whether w waits at all: a nil interface or a nil
// *rate.Limiter in it both mean unlimited.
func hasLimiter(w TokenWaiter) bool {
	if l, ok := w.(*rate.Limiter); ok {
		return l != nil
	}
	return w != nil
}

// waitForToken waits for one token. rate.Limiter.Wait fails at once, without
// a context error, when the token would arrive after the deadline; that case
// is reported as ErrRateLimitPastDeadline wrapping context.DeadlineExceeded.
func waitForToken(ctx context.Context, limiter TokenWaiter) error {
	err := limiter.Wait(ctx)
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return fmt.Errorf("%w: %w", ErrRateLimitPastDeadline, context.DeadlineExceeded)
}

// ApplyRateLimit puts limiter in front of c's transport, replacing any limit c
// already has, so applying a new one never stacks two. c's Timeout moves onto
// the RateLimitTransport so the wait does not use it up. A nil limiter removes
// the limit and gives the timeout back to c. A nil c is a no-op.
func ApplyRateLimit(c *http.Client, limiter *rate.Limiter) {
	if c == nil {
		return
	}
	inner, timeout := ClientTransport(c), ClientTimeout(c)
	if limiter == nil {
		c.Transport, c.Timeout = inner, timeout
		return
	}
	c.Transport = &RateLimitTransport{Next: inner, Limiter: limiter, Timeout: timeout}
	c.Timeout = 0
}

// ClientTimeout returns c's effective request timeout: c.Timeout, or the
// Timeout ApplyRateLimit moved onto its RateLimitTransport. Nil c returns 0.
func ClientTimeout(c *http.Client) time.Duration {
	if c == nil {
		return 0
	}
	if rlt, ok := c.Transport.(*RateLimitTransport); ok && c.Timeout == 0 {
		return rlt.Timeout
	}
	return c.Timeout
}

// ClientTransport returns c's transport beneath any RateLimitTransport. Nil c
// returns nil.
func ClientTransport(c *http.Client) http.RoundTripper {
	if c == nil {
		return nil
	}
	if rlt, ok := c.Transport.(*RateLimitTransport); ok {
		return rlt.Next
	}
	return c.Transport
}
