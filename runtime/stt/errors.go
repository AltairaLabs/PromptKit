package stt

import (
	"errors"
	"fmt"
)

// Common errors for STT services.
var (
	// ErrEmptyAudio is returned when audio data is empty.
	ErrEmptyAudio = errors.New("audio data is empty")

	// ErrRateLimited is returned when the provider rate limits requests.
	ErrRateLimited = errors.New("rate limited by provider")

	// ErrInvalidFormat is returned when the audio format is not supported.
	ErrInvalidFormat = errors.New("unsupported audio format")

	// ErrAudioTooShort is returned when audio is too short to transcribe.
	ErrAudioTooShort = errors.New("audio too short to transcribe")
)

// TranscriptionError represents an error during transcription.
type TranscriptionError struct {
	// Provider is the STT provider name.
	Provider string

	// Code is the provider-specific error code.
	Code string

	// Message is a human-readable error message.
	Message string

	// Cause is the underlying error, if any.
	Cause error

	// Retryable indicates whether the request can be retried.
	Retryable bool
}

// NewTranscriptionError creates a new TranscriptionError.
func NewTranscriptionError(provider, code, message string, cause error, retryable bool) *TranscriptionError {
	return &TranscriptionError{
		Provider:  provider,
		Code:      code,
		Message:   message,
		Cause:     cause,
		Retryable: retryable,
	}
}

// Error implements the error interface.
func (e *TranscriptionError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s transcription error [%s]: %s", e.Provider, e.Code, e.Message)
	}
	return fmt.Sprintf("%s transcription error: %s", e.Provider, e.Message)
}

// Unwrap returns the underlying error.
func (e *TranscriptionError) Unwrap() error {
	return e.Cause
}

// Is implements error matching for errors.Is.
func (e *TranscriptionError) Is(target error) bool {
	if e.Cause != nil && errors.Is(e.Cause, target) {
		return true
	}
	t, ok := target.(*TranscriptionError)
	if !ok {
		return false
	}
	return e.Provider == t.Provider && e.Code == t.Code
}
