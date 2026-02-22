package providers

import (
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
	if !strings.Contains(errMsg, "bedrock error") {
		t.Errorf("expected 'bedrock error' prefix, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "HTTP 400") {
		t.Errorf("expected HTTP status code in message, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "on-demand throughput") {
		t.Errorf("expected extracted message, got: %s", errMsg)
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
	if !strings.Contains(errMsg, "API error") {
		t.Errorf("expected generic 'API error' prefix for empty platform, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "HTTP 403") {
		t.Errorf("expected HTTP status code, got: %s", errMsg)
	}
}

func TestParsePlatformHTTPError_AzurePlatform(t *testing.T) {
	body := []byte(`{"message":"Rate limit exceeded"}`)
	err := ParsePlatformHTTPError("azure", 429, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "azure error") {
		t.Errorf("expected 'azure error' prefix, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "Rate limit exceeded") {
		t.Errorf("expected extracted message, got: %s", errMsg)
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
}
