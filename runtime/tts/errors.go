package tts

import "errors"

// Common TTS errors.
var (
	// ErrInvalidVoice is returned when the requested voice is not available.
	ErrInvalidVoice = errors.New("invalid or unsupported voice")

	// ErrInvalidFormat is returned when the requested format is not supported.
	ErrInvalidFormat = errors.New("invalid or unsupported audio format")

	// ErrEmptyText is returned when attempting to synthesize empty text.
	ErrEmptyText = errors.New("text cannot be empty")

	// ErrSynthesisFailed is returned when TTS synthesis fails.
	ErrSynthesisFailed = errors.New("speech synthesis failed")

	// ErrRateLimited is returned when API rate limits are exceeded.
	ErrRateLimited = errors.New("rate limit exceeded")

	// ErrQuotaExceeded is returned when account quota is exceeded.
	ErrQuotaExceeded = errors.New("quota exceeded")

	// ErrServiceUnavailable is returned when the TTS service is unavailable.
	ErrServiceUnavailable = errors.New("TTS service unavailable")
)

// SynthesisError provides detailed error information from TTS providers.
type SynthesisError struct {
	// Provider is the TTS provider that returned the error.
	Provider string

	// Code is the provider-specific error code.
	Code string

	// Message is the error message.
	Message string

	// Cause is the underlying error (if any).
	Cause error

	// Retryable indicates if the error is transient and retry may succeed.
	Retryable bool
}

// Error implements the error interface.
func (e *SynthesisError) Error() string {
	if e.Cause != nil {
		return e.Provider + ": " + e.Message + ": " + e.Cause.Error()
	}
	return e.Provider + ": " + e.Message
}

// Unwrap returns the underlying error.
func (e *SynthesisError) Unwrap() error {
	return e.Cause
}

// NewSynthesisError creates a new SynthesisError.
func NewSynthesisError(provider, code, message string, cause error, retryable bool) *SynthesisError {
	return &SynthesisError{
		Provider:  provider,
		Code:      code,
		Message:   message,
		Cause:     cause,
		Retryable: retryable,
	}
}
