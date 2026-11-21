// Package retry is the one attempt loop the runtime's retrying call sites
// share: provider HTTP requests (providers.DoWithRetry), A2A agent calls, TTS
// and STT requests, MCP connects and realtime WebSocket connects.
//
// Do runs an operation until it succeeds, fails in a way its policy does not
// retry, or runs out of attempts. Between attempts it waits the policy's
// jittered backoff, or the wait the failure itself asked for (a Retry-After),
// and stops at once when the context ends. Every retry decision is logged and
// reported to the process-wide Observer, so each call site emits the same
// retry metric.
package retry

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// Outcome names a retry decision reported to the Observer. The values are the
// label values of the provider retry metric.
type Outcome string

const (
	// OutcomeRetry is reported before each wait for another attempt.
	OutcomeRetry Outcome = "retry"
	// OutcomeSuccess is reported when an attempt after the first succeeds.
	OutcomeSuccess Outcome = "success"
	// OutcomeExhausted is reported when the last attempt fails with an error
	// the policy would otherwise have retried.
	OutcomeExhausted Outcome = "exhausted"
)

// Observer receives every retry decision Do makes, with the policy's Name.
type Observer func(name string, outcome Outcome)

var observer atomic.Pointer[Observer]

// SetObserver installs the process-wide Observer; nil removes it.
func SetObserver(o Observer) {
	if o == nil {
		observer.Store(nil)
		return
	}
	observer.Store(&o)
}

func observe(name string, outcome Outcome) {
	if o := observer.Load(); o != nil {
		(*o)(name, outcome)
	}
}

// Backoff computes the wait before each retry: Initial grown by Multiplier per
// attempt and capped at Max, then scaled by a uniform random factor in
// [JitterMin, JitterMax] and capped at Max again, so no wait exceeds Max.
type Backoff struct {
	// Initial is the wait before the first retry, before jitter.
	Initial time.Duration
	// Max caps every wait. Zero or negative means no cap.
	Max time.Duration
	// Multiplier grows the wait per attempt. Zero means 2 (exponential);
	// 1 keeps it fixed.
	Multiplier float64
	// JitterMin and JitterMax bound the random factor the wait is scaled by.
	// Both zero means no jitter.
	JitterMin, JitterMax float64
}

const defaultMultiplier = 2

// Exponential is the Backoff DoWithRetry has always used: initial doubled per
// attempt, lengthened by up to half at random, and never more than ceiling.
func Exponential(initial, ceiling time.Duration) Backoff {
	return Backoff{Initial: initial, Max: ceiling, JitterMin: 1, JitterMax: exponentialJitterMax}
}

// exponentialJitterMax lengthens an Exponential wait by up to half.
const exponentialJitterMax = 1.5

// FullJitter is the exponential Backoff whose waits are uniform in
// [0, min(ceiling, initial*2^attempt)): it spreads callers that failed together
// the most.
func FullJitter(initial, ceiling time.Duration) Backoff {
	return Backoff{Initial: initial, Max: ceiling, JitterMax: 1}
}

// Delay returns the wait after the given failed attempt (0-based).
func (b Backoff) Delay(attempt int) time.Duration {
	multiplier := b.Multiplier
	if multiplier == 0 {
		multiplier = defaultMultiplier
	}
	// Grow and cap in float64: a large attempt overflows time.Duration.
	delay := float64(b.Initial) * math.Pow(multiplier, float64(max(attempt, 0)))
	delay = b.capped(delay)
	if b.JitterMin != 0 || b.JitterMax != 0 {
		delay *= b.JitterMin + randFloat64()*(b.JitterMax-b.JitterMin)
	}
	return time.Duration(max(b.capped(delay), 0))
}

func (b Backoff) capped(delay float64) float64 {
	if b.Max > 0 && delay > float64(b.Max) {
		return float64(b.Max)
	}
	return delay
}

