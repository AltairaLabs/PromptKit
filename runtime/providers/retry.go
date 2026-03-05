package providers

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
)

// Default retry constants.
const (
	DefaultMaxRetries     = 3
	DefaultInitialDelayMs = 500
	DefaultBackoff        = "exponential"
	maxJitterFraction     = 0.5
	maxBackoffDuration    = 60 * time.Second
	exponentialBase       = 2
	// float64MantissaBits is the number of bits in a float64 mantissa
	// (IEEE 754 double precision). Used to convert a uint64 to [0,1).
	float64MantissaBits = 53
	// uint64Bits is the number of bits in a uint64.
	uint64Bits = 64
)

// DefaultRetryPolicy returns a RetryPolicy with sensible defaults:
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

	// Check for context cancellation -- never retry these.
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
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
	val := resp.Header.Get("Retry-After")
	if val == "" {
		return 0
	}

	// Try delta-seconds first.
	if seconds, err := strconv.Atoi(val); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	// Try HTTP-date format.
	if t, err := http.ParseTime(val); err == nil {
		delay := time.Until(t)
		if delay > 0 {
			return delay
		}
	}

	return 0
}

// cryptoRandFloat64 returns a cryptographically secure random float64
// in [0.0, 1.0) using crypto/rand.
func cryptoRandFloat64() float64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback: return 0 (no jitter) if crypto/rand fails.
		return 0
	}
	// Use top 53 bits (mantissa width) for uniform float64 in [0, 1).
	const maxMantissa = 1 << float64MantissaBits
	bitsToDiscard := uint64Bits - float64MantissaBits
	n := binary.BigEndian.Uint64(buf[:]) >> bitsToDiscard
	return float64(n) / float64(maxMantissa)
}

// calculateBackoff computes the delay for a given retry attempt
// using the configured backoff strategy with jitter.
func calculateBackoff(
	policy pipeline.RetryPolicy,
	attempt int,
	retryAfter time.Duration,
) time.Duration {
	// Honor Retry-After if present.
	if retryAfter > 0 {
		return retryAfter
	}

	initialDelay := time.Duration(policy.InitialDelayMs) * time.Millisecond
	if initialDelay <= 0 {
		initialDelay = time.Duration(DefaultInitialDelayMs) * time.Millisecond
	}

	var delay time.Duration
	switch policy.Backoff {
	case "fixed":
		delay = initialDelay
	default: // "exponential" or unset
		multiplier := math.Pow(exponentialBase, float64(attempt))
		delay = time.Duration(float64(initialDelay) * multiplier)
	}

	// Cap at maximum.
	if delay > maxBackoffDuration {
		delay = maxBackoffDuration
	}

	// Add jitter: uniform random in [0, delay * maxJitterFraction].
	jitter := time.Duration(
		cryptoRandFloat64() * maxJitterFraction * float64(delay),
	)
	delay += jitter

	return delay
}

// DoRequestFunc is a function that performs an HTTP request.
// It is called by DoWithRetry on each attempt.
type DoRequestFunc func() (*http.Response, error)

// retryState tracks the state across retry attempts.
type retryState struct {
	lastErr  error
	lastResp *http.Response
}

// DoWithRetry executes doFn with retry logic according to the given
// policy. It retries on retryable HTTP status codes (429, 502, 503,
// 504) and transient network errors. The Retry-After header is honored
// for 429 responses. On retryable HTTP errors the response body is
// closed before retrying. The caller is responsible for closing the
// body of the final returned response.
func DoWithRetry(
	ctx context.Context,
	policy pipeline.RetryPolicy,
	providerName string,
	doFn DoRequestFunc,
) (*http.Response, error) {
	maxAttempts := policy.MaxRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	state := &retryState{}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp, err := doFn()

		if err == nil && !isRetryableStatusCode(resp.StatusCode) {
			return resp, nil
		}

		retryAfter, shouldRetry := state.classify(resp, err)
		if !shouldRetry {
			return resp, err
		}

		if attempt >= maxAttempts-1 {
			break
		}

		delay := calculateBackoff(policy, attempt, retryAfter)
		logger.Warn("retrying provider request",
			"provider", providerName,
			"attempt", attempt+1,
			"max_retries", policy.MaxRetries,
			"delay", delay.String(),
			"error", state.lastErr,
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	if state.lastResp != nil {
		return state.lastResp, state.lastErr
	}
	return nil, state.lastErr
}

// classify determines whether a request result is retryable and
// updates the retry state accordingly.
func (s *retryState) classify(
	resp *http.Response, err error,
) (retryAfter time.Duration, shouldRetry bool) {
	if err != nil {
		if isRetryableError(err) {
			s.lastErr = err
			return 0, true
		}
		// Non-retryable error (e.g. context canceled).
		return 0, false
	}

	if isRetryableStatusCode(resp.StatusCode) {
		retryAfter = parseRetryAfter(resp)
		s.lastResp = resp
		s.lastErr = &RetryableHTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
		}
		// Close body so we don't leak connections on retry.
		_ = resp.Body.Close()
		return retryAfter, true
	}

	return 0, false
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
