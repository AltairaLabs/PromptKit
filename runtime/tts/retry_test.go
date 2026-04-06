package tts

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mockTTSService implements Service for testing retry behavior.
type mockTTSService struct {
	calls   atomic.Int32
	results []mockResult
}

type mockResult struct {
	err error
}

func (m *mockTTSService) Name() string                    { return "mock" }
func (m *mockTTSService) SupportedVoices() []Voice        { return nil }
func (m *mockTTSService) SupportedFormats() []AudioFormat { return nil }

func (m *mockTTSService) Synthesize(_ context.Context, _ string, _ SynthesisConfig) (io.ReadCloser, error) {
	idx := int(m.calls.Add(1)) - 1
	if idx < len(m.results) && m.results[idx].err != nil {
		return nil, m.results[idx].err
	}
	return io.NopCloser(strings.NewReader("audio-data")), nil
}

func TestSynthesizeWithRetry_SuccessFirstAttempt(t *testing.T) {
	t.Parallel()
	svc := &mockTTSService{}
	result, err := SynthesizeWithRetry(context.Background(), svc, "hello", SynthesisConfig{}, DefaultRetryConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close()
	if svc.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", svc.calls.Load())
	}
}

func TestSynthesizeWithRetry_RetryOnRetryableError(t *testing.T) {
	t.Parallel()
	svc := &mockTTSService{
		results: []mockResult{
			{err: NewSynthesisError("mock", "429", "rate limited", nil, true)},
			{err: NewSynthesisError("mock", "503", "unavailable", nil, true)},
			{}, // success on 3rd attempt
		},
	}
	result, err := SynthesizeWithRetry(context.Background(), svc, "hello", SynthesisConfig{}, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close()
	if svc.calls.Load() != 3 {
		t.Errorf("calls = %d, want 3 (initial + 2 retries)", svc.calls.Load())
	}
}

func TestSynthesizeWithRetry_NoRetryOnNonRetryableError(t *testing.T) {
	t.Parallel()
	svc := &mockTTSService{
		results: []mockResult{
			{err: NewSynthesisError("mock", "401", "unauthorized", nil, false)},
		},
	}
	_, err := SynthesizeWithRetry(context.Background(), svc, "hello", SynthesisConfig{}, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if svc.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (no retry on non-retryable)", svc.calls.Load())
	}
}

func TestSynthesizeWithRetry_NoRetryOnPlainError(t *testing.T) {
	t.Parallel()
	svc := &mockTTSService{
		results: []mockResult{
			{err: errors.New("some non-synthesis error")},
		},
	}
	_, err := SynthesizeWithRetry(context.Background(), svc, "hello", SynthesisConfig{}, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if svc.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (plain errors are not retryable)", svc.calls.Load())
	}
}

func TestSynthesizeWithRetry_ExhaustsAttempts(t *testing.T) {
	t.Parallel()
	svc := &mockTTSService{
		results: []mockResult{
			{err: NewSynthesisError("mock", "503", "down", nil, true)},
			{err: NewSynthesisError("mock", "503", "still down", nil, true)},
			{err: NewSynthesisError("mock", "503", "really down", nil, true)},
		},
	}
	_, err := SynthesizeWithRetry(context.Background(), svc, "hello", SynthesisConfig{}, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if svc.calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", svc.calls.Load())
	}
}

func TestSynthesizeWithRetry_RespectsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	svc := &mockTTSService{
		results: []mockResult{
			{err: NewSynthesisError("mock", "503", "down", nil, true)},
		},
	}
	_, err := SynthesizeWithRetry(ctx, svc, "hello", SynthesisConfig{}, DefaultRetryConfig())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSynthesizeWithRetry_MaxAttemptsZeroMeansOneCall(t *testing.T) {
	t.Parallel()
	svc := &mockTTSService{}
	result, err := SynthesizeWithRetry(context.Background(), svc, "hello", SynthesisConfig{}, RetryConfig{
		MaxAttempts: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close()
	if svc.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", svc.calls.Load())
	}
}
