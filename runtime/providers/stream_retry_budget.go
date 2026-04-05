package providers

import (
	"golang.org/x/time/rate"
)

// RetryBudget is a token bucket that governs how often streaming retries
// may actually re-dial the upstream. The initial attempt of each request
// is NOT gated by the budget — only retries consume tokens.
//
// Rationale: per-call bounded retry (policy.MaxAttempts) is not enough at
// scale. When a single HTTP/2 connection reset kills ~100 streams at once,
// naive bounded retry causes 100 simultaneous reconnect attempts, which
// amplifies the upstream problem instead of recovering from it. The budget
// caps the *rate* at which retries hit the upstream so one storm cannot
// saturate the provider's capacity for the entire runtime.
//
// Design: gRPC's "retry throttling" pattern, implemented with a standard
// golang.org/x/time/rate token bucket. Non-blocking acquire (fail-fast) —
// exhausted budgets return the original error immediately rather than
// stacking goroutines on a starved bucket.
//
// All methods are nil-safe: a nil *RetryBudget allows every retry
// (equivalent to unlimited budget). This lets callers use the budget
// unconditionally without guarding every call site.
type RetryBudget struct {
	limiter    *rate.Limiter
	ratePerSec float64
	burst      int
}

// NewRetryBudget creates a new token bucket sized for streaming retries.
// ratePerSec is the sustained refill rate; burst is the maximum number of
// tokens that can accumulate. Returns nil when either parameter is
// non-positive (unlimited budget).
//
// Typical sizing: start with rate=5/s, burst=10 and tune based on
// promptkit_stream_retries_total{outcome="budget_exhausted"}. These
// defaults are deliberately conservative — a healthy workload should
// almost never hit the budget, so high rejection counts are a signal
// that either retries are storming (upstream degraded) or the budget
// is undersized (bump it).
func NewRetryBudget(ratePerSec float64, burst int) *RetryBudget {
	if ratePerSec <= 0 || burst <= 0 {
		return nil
	}
	return &RetryBudget{
		limiter:    rate.NewLimiter(rate.Limit(ratePerSec), burst),
		ratePerSec: ratePerSec,
		burst:      burst,
	}
}

// TryAcquire attempts to take one token from the bucket without blocking.
// Returns true if a token was consumed (retry is permitted), false if the
// bucket is empty (retry must be rejected). A nil budget always returns
// true so provider code can call TryAcquire unconditionally.
func (b *RetryBudget) TryAcquire() bool {
	if b == nil {
		return true
	}
	return b.limiter.Allow()
}

// Available returns the current number of tokens in the bucket. Intended
// for the promptkit_stream_retry_budget_available gauge. A nil budget
// returns math-infinity-ish sentinel: since there is no concept of
// "available" for unlimited buckets, we return the burst capacity so
// gauges still report a meaningful finite number.
//
// Note: rate.Limiter.Tokens reflects state at the time of the call; it
// may drift between TryAcquire and Available under concurrent load.
// This is fine for observability — the gauge is a trailing indicator.
func (b *RetryBudget) Available() float64 {
	if b == nil {
		return 0
	}
	return b.limiter.Tokens()
}

// Burst returns the configured burst size. Used by callers that want to
// compute saturation ratios (available / burst). Returns 0 for nil.
func (b *RetryBudget) Burst() int {
	if b == nil {
		return 0
	}
	return b.burst
}

// RatePerSec returns the configured refill rate. Returns 0 for nil.
func (b *RetryBudget) RatePerSec() float64 {
	if b == nil {
		return 0
	}
	return b.ratePerSec
}