// FullJitterPolicy is the policy of an idempotent one-shot call (a TTS or STT
// request): attempts in all, FullJitter waits from initial up to ceiling, and
// a failure's own wait capped at ceiling. provider is logged and, prefixed
// with kind, names the site.
func FullJitterPolicy(
	kind, provider string, attempts int, initial, ceiling time.Duration,
	classify func(error) (bool, time.Duration),
) Policy {
	return Policy{
		Name:          kind + "/" + provider,
		MaxAttempts:   attempts,
		Backoff:       FullJitter(initial, ceiling),
		MaxRetryAfter: ceiling,
		Classify:      classify,
		LogAttrs:      []any{"provider", provider},
	}
}

// Policy configures Do.
type Policy struct {
	// Name identifies the call site in logs and to the Observer.
	Name string
	// MaxAttempts is the number of attempts in all, the first included.
	// Values below 1 mean 1.
	MaxAttempts int
	// Backoff computes the wait between attempts.
	Backoff Backoff
	// Classify reports whether a failure is worth another attempt and how long
	// the failure asked to wait first (zero: use Backoff). Nil retries every
	// failure but the context's own.
	Classify func(err error) (retry bool, after time.Duration)
	// MaxRetryAfter caps the wait a failure asks for. Zero means no cap.
	MaxRetryAfter time.Duration
	// LogAttrs are added to the retry log lines.
	LogAttrs []any
}

func (p *Policy) classify(err error) (retry bool, after time.Duration) {
	if p.Classify != nil {
		return p.Classify(err)
	}
	return !IsContextError(err), 0
}

// IsContextError reports whether err is a context's cancellation or deadline.
func IsContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// Do calls op until it succeeds, fails with an error the policy does not
// retry, or has made MaxAttempts attempts, and returns the last attempt's
// value and error. It makes no attempt once ctx has ended, and returns
// ctx.Err() when ctx ends before or while it waits to retry.
func Do[T any](ctx context.Context, p Policy, op func(ctx context.Context) (T, error)) (T, error) {
	attempts := max(p.MaxAttempts, 1)
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			var zero T
			return zero, err
		}
		v, err := op(ctx)
		if err == nil {
			if attempt > 0 {
				observe(p.Name, OutcomeSuccess)
			}
			return v, nil
		}
		retry, after := p.classify(err)
		if !retry {
			logger.Debug("request failed (non-retryable)", p.logAttrs("error", err)...)
			return v, err
		}
		if attempt >= attempts-1 {
			observe(p.Name, OutcomeExhausted)
			logger.Warn("request failed after all retries", p.logAttrs("attempts", attempts, "error", err)...)
			return v, err
		}
		delay := p.Wait(attempt, after)
		observe(p.Name, OutcomeRetry)
		logger.Warn("retrying request", p.logAttrs(
			"attempt", attempt+1, "max_attempts", attempts, "delay", delay.String(), "error", err)...)
		if err := Sleep(ctx, delay); err != nil {
			var zero T
			return zero, err
		}
	}
}

// Wait is how long Do waits after the given failed attempt (0-based): the wait
// the failure asked for, capped at MaxRetryAfter, or else the backoff.
func (p *Policy) Wait(attempt int, after time.Duration) time.Duration {
	if after <= 0 {
		return p.Backoff.Delay(attempt)
	}
	if p.MaxRetryAfter > 0 && after > p.MaxRetryAfter {
		return p.MaxRetryAfter
	}
	return after
}

func (p *Policy) logAttrs(kv ...any) []any {
	attrs := append([]any{"name", p.Name}, p.LogAttrs...)
	return append(attrs, kv...)
}

// Sleep waits d, returning ctx.Err() early if ctx ends first.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ParseRetryAfter returns the wait a Retry-After header asks for, in
// delta-seconds or HTTP-date form, or zero when it is absent, unparsable or
// already past.
func ParseRetryAfter(h http.Header) time.Duration {
	val := h.Get("Retry-After")
	if val == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(val); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		if delay := time.Until(t); delay > 0 {
			return delay
		}
	}
	return 0
}

const (
	// float64MantissaBits is the width of a float64 mantissa.
	float64MantissaBits = 53
	uint64Bits          = 64
)

// randFloat64 returns a uniform float64 in [0, 1) from crypto/rand, or 0 if
// it fails.
func randFloat64() float64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0
	}
	n := binary.BigEndian.Uint64(buf[:]) >> (uint64Bits - float64MantissaBits)
	return float64(n) / float64(uint64(1)<<float64MantissaBits)
}
