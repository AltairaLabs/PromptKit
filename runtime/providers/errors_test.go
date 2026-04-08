package providers

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParsePlatformHTTPError_BedrockJSON(t *testing.T) {
	body := []byte(`{"message":"Invocation of model ID anthropic.claude-3-5-haiku with on-demand throughput isn't supported."}`)
	err := ParsePlatformHTTPError("bedrock", 400, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "bedrock") {
		t.Errorf("expected 'bedrock' in error, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "400") {
		t.Errorf("expected status code in message, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "on-demand throughput") {
		t.Errorf("expected extracted message, got: %s", errMsg)
	}
	var httpErr *ProviderHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatal("expected ProviderHTTPError, got different type")
	}
	if httpErr.StatusCode != 400 {
		t.Errorf("expected StatusCode 400, got %d", httpErr.StatusCode)
	}
}

func TestParsePlatformHTTPError_RawFallback(t *testing.T) {
	body := []byte(`not json`)
	err := ParsePlatformHTTPError("bedrock", 500, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not json") {
		t.Errorf("should fall back to raw body, got: %s", err.Error())
	}
}

func TestParsePlatformHTTPError_EmptyPlatform(t *testing.T) {
	body := []byte(`{"error":"something went wrong"}`)
	err := ParsePlatformHTTPError("", 403, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "403") {
		t.Errorf("expected status code in message, got: %s", errMsg)
	}
	var httpErr *ProviderHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatal("expected ProviderHTTPError")
	}
	if httpErr.StatusCode != 403 {
		t.Errorf("expected StatusCode 403, got %d", httpErr.StatusCode)
	}
}

func TestParsePlatformHTTPError_AzurePlatform(t *testing.T) {
	body := []byte(`{"message":"Rate limit exceeded"}`)
	err := ParsePlatformHTTPError("azure", 429, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "azure") {
		t.Errorf("expected 'azure' in error, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "Rate limit exceeded") {
		t.Errorf("expected extracted message, got: %s", errMsg)
	}
	var httpErr *ProviderHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatal("expected ProviderHTTPError")
	}
	if httpErr.StatusCode != 429 {
		t.Errorf("expected StatusCode 429, got %d", httpErr.StatusCode)
	}
}

func TestErrPayloadTooLarge_IsSentinel(t *testing.T) {
	// Verify ErrPayloadTooLarge works as a sentinel error with fmt.Errorf wrapping.
	wrapped := fmt.Errorf("%w: payload size 200 bytes exceeds maximum 100 bytes", ErrPayloadTooLarge)
	if !errors.Is(wrapped, ErrPayloadTooLarge) {
		t.Error("Expected errors.Is to match ErrPayloadTooLarge through wrapping")
	}
	if !strings.Contains(wrapped.Error(), "request payload too large") {
		t.Errorf("Expected wrapped error to contain sentinel message, got: %s", wrapped.Error())
	}
}

func TestParsePlatformHTTPError_EmptyMessage(t *testing.T) {
	body := []byte(`{"message":""}`)
	err := ParsePlatformHTTPError("vertex", 500, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Empty message should fall back to raw body
	if !strings.Contains(err.Error(), `{"message":""}`) {
		t.Errorf("expected raw body fallback for empty message, got: %s", err.Error())
	}
	var httpErr *ProviderHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatal("expected ProviderHTTPError")
	}
	if httpErr.StatusCode != 500 {
		t.Errorf("expected StatusCode 500, got %d", httpErr.StatusCode)
	}
}

// --- ProviderHTTPError ---

func TestProviderHTTPError_Error(t *testing.T) {
	t.Parallel()
	err := &ProviderHTTPError{
		StatusCode: 503,
		URL:        "https://api.openai.com/v1/chat/completions",
		Body:       `{"error":"overloaded"}`,
		Provider:   "openai",
	}
	want := `API request to https://api.openai.com/v1/chat/completions failed with status 503: {"error":"overloaded"}`
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestProviderHTTPError_ErrorsAs(t *testing.T) {
	t.Parallel()
	original := &ProviderHTTPError{StatusCode: 429, URL: "https://api.example.com", Provider: "test"}
	wrapped := fmt.Errorf("provider stream failed: %w", original)

	var httpErr *ProviderHTTPError
	if !errors.As(wrapped, &httpErr) {
		t.Fatal("errors.As failed to find ProviderHTTPError through wrapping")
	}
	if httpErr.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", httpErr.StatusCode)
	}
}

// --- ProviderTransportError ---

func TestProviderTransportError_Error(t *testing.T) {
	t.Parallel()
	cause := errors.New("http2: response body closed")
	err := &ProviderTransportError{Cause: cause, Provider: "gemini"}
	if err.Error() != "provider transport error: http2: response body closed" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestProviderTransportError_Unwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("connection reset by peer")
	err := &ProviderTransportError{Cause: cause, Provider: "openai"}
	if !errors.Is(err, cause) {
		t.Error("errors.Is failed to find cause through Unwrap")
	}
}

func TestProviderTransportError_ErrorsAs(t *testing.T) {
	t.Parallel()
	cause := errors.New("unexpected EOF")
	original := &ProviderTransportError{Cause: cause, Provider: "test"}
	wrapped := fmt.Errorf("failed to send request: %w", original)

	var transportErr *ProviderTransportError
	if !errors.As(wrapped, &transportErr) {
		t.Fatal("errors.As failed to find ProviderTransportError through wrapping")
	}
	if transportErr.Provider != "test" {
		t.Errorf("Provider = %q, want test", transportErr.Provider)
	}
}

// --- IsTransient ---

func TestIsTransient_HTTPError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code int
		want bool
	}{
		{200, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true},
		{500, false},
		{502, true},
		{503, true},
		{504, true},
	}
	for _, tt := range tests {
		err := &ProviderHTTPError{StatusCode: tt.code}
		if got := IsTransient(err); got != tt.want {
			t.Errorf("IsTransient(HTTP %d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestIsTransient_TransportError(t *testing.T) {
	t.Parallel()
	err := &ProviderTransportError{Cause: errors.New("connection reset")}
	if !IsTransient(err) {
		t.Error("IsTransient(ProviderTransportError) = false, want true")
	}
}

func TestIsTransient_WrappedErrors(t *testing.T) {
	t.Parallel()
	httpErr := fmt.Errorf("outer: %w", &ProviderHTTPError{StatusCode: 503})
	if !IsTransient(httpErr) {
		t.Error("IsTransient failed through wrapping for HTTP 503")
	}

	transportErr := fmt.Errorf("outer: %w", &ProviderTransportError{Cause: errors.New("EOF")})
	if !IsTransient(transportErr) {
		t.Error("IsTransient failed through wrapping for transport error")
	}
}

func TestIsTransient_NonProviderError(t *testing.T) {
	t.Parallel()
	err := errors.New("validation failed: missing CAPABILITY_PASS")
	if IsTransient(err) {
		t.Error("IsTransient(non-provider error) = true, want false")
	}
}

func TestIsTransient_Nil(t *testing.T) {
	t.Parallel()
	if IsTransient(nil) {
		t.Error("IsTransient(nil) = true, want false")
	}
}
