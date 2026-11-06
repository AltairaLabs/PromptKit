package base

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/time/rate"
)

// ErrRateLimitPastDeadline reports that a request's turn under the provider's
// rate limit comes after its context deadline, so the request was not sent. It
// wraps context.DeadlineExceeded, so retry loops treat it as a deadline and
// do not retry it.
var ErrRateLimitPastDeadline = errors.New("provider rate limit: the next request slot is past the context deadline")

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
type RateLimitTransport struct {
	Next    http.RoundTripper
	Limiter *rate.Limiter
}

// RoundTrip implements http.RoundTripper.
func (t *RateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Limiter != nil {
		if err := waitForToken(req.Context(), t.Limiter); err != nil {
			if req.Body != nil {
				_ = req.Body.Close()
			}
			return nil, err
		}
	}
	next := t.Next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(req)
}

// waitForToken waits for one token. rate.Limiter.Wait fails at once, without
// a context error, when the token would arrive after the deadline; that case
// is reported as ErrRateLimitPastDeadline wrapping context.DeadlineExceeded.
func waitForToken(ctx context.Context, limiter *rate.Limiter) error {
	err := limiter.Wait(ctx)
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return fmt.Errorf("%w: %w", ErrRateLimitPastDeadline, context.DeadlineExceeded)
}

// WithRateLimit returns rt wrapped in a RateLimitTransport for limiter,
// replacing any RateLimitTransport rt already is, so applying a new limit
// never stacks two. A nil limiter returns rt without one.
func WithRateLimit(rt http.RoundTripper, limiter *rate.Limiter) http.RoundTripper {
	if existing, ok := rt.(*RateLimitTransport); ok {
		rt = existing.Next
	}
	if limiter == nil {
		return rt
	}
	return &RateLimitTransport{Next: rt, Limiter: limiter}
}
