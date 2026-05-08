package stt

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// mockSTTProvider implements base.STTProvider for testing retry behavior.
type mockSTTProvider struct {
	calls   atomic.Int32
	results []mockResult
}

type mockResult struct {
	text string
	err  error
}

func (m *mockSTTProvider) Name() string                        { return "mock" }
func (m *mockSTTProvider) Type() base.ProviderType             { return base.ProviderTypeSTT }
func (m *mockSTTProvider) Pricing() *base.PricingDescriptor    { return nil }
func (m *mockSTTProvider) Validate() error                     { return nil }
func (m *mockSTTProvider) Init(_ context.Context) error        { return nil }
func (m *mockSTTProvider) HealthCheck(_ context.Context) error { return nil }
func (m *mockSTTProvider) Close() error                        { return nil }

func (m *mockSTTProvider) Transcribe(_ context.Context, _ base.STTRequest) (base.STTResponse, error) {
	idx := int(m.calls.Add(1)) - 1
	if idx < len(m.results) {
		return base.STTResponse{Text: m.results[idx].text}, m.results[idx].err
	}
	return base.STTResponse{Text: "transcribed"}, nil
}

func TestTranscribeWithRetry_SuccessFirstAttempt(t *testing.T) {
	t.Parallel()
	svc := &mockSTTProvider{}
	result, err := TranscribeWithRetry(context.Background(), svc, base.STTRequest{Audio: []byte("audio")}, DefaultRetryConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "transcribed" {
		t.Errorf("result = %q, want %q", result.Text, "transcribed")
	}
	if svc.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", svc.calls.Load())
	}
}

func TestTranscribeWithRetry_RetryOnRetryableError(t *testing.T) {
	t.Parallel()
	svc := &mockSTTProvider{
		results: []mockResult{
			{err: NewTranscriptionError("mock", "429", "rate limited", nil, true)},
			{err: NewTranscriptionError("mock", "503", "unavailable", nil, true)},
			{text: "hello world"},
		},
	}
	result, err := TranscribeWithRetry(context.Background(), svc, base.STTRequest{Audio: []byte("audio")}, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "hello world" {
		t.Errorf("result = %q, want %q", result.Text, "hello world")
	}
	if svc.calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", svc.calls.Load())
	}
}

func TestTranscribeWithRetry_NoRetryOnNonRetryableError(t *testing.T) {
	t.Parallel()
	svc := &mockSTTProvider{
		results: []mockResult{
			{err: NewTranscriptionError("mock", "401", "unauthorized", nil, false)},
		},
	}
	_, err := TranscribeWithRetry(context.Background(), svc, base.STTRequest{Audio: []byte("audio")}, RetryConfig{
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
	svc := &mockSTTProvider{
		results: []mockResult{
			{err: errors.New("audio too short")},
		},
	}
	_, err := TranscribeWithRetry(context.Background(), svc, base.STTRequest{Audio: []byte("")}, RetryConfig{
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
	svc := &mockSTTProvider{
		results: []mockResult{
			{err: NewTranscriptionError("mock", "503", "down", nil, true)},
			{err: NewTranscriptionError("mock", "503", "still down", nil, true)},
			{err: NewTranscriptionError("mock", "503", "really down", nil, true)},
		},
	}
	_, err := TranscribeWithRetry(context.Background(), svc, base.STTRequest{Audio: []byte("audio")}, RetryConfig{
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

	svc := &mockSTTProvider{
		results: []mockResult{
			{err: NewTranscriptionError("mock", "503", "down", nil, true)},
		},
	}
	_, err := TranscribeWithRetry(ctx, svc, base.STTRequest{Audio: []byte("audio")}, DefaultRetryConfig())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
