package stt

import (
	"cmp"
	"context"
	"errors"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/internal/retry"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// Retry defaults for STT transcription.
const (
	defaultRetryMaxAttempts  = 3
	defaultRetryInitialDelay = 250 * time.Millisecond
	defaultRetryMaxDelay     = 2 * time.Second
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
// callers. A TranscriptionError's RetryAfter replaces the backoff; one past
// MaxDelay is not waited, and the error is returned at once.
func TranscribeWithRetry(
	ctx context.Context,
	svc base.STTProvider,
	req base.STTRequest,
	cfg RetryConfig,
) (base.STTResponse, error) {
	policy := retry.FullJitterPolicy("stt", svc.Name(), cfg.MaxAttempts,
		cmp.Or(max(cfg.InitialDelay, 0), defaultRetryInitialDelay),
		cmp.Or(max(cfg.MaxDelay, 0), defaultRetryMaxDelay),
		classifyTranscriptionError)
	result, err := retry.Do(ctx, policy, func(ctx context.Context) (base.STTResponse, error) {
		return svc.Transcribe(ctx, req)
	})
	if err != nil {
		return base.STTResponse{}, err
	}
	return result, nil
}

// classifyTranscriptionError retries a TranscriptionError marked Retryable,
// after the RetryAfter it carries.
func classifyTranscriptionError(err error) (bool, time.Duration) {
	var te *TranscriptionError
	if errors.As(err, &te) {
		return te.Retryable, te.RetryAfter
	}
	return false, 0
}
