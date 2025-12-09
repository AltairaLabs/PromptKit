package tts

import (
	"errors"
	"testing"
)

func TestSynthesisError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *SynthesisError
		want string
	}{
		{
			name: "with cause",
			err: &SynthesisError{
				Provider: "openai",
				Code:     "rate_limit",
				Message:  "rate limited",
				Cause:    ErrRateLimited,
			},
			want: "openai: rate limited: rate limit exceeded",
		},
		{
			name: "without cause",
			err: &SynthesisError{
				Provider: "elevenlabs",
				Code:     "invalid_voice",
				Message:  "voice not found",
			},
			want: "elevenlabs: voice not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("SynthesisError.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSynthesisError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := &SynthesisError{
		Provider: "test",
		Message:  "test error",
		Cause:    cause,
	}

	if err.Unwrap() != cause {
		t.Errorf("SynthesisError.Unwrap() = %v, want %v", err.Unwrap(), cause)
	}
}

func TestNewSynthesisError(t *testing.T) {
	cause := errors.New("test cause")
	err := NewSynthesisError("openai", "500", "internal error", cause, true)

	if err.Provider != "openai" {
		t.Errorf("Provider = %v, want openai", err.Provider)
	}

	if err.Code != "500" {
		t.Errorf("Code = %v, want 500", err.Code)
	}

	if err.Message != "internal error" {
		t.Errorf("Message = %v, want internal error", err.Message)
	}

	if err.Cause != cause {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}

	if !err.Retryable {
		t.Error("Retryable = false, want true")
	}
}

func TestCommonErrors(t *testing.T) {
	// Just verify the errors are defined
	if ErrInvalidVoice == nil {
		t.Error("ErrInvalidVoice is nil")
	}
	if ErrInvalidFormat == nil {
		t.Error("ErrInvalidFormat is nil")
	}
	if ErrEmptyText == nil {
		t.Error("ErrEmptyText is nil")
	}
	if ErrSynthesisFailed == nil {
		t.Error("ErrSynthesisFailed is nil")
	}
	if ErrRateLimited == nil {
		t.Error("ErrRateLimited is nil")
	}
	if ErrQuotaExceeded == nil {
		t.Error("ErrQuotaExceeded is nil")
	}
	if ErrServiceUnavailable == nil {
		t.Error("ErrServiceUnavailable is nil")
	}
}
