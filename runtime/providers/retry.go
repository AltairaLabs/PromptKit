package providers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/internal/retry"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// Default retry constants.
const (
	DefaultMaxRetries     = 3
	DefaultInitialDelayMs = 500
	DefaultBackoff        = "exponential"
	maxBackoffDuration    = 60 * time.Second
)

// DefaultRetryPolicy returns the RetryPolicy providers start with:
// 3 retries, exponential backoff, 500ms initial delay.
func DefaultRetryPolicy() pipeline.RetryPolicy {
	return pipeline.RetryPolicy{
		MaxRetries:     DefaultMaxRetries,
		Backoff:        DefaultBackoff,
		InitialDelayMs: DefaultInitialDelayMs,
	}
}

// isRetryableStatusCode returns true for HTTP status codes that indicate
// a transient error worth retrying.
func isRetryableStatusCode(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// isRetryableError returns true for transient network errors that are
// worth retrying (connection refused, DNS errors, timeouts, etc.).
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation -- never retry these. Nor a credential
	// that could not be applied: the next attempt would fail the same way.
	if retry.IsContextError(err) || errors.Is(err, base.ErrRequestAuthorization) {
		return false
	}

	// Check for net.Error (includes timeouts and temporary errors).
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// Check for DNS errors.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// Check for connection refused / reset.
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// parseRetryAfter extracts the delay from a Retry-After header.
// It supports both delta-seconds and HTTP-date formats.
// Returns 0 if the header is missing or cannot be parsed.
func parseRetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	return retry.ParseRetryAfter(resp.Header)
}

// httpBackoff is the backoff a RetryPolicy describes: InitialDelayMs (default
// DefaultInitialDelayMs), doubled per attempt unless Backoff is "fixed",
// lengthened by up to half at random, and never more than a minute.
func httpBackoff(policy pipeline.RetryPolicy) retry.Backoff {
	initialDelay := time.Duration(policy.InitialDelayMs) * time.Millisecond
	if initialDelay <= 0 {
		initialDelay = time.Duration(DefaultInitialDelayMs) * time.Millisecond
	}
	b := retry.Exponential(initialDelay, maxBackoffDuration)
	if policy.Backoff == "fixed" {
		b.Multiplier = 1
	}
	return b
}

// DoRequestFunc is a function that performs an HTTP request.
// It is called by DoWithRetry on each attempt.
type DoRequestFunc func() (*http.Response, error)

// DoWithRetry executes doFn with retry logic according to the given
// policy. It retries on retryable HTTP status codes (429, 502, 503,
// 504) and transient network errors, and never after context
// cancellation. A Retry-After header on a retryable response replaces the
// computed backoff delay. The body of a retryable response is closed before
// retrying. The caller closes the body of a successful response.
//
// When every attempt fails with a retryable status, DoWithRetry returns the
// last response, whose body is already closed, together with a
// *RetryableHTTPError.
func DoWithRetry(
	ctx context.Context,
	policy pipeline.RetryPolicy,
	providerName string,
	doFn DoRequestFunc,
) (*http.Response, error) {
	// retryAfter is what the last retryable response asked to wait.
	var retryAfter time.Duration
	return retry.Do(ctx, retry.Policy{
		Name:        providerName,
		MaxAttempts: policy.MaxRetries + 1,
		Backoff:     httpBackoff(policy),
		LogAttrs:    []any{"provider", providerName},
		Classify: func(err error) (bool, time.Duration) {
			var statusErr *RetryableHTTPError
			if errors.As(err, &statusErr) {
				return true, retryAfter
			}
			return isRetryableError(err), 0
		},
	}, func(context.Context) (*http.Response, error) {
		resp, err := doFn()
		if err != nil || !isRetryableStatusCode(resp.StatusCode) {
			return resp, err
		}
		retryAfter = parseRetryAfter(resp)
		// Close body so we don't leak connections on retry.
		_ = resp.Body.Close()
		return resp, &RetryableHTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	})
}

// RetryableHTTPError is returned when all retries are exhausted for
// a retryable HTTP status code.
type RetryableHTTPError struct {
	StatusCode int
	Status     string
}

// Error implements the error interface.
func (e *RetryableHTTPError) Error() string {
	return "request failed after retries: " + e.Status
}
