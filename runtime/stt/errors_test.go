package stt

import (
	"errors"
	"testing"
)

func TestTranscriptionError_Error(t *testing.T) {
	cases := []struct {
		name string
		err  *TranscriptionError
		want string
	}{
		{"with a code", NewTranscriptionError("openai", "429", "slow down", nil, true),
			"openai transcription error [429]: slow down"},
		{"without a code", NewTranscriptionError("openai", "", "request failed", nil, true),
			"openai transcription error: request failed"},
	}
	for _, tc := range cases {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("%s: Error() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A TranscriptionError matches its cause, and another TranscriptionError with
// the same provider and code, and nothing else.
func TestTranscriptionError_Matching(t *testing.T) {
	limited := NewTranscriptionError("openai", "429", "slow down", ErrRateLimited, true)
	if !errors.Is(limited, ErrRateLimited) {
		t.Error("errors.Is(err, its cause) = false")
	}
	if errors.Unwrap(limited) != ErrRateLimited {
		t.Errorf("Unwrap = %v, want %v", errors.Unwrap(limited), ErrRateLimited)
	}
	if !errors.Is(limited, &TranscriptionError{Provider: "openai", Code: "429"}) {
		t.Error("errors.Is(err, same provider and code) = false")
	}
	if errors.Is(limited, &TranscriptionError{Provider: "openai", Code: "500"}) {
		t.Error("errors.Is(err, another code) = true")
	}
	if errors.Is(limited, ErrAudioTooShort) {
		t.Error("errors.Is(err, an unrelated sentinel) = true")
	}
	bare := NewTranscriptionError("openai", "", "request failed", nil, true)
	if errors.Is(bare, ErrRateLimited) {
		t.Error("errors.Is(err without cause, ErrRateLimited) = true")
	}
}
