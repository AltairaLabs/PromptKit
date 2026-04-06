package stt

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// mockSTTService implements Service for testing retry behavior.
type mockSTTService struct {
	calls   atomic.Int32
	results []mockResult
}

type mockResult struct {
	text string
	err  error
}

func (m *mockSTTService) Name() string               { return "mock" }
func (m *mockSTTService) SupportedFormats() []string { return nil }

func (m *mockSTTService) Transcribe(_ context.Context, _ []byte, _ TranscriptionConfig) (string, error) {
	idx := int(m.calls.Add(1)) - 1
	if idx < len(m.results) {
		return m.results[idx].text, m.results[idx].err
	}
	return "transcribed", nil
}

func TestTranscribeWithRetry_SuccessFirstAttempt(t *testing.T) {
	t.Parallel()
	svc := &mockSTTService{}
	result, err := TranscribeWithRetry(context.Background(), svc, []byte("audio"), TranscriptionConfig{}, DefaultRetryConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "transcribed" {
		t.Errorf("result = %q, want %q", result, "transcribed")
	}
	if svc.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", svc.calls.Load())
	}
}

func TestTranscribeWithRetry_RetryOnRetryableError(t *testing.T) {
	t.Parallel()
	svc := &mockSTTService{
		results: []mockResult{
			{err: NewTranscriptionError("mock", "429", "rate limited", nil, true)},
			{err: NewTranscriptionError("mock", "503", "unavailable", nil, true)},
			{text: "hello world"},
		},
	}
	result, err := TranscribeWithRetry(context.Background(), svc, []byte("audio"), TranscriptionConfig{}, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("result = %q, want %q", result, "hello world")
	}
	if svc.calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", svc.calls.Load())
	}
}

func TestTranscribeWithRetry_NoRetryOnNonRetryableError(t *testing.T) {
	t.Parallel()
	svc := &mockSTTService{
		results: []mockResult{
			{err: NewTranscriptionError("mock", "401", "unauthorized", nil, false)},
		},
	}
	_, err := TranscribeWithRetry(context.Background(), svc, []byte("audio"), TranscriptionConfig{}, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if svc.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", svc.calls.Load())
	}
}

func TestTranscribeWithRetry_NoRetryOnAudioTooShort(t *testing.T) {
	t.Parallel()
	svc := &mockSTTService{
		results: []mockResult{
			{err: errors.New("audio too short")},
		},
	}
	_, err := TranscribeWithRetry(context.Background(), svc, []byte(""), TranscriptionConfig{}, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if svc.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (plain errors not retryable)", svc.calls.Load())
	}
}

func TestTranscribeWithRetry_ExhaustsAttempts(t *testing.T) {
	t.Parallel()
	svc := &mockSTTService{
		results: []mockResult{
			{err: NewTranscriptionError("mock", "503", "down", nil, true)},
			{err: NewTranscriptionError("mock", "503", "still down", nil, true)},
			{err: NewTranscriptionError("mock", "503", "really down", nil, true)},
		},
	}
	_, err := TranscribeWithRetry(context.Background(), svc, []byte("audio"), TranscriptionConfig{}, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if svc.calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", svc.calls.Load())
	}
}

func TestTranscribeWithRetry_RespectsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc := &mockSTTService{
		results: []mockResult{
			{err: NewTranscriptionError("mock", "503", "down", nil, true)},
		},
	}
	_, err := TranscribeWithRetry(ctx, svc, []byte("audio"), TranscriptionConfig{}, DefaultRetryConfig())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
