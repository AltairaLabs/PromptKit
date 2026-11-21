package tts

import (
	"cmp"
	"context"
	"errors"
	"io"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/internal/retry"
)

// Retry defaults for TTS synthesis.
const (
	defaultRetryMaxAttempts  = 3
	defaultRetryInitialDelay = 250 * time.Millisecond
	defaultRetryMaxDelay     = 2 * time.Second
)

// RetryConfig configures bounded retry for TTS synthesis calls.
// TTS calls are one-shot and idempotent, so retry has no
// content-duplication risk.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts including the initial
	// call. 3 means "initial + up to 2 retries". Values < 1 are
	// treated as 1 (no retry).
	MaxAttempts int
	// InitialDelay is the base backoff before the first retry.
	InitialDelay time.Duration
	// MaxDelay caps the per-attempt backoff.
	MaxDelay time.Duration
}

// DefaultRetryConfig returns sensible defaults for TTS retry.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  defaultRetryMaxAttempts,
		InitialDelay: defaultRetryInitialDelay,
		MaxDelay:     defaultRetryMaxDelay,
	}
}

// SynthesizeWithRetry calls svc.Synthesize with bounded retry on
// transient errors. Only errors where SynthesisError.Retryable is
// true are retried; all others are returned immediately. Uses full
// jitter backoff to avoid synchronized retries across concurrent
// callers; a SynthesisError's RetryAfter, capped at MaxDelay, replaces it.
func SynthesizeWithRetry(
	ctx context.Context,
	svc Service,
	text string,
	config SynthesisConfig,
	cfg RetryConfig,
) (io.ReadCloser, error) {
	policy := retry.FullJitterPolicy("tts", svc.Name(), cfg.MaxAttempts,
		cmp.Or(max(cfg.InitialDelay, 0), defaultRetryInitialDelay),
		cmp.Or(max(cfg.MaxDelay, 0), defaultRetryMaxDelay),
		classifySynthesisError)
	result, err := retry.Do(ctx, policy, func(ctx context.Context) (io.ReadCloser, error) {
		return svc.Synthesize(ctx, text, config)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// classifySynthesisError retries a SynthesisError marked Retryable, after the
// RetryAfter it carries.
func classifySynthesisError(err error) (bool, time.Duration) {
	var se *SynthesisError
	if errors.As(err, &se) {
		return se.Retryable, se.RetryAfter
	}
	return false, 0
}
