package gemini

import (
	"errors"
	"fmt"
)

// Common errors for Gemini streaming
var (
	// ErrInvalidAudioFormat indicates audio format doesn't meet Gemini requirements
	ErrInvalidAudioFormat = errors.New("invalid audio format")

	// ErrRateLimitExceeded indicates too many requests
	ErrRateLimitExceeded = errors.New("rate limit exceeded")

	// ErrAuthenticationFailed indicates invalid API key
	ErrAuthenticationFailed = errors.New("authentication failed")

	// ErrServiceUnavailable indicates temporary service issue
	ErrServiceUnavailable = errors.New("service unavailable")

	// ErrPolicyViolation indicates content policy violation
	ErrPolicyViolation = errors.New("policy violation")

	// ErrInvalidRequest indicates malformed request
	ErrInvalidRequest = errors.New("invalid request")
)

// GeminiAPIError represents an error from the Gemini API
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// Error implements the error interface
func (e *APIError) Error() string {
	return fmt.Sprintf("gemini api error (code %d, status %s): %s", e.Code, e.Status, e.Message)
}

// IsRetryable returns true if the error can be retried
func (e *APIError) IsRetryable() bool {
	// Retryable errors: temporary failures, rate limits, service unavailable
	switch e.Code {
	case 429: // Rate limit
		return true
	case 500: // Internal server error
		return true
	case 503: // Service unavailable
		return true
	default:
		return false
	}
}

// IsAuthError returns true if the error is authentication-related
func (e *APIError) IsAuthError() bool {
	return e.Code == 401
}

// IsPolicyViolation returns true if the error is a content policy violation
func (e *APIError) IsPolicyViolation() bool {
	return e.Code == 400 && e.Status == "POLICY_VIOLATION"
}

// ErrorResponse wraps a GeminiAPIError in a message format
type ErrorResponse struct {
	Error *APIError `json:"error"`
}

// ClassifyError converts an API error code to a standard error
func ClassifyError(apiErr *APIError) error {
	if apiErr == nil {
		return nil
	}

	switch apiErr.Code {
	case 400:
		if apiErr.IsPolicyViolation() {
			return fmt.Errorf("policy violation: %w - %s", ErrPolicyViolation, apiErr.Message)
		}
		return fmt.Errorf("invalid request: %w - %s", ErrInvalidRequest, apiErr.Message)
	case 401:
		return fmt.Errorf("authentication failed: %w - %s", ErrAuthenticationFailed, apiErr.Message)
	case 429:
		return fmt.Errorf("rate limit exceeded: %w - %s", ErrRateLimitExceeded, apiErr.Message)
	case 503:
		return fmt.Errorf("service unavailable: %w - %s", ErrServiceUnavailable, apiErr.Message)
	default:
		return apiErr
	}
}

// RecoveryStrategy defines how to handle different error types
type RecoveryStrategy int

const (
	// RecoveryRetry indicates the operation should be retried
	RecoveryRetry RecoveryStrategy = iota

	// RecoveryFailFast indicates the operation should fail immediately
	RecoveryFailFast

	// RecoveryGracefulDegradation indicates fallback to a simpler mode
	RecoveryGracefulDegradation

	// RecoveryWaitAndRetry indicates retry after a delay
	RecoveryWaitAndRetry
)

// DetermineRecoveryStrategy determines how to handle an error
func DetermineRecoveryStrategy(err error) RecoveryStrategy {
	if err == nil {
		return RecoveryRetry
	}

	// Check for known error types
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.IsRetryable() {
			return RecoveryWaitAndRetry
		}
		if apiErr.IsAuthError() || apiErr.IsPolicyViolation() {
			return RecoveryFailFast
		}
	}

	// Check for standard errors
	if errors.Is(err, ErrAuthenticationFailed) {
		return RecoveryFailFast
	}
	if errors.Is(err, ErrPolicyViolation) {
		return RecoveryFailFast
	}
	if errors.Is(err, ErrRateLimitExceeded) {
		return RecoveryWaitAndRetry
	}
	if errors.Is(err, ErrServiceUnavailable) {
		return RecoveryWaitAndRetry
	}

	// Unknown errors - try graceful degradation
	return RecoveryGracefulDegradation
}

// SafetyRating represents content safety assessment
type SafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

// PromptFeedback contains safety ratings and block reason
type PromptFeedback struct {
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
	BlockReason   string         `json:"blockReason,omitempty"`
}

// IsBlocked returns true if content was blocked by safety filters
func (f *PromptFeedback) IsBlocked() bool {
	return f.BlockReason != ""
}

// GetBlockReason returns a human-readable block reason
func (f *PromptFeedback) GetBlockReason() string {
	if f.BlockReason == "" {
		return "none"
	}
	return f.BlockReason
}
