package providers

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrPayloadTooLarge is returned when a request payload exceeds the provider's
// configured maximum size (MaxRequestPayloadSize).
var ErrPayloadTooLarge = errors.New("request payload too large")

// ProviderHTTPError wraps a non-2xx HTTP response from a provider API.
// Use errors.As to extract the status code for classification.
type ProviderHTTPError struct {
	StatusCode int
	URL        string
	Body       string
	Provider   string
}

func (e *ProviderHTTPError) Error() string {
	return fmt.Sprintf("API request to %s failed with status %d: %s", e.URL, e.StatusCode, e.Body)
}

// ProviderTransportError wraps a connection-level failure (http2 reset,
// TCP reset, dial timeout, etc.). These are always transient.
type ProviderTransportError struct {
	Cause    error
	Provider string
}

func (e *ProviderTransportError) Error() string {
	return fmt.Sprintf("provider transport error: %v", e.Cause)
}

func (e *ProviderTransportError) Unwrap() error {
	return e.Cause
}

// IsTransient returns true if err represents a transient provider failure
// (retryable HTTP status or connection-level error). Uses errors.As to
// traverse wrapped error chains.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *ProviderHTTPError
	if errors.As(err, &httpErr) {
		return IsRetryableStreamStatus(httpErr.StatusCode)
	}
	var transportErr *ProviderTransportError
	return errors.As(err, &transportErr)
}

// ParsePlatformHTTPError extracts a human-readable error from platform-specific
// HTTP error responses (Bedrock, Vertex, Azure). These platforms return JSON
// like {"message":"..."} on HTTP 4xx/5xx. Falls back to raw body if parsing fails.
// When platform is empty, returns a generic error with the raw body.
func ParsePlatformHTTPError(platform string, statusCode int, body []byte) error {
	msg := string(body)
	var errResp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Message != "" {
		msg = errResp.Message
	}
	return &ProviderHTTPError{
		StatusCode: statusCode,
		URL:        platform,
		Body:       msg,
		Provider:   platform,
	}
}
