package stt

import (
	"context"
	"errors"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// Retry defaults for STT transcription.
const (
	defaultRetryMaxAttempts  = 3
	defaultRetryInitialDelay = 250 * time.Millisecond
	defaultRetryMaxDelay     = 2 * time.Second
	maxBackoffShift          = 30
)

// RetryConfig configures bounded retry for STT transcription calls.
// Defaults are on (unlike streaming retry) because STT calls are
// one-shot and idempotent — retry has no content-duplication risk,
// and the alternative is silently dropped speech.
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

// DefaultRetryConfig returns sensible defaults for STT retry.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  defaultRetryMaxAttempts,
		InitialDelay: defaultRetryInitialDelay,
		MaxDelay:     defaultRetryMaxDelay,
	}
}

// TranscribeWithRetry calls svc.Transcribe with bounded retry on
// transient errors. Only errors where TranscriptionError.Retryable is
// true are retried; all others are returned immediately. Uses full
// jitter backoff to avoid synchronized retries across concurrent
// callers.
//
//nolint:gocritic // hugeParam: config value is caller-owned and not modified
func TranscribeWithRetry(
	ctx context.Context,
	svc Service,
	audio []byte,
	config TranscriptionConfig,
	retry RetryConfig,
) (string, error) {
	maxAttempts := retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		result, err := svc.Transcribe(ctx, audio, config)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !isRetryable(err) || attempt >= maxAttempts-1 {
			break
		}

		delay := backoff(attempt, retry.InitialDelay, retry.MaxDelay)
		logger.Warn("STT transcription failed, retrying",
			"provider", svc.Name(),
			"attempt", attempt+1,
			"max_attempts", maxAttempts,
			"delay", delay.String(),
			"error", err,
		)

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}
	}
	return "", lastErr
}

// isRetryable checks if err is a TranscriptionError with Retryable set.
func isRetryable(err error) bool {
	var te *TranscriptionError
	if errors.As(err, &te) {
		return te.Retryable
	}
	return false
}

// backoff computes full-jitter delay for the given attempt.
func backoff(attempt int, initial, ceiling time.Duration) time.Duration {
	if initial <= 0 {
		initial = defaultRetryInitialDelay
	}
	if ceiling <= 0 {
		ceiling = defaultRetryMaxDelay
	}
	shift := uint(min(attempt, maxBackoffShift)) //nolint:gosec // bounded by min
	delay := initial << shift
	if delay <= 0 || delay > ceiling {
		delay = ceiling
	}
	jitter := time.Duration(time.Now().UnixNano()) % delay
	if jitter < 0 {
		jitter = -jitter
	}
	return jitter
}
