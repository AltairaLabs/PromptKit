package providers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HostFromURL extracts just the host portion (without scheme or path)
// from a URL string, intended for use as a Prometheus label on
// streaming metrics. Returns an empty string on parse error — callers
// treat empty-host labels as "unknown host" rather than failing.
func HostFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

// Default values for StreamRetryPolicy. Kept small on purpose: streaming retry
// targets transient h2 stream resets, not generic 5xx storms, and the wrong
// default is "retry aggressively". See docs/local-backlog/STREAMING_RETRY_AT_SCALE.md.
const (
	DefaultStreamRetryMaxAttempts  = 2
	DefaultStreamRetryInitialDelay = 250 * time.Millisecond
	DefaultStreamRetryMaxDelay     = 2 * time.Second
)

// StreamRetryWindow enumerates the points at which a streaming request may
// still be retried. Only pre-first-chunk retry is supported today; the type
// is an enum so that future modes (e.g. dedup-aware mid-stream resume) can
// be added without silently changing behavior.
type StreamRetryWindow string

const (
	// StreamRetryWindowPreFirstChunk retries only while no content chunk
	// has been forwarded downstream. This is the only safe mode without a
	// deduplication mechanism.
	StreamRetryWindowPreFirstChunk StreamRetryWindow = "pre_first_chunk"
)

// StreamRetryPolicy governs bounded retry behavior for streaming requests
// that fail before any content chunk has been forwarded downstream.
//
// The policy is intentionally separate from pipeline.RetryPolicy (which
// covers non-streaming requests) because the failure classes and safety
// constraints are different: streaming retries must respect the idempotency
// window, use full jitter instead of half jitter to break h2 herd resets,
// and default to far fewer attempts.
type StreamRetryPolicy struct {
	// Enabled turns the retry loop on. Zero value is off.
	Enabled bool
	// MaxAttempts is total attempts including the initial request. Values
	// <1 are normalized to 1 (no retry). Zero falls back to the default.
	MaxAttempts int
	// InitialDelay is the base backoff before the first retry. Zero falls
	// back to the default.
	InitialDelay time.Duration
	// MaxDelay caps per-attempt backoff. Zero falls back to the default.
	MaxDelay time.Duration
	// Window controls which point in the stream lifecycle is eligible for
	// retry. Empty falls back to StreamRetryWindowPreFirstChunk.
	Window StreamRetryWindow
}

// DisabledStreamRetryPolicy returns a zero-value policy (retry off). Used
// as the BaseProvider default so callers never see nil.
func DisabledStreamRetryPolicy() StreamRetryPolicy {
	return StreamRetryPolicy{}
}

// Attempts returns the normalized number of attempts (>=1). Returns 1 when
// retry is disabled so callers can use it unconditionally in a for loop.
func (p StreamRetryPolicy) Attempts() int {
	if !p.Enabled {
		return 1
	}
	if p.MaxAttempts < 1 {
		return DefaultStreamRetryMaxAttempts
	}
	return p.MaxAttempts
}

// InitialDelayOrDefault returns the configured initial delay or the default.
func (p StreamRetryPolicy) InitialDelayOrDefault() time.Duration {
	if p.InitialDelay > 0 {
		return p.InitialDelay
	}
	return DefaultStreamRetryInitialDelay
}

// MaxDelayOrDefault returns the configured max delay or the default.
func (p StreamRetryPolicy) MaxDelayOrDefault() time.Duration {
	if p.MaxDelay > 0 {
		return p.MaxDelay
	}
	return DefaultStreamRetryMaxDelay
}

// BackoffFor computes the delay for the given attempt index (0-based) using
// full jitter: uniform random in [0, min(maxDelay, initialDelay * 2^attempt)].
// Full jitter (as opposed to equal or decorrelated jitter) is deliberate —
// when a single h2 connection reset kills ~100 streams, equal jitter still
// synchronizes the retries into narrow buckets; full jitter smears them.
func (p StreamRetryPolicy) BackoffFor(attempt int) time.Duration {
	initial := p.InitialDelayOrDefault()
	ceiling := p.MaxDelayOrDefault()
	if attempt < 0 {
		attempt = 0
	}
	// cap growth to avoid shift overflow on pathological inputs
	const maxShift = 30
	shift := attempt
	if shift > maxShift {
		shift = maxShift
	}
	delay := initial << shift
	if delay <= 0 || delay > ceiling {
		delay = ceiling
	}
	// full jitter: uniform in [0, delay]
	return time.Duration(cryptoRandFloat64() * float64(delay))
}

// IsRetryableStreamError returns true if the error looks like a transient
// streaming failure that is safe to retry from the pre-first-chunk window.
//
// This deliberately covers a narrower set than isRetryableError in retry.go:
// we want h2 stream resets, TCP resets, TLS close_notify races, and idle
// connection reuse failures — but never context cancellation, deadline, or
// application-layer parse errors.
func IsRetryableStreamError(err error) bool {
	if err == nil {
		return false
	}
	// Never retry caller cancellation or deadline.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if isRetryableError(err) {
		return true
	}
	// Go's http2 package uses string-matched errors in several places that
	// do not implement net.Error. These are the canonical transient cases
	// we want to catch for the gpt-5-pro failure class.
	msg := err.Error()
	for _, sub := range streamRetryErrorSubstrings {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}

// streamRetryErrorSubstrings lists substrings of errors that indicate a
// transient streaming failure worth retrying. Kept narrow on purpose: we
// want the h2 stream reset case and common TCP/TLS races, not every
// possible transport error.
var streamRetryErrorSubstrings = []string{
	"http2: response body closed", // client-side h2 stream teardown race
	"http2: server sent GOAWAY",   // server graceful shutdown mid-stream
	"stream error: stream ID",     // h2 RST_STREAM from server
	"unexpected EOF",              // TCP reset surfaced during read
	"connection reset by peer",
	"broken pipe",
	"use of closed network connection",
}

// IsRetryableStreamStatus returns true for HTTP status codes that are worth
// retrying on a streaming request. Mirrors isRetryableStatusCode but is
// named distinctly so future divergence (e.g. treating 409 as retryable
// for Responses API) does not mutate non-streaming semantics.
func IsRetryableStreamStatus(code int) bool {
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
