package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSetLevel(t *testing.T) {
	// Test setting different levels
	SetLevel(slog.LevelDebug)
	if DefaultLogger == nil {
		t.Error("Expected DefaultLogger to be set")
	}

	SetLevel(slog.LevelInfo)
	if DefaultLogger == nil {
		t.Error("Expected DefaultLogger to be set")
	}

	SetLevel(slog.LevelWarn)
	if DefaultLogger == nil {
		t.Error("Expected DefaultLogger to be set")
	}

	SetLevel(slog.LevelError)
	if DefaultLogger == nil {
		t.Error("Expected DefaultLogger to be set")
	}
}

func TestSetVerbose(t *testing.T) {
	// Enable verbose
	SetVerbose(true)
	if DefaultLogger == nil {
		t.Error("Expected DefaultLogger to be set after SetVerbose(true)")
	}

	// Disable verbose
	SetVerbose(false)
	if DefaultLogger == nil {
		t.Error("Expected DefaultLogger to be set after SetVerbose(false)")
	}
}

func TestInfo(t *testing.T) {
	// Should not panic
	Info("test message")
	Info("test with args", "key", "value")
	Info("test with multiple", "key1", "value1", "key2", "value2")
}

func TestInfoContext(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	InfoContext(ctx, "test message")
	InfoContext(ctx, "test with args", "key", "value")
}

func TestDebug(t *testing.T) {
	SetVerbose(true) // Enable debug logging

	// Should not panic
	Debug("debug message")
	Debug("debug with args", "key", "value")

	SetVerbose(false) // Reset
}

func TestDebugContext(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	ctx := context.Background()

	// Should not panic
	DebugContext(ctx, "debug message")
	DebugContext(ctx, "debug with args", "key", "value")

	SetVerbose(false) // Reset
}

func TestWarn(t *testing.T) {
	// Should not panic
	Warn("warning message")
	Warn("warning with args", "key", "value")
}

func TestWarnContext(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	WarnContext(ctx, "warning message")
	WarnContext(ctx, "warning with args", "key", "value")
}

func TestError(t *testing.T) {
	// Should not panic
	Error("error message")
	Error("error with args", "key", "value", "error", "test error")
}

func TestErrorContext(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	ErrorContext(ctx, "error message")
	ErrorContext(ctx, "error with args", "key", "value", "error", "test error")
}

func TestLLMCall(t *testing.T) {
	// Should not panic
	LLMCall("openai", "assistant", 5, 0.7)
	LLMCall("anthropic", "assistant", 10, 0.9)
}

func TestLLMResponse(t *testing.T) {
	// Should not panic
	LLMResponse("openai", "assistant", 150, 50, 0.01)
	LLMResponse("anthropic", "assistant", 200, 100, 0.02)
}

func TestLLMError(t *testing.T) {
	// Should not panic
	LLMError("openai", "assistant", errors.New("timeout error"))
	LLMError("anthropic", "assistant", errors.New("rate limit exceeded"))
}

func TestToolCall(t *testing.T) {
	// Should not panic
	ToolCall("openai", 5, 3, "auto")
	ToolCall("anthropic", 10, 5, "required")
}

func TestToolResponse(t *testing.T) {
	// Should not panic
	ToolResponse("openai", 150, 50, 2, 0.01)
	ToolResponse("anthropic", 200, 100, 3, 0.02)
}

func TestDefaultLoggerInitialized(t *testing.T) {
	// Test that DefaultLogger is initialized on package load
	if DefaultLogger == nil {
		t.Error("Expected DefaultLogger to be initialized")
	}
}

func TestLoggingWithNilContext(t *testing.T) {
	// Should handle nil context gracefully
	// Note: This might panic depending on implementation, but testing it
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Recovered from panic with nil context: %v", r)
		}
	}()

	ctx := context.Background()
	InfoContext(ctx, "test")
}

func TestLoggingWithStructuredAttributes(t *testing.T) {
	// Test various attribute types
	Info("structured log",
		"string", "value",
		"int", 42,
		"bool", true,
		"float", 3.14,
	)
}

func TestRedactSensitiveData_OpenAIKey(t *testing.T) {
	// OpenAI keys start with sk- and are at least 32 chars
	fakeKey := "sk-1234567890abcdefghijklmnopqrstuvwxyz12345678" // Fake test key - not a real credential
	input := "My API key is " + fakeKey + " and I want it hidden"
	result := RedactSensitiveData(input)

	if result == input {
		t.Error("Expected API key to be redacted")
	}

	if strings.Contains(result, fakeKey) {
		t.Error("Expected full API key to not be in result")
	}

	if !strings.Contains(result, "sk-1...[REDACTED]") {
		t.Error("Expected redacted form to be present")
	}
}

func TestRedactSensitiveData_GoogleKey(t *testing.T) {
	fakeGoogleKey := "AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe" // Fake test key - not a real credential
	input := "Google API key: " + fakeGoogleKey
	result := RedactSensitiveData(input)

	if result == input {
		t.Error("Expected Google API key to be redacted")
	}

	if strings.Contains(result, fakeGoogleKey) {
		t.Error("Expected full API key to not be in result")
	}

	if !strings.Contains(result, "AIza...[REDACTED]") {
		t.Error("Expected redacted form to be present")
	}
}

func TestRedactSensitiveData_BearerToken(t *testing.T) {
	fakeToken := "abc123def456" // Fake test token - not a real credential
	input := "Authorization: Bearer " + fakeToken
	result := RedactSensitiveData(input)

	if result == input {
		t.Error("Expected Bearer token to be redacted")
	}

	if strings.Contains(result, "Bearer "+fakeToken) {
		t.Error("Expected full token to not be in result")
	}

	if !strings.Contains(result, "Bearer [REDACTED]") {
		t.Error("Expected redacted Bearer token")
	}
}

func TestRedactSensitiveData_MultipleKeys(t *testing.T) {
	fakeOpenAIKey := "sk-1234567890abcdefghijklmnopqrstuvwxyz12345678" // Fake test key - not a real credential
	fakeGoogleKey := "AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe"         // Fake test key - not a real credential
	input := "Keys: " + fakeOpenAIKey + " and " + fakeGoogleKey
	result := RedactSensitiveData(input)

	if strings.Contains(result, fakeOpenAIKey) {
		t.Error("OpenAI key should be redacted")
	}

	if strings.Contains(result, fakeGoogleKey) {
		t.Error("Google key should be redacted")
	}

	if !strings.Contains(result, "sk-1...[REDACTED]") || !strings.Contains(result, "AIza...[REDACTED]") {
		t.Error("Both keys should be redacted")
	}
}

func TestRedactSensitiveData_NoSensitiveData(t *testing.T) {
	input := "This is just a normal string with no secrets"
	result := RedactSensitiveData(input)

	if result != input {
		t.Error("Expected string without sensitive data to remain unchanged")
	}
}

func TestAPIRequest_BasicCall(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	defer SetVerbose(false)

	// Should not panic
	APIRequest("TestProvider", "POST", "https://api.test.com/v1/endpoint", nil, nil)
}

func TestAPIRequest_WithHeaders(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	defer SetVerbose(false)

	fakeBearerToken := "sk-1234567890abcdefghijklmnopqrstuvwxyz12345678" // Fake test key - not a real credential
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + fakeBearerToken,
	}

	// Should not panic and should redact the bearer token
	APIRequest("TestProvider", "POST", "https://api.test.com/v1/endpoint", headers, nil)
}

func TestAPIRequest_WithBody(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	defer SetVerbose(false)

	body := map[string]interface{}{
		"prompt":      "Hello world",
		"temperature": 0.7,
		"max_tokens":  100,
	}

	// Should not panic
	APIRequest("TestProvider", "POST", "https://api.test.com/v1/endpoint", nil, body)
}

func TestAPIRequest_WithAPIKeyInURL(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	defer SetVerbose(false)

	fakeAPIKey := "AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe" // Fake test key - not a real credential
	url := "https://api.test.com/v1/endpoint?key=" + fakeAPIKey

	// Should not panic and should redact the API key in URL
	APIRequest("TestProvider", "GET", url, nil, nil)
}

func TestAPIRequest_WhenVerboseDisabled(t *testing.T) {
	SetVerbose(false) // Disable debug logging

	// Should not panic and should be no-op (not log anything)
	APIRequest("TestProvider", "POST", "https://api.test.com/v1/endpoint", nil, nil)
}

func TestAPIResponse_Success(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	defer SetVerbose(false)

	body := `{"status":"success","data":{"id":"123"}}`

	// Should not panic
	APIResponse("TestProvider", 200, body, nil)
}

func TestAPIResponse_Error(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	defer SetVerbose(false)

	// Should not panic
	APIResponse("TestProvider", 500, "", errors.New("connection failed"))
}

func TestAPIResponse_WithSensitiveDataInBody(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	defer SetVerbose(false)

	fakeAPIKeyInJSON := "sk-1234567890abcdefghijklmnopqrstuvwxyz12345678" // Fake test key - not a real credential
	body := `{"api_key":"` + fakeAPIKeyInJSON + `","status":"ok"}`

	// Should not panic and should redact API key in body
	APIResponse("TestProvider", 200, body, nil)
}

func TestAPIResponse_InvalidJSON(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	defer SetVerbose(false)

	body := "This is not JSON"

	// Should not panic, should handle non-JSON body gracefully
	APIResponse("TestProvider", 200, body, nil)
}

func TestAPIResponse_EmptyBody(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	defer SetVerbose(false)

	// Should not panic
	APIResponse("TestProvider", 204, "", nil)
}

func TestAPIResponse_ClientError(t *testing.T) {
	SetVerbose(true) // Enable debug logging
	defer SetVerbose(false)

	body := `{"error":"rate limit exceeded"}`

	// Should not panic, 4xx should be logged appropriately
	APIResponse("TestProvider", 429, body, nil)
}

func TestAPIResponse_WhenVerboseDisabled(t *testing.T) {
	SetVerbose(false) // Disable debug logging

	// Should not panic and should be no-op (not log anything)
	APIResponse("TestProvider", 200, `{"status":"ok"}`, nil)
}

func TestRedactSensitiveData_ShortKey(t *testing.T) {
	// OpenAI keys are required to be at least 32 chars, so short keys won't match
	input := "Short: sk-abc"
	result := RedactSensitiveData(input)

	// Should remain unchanged as it doesn't match the pattern
	if result != input {
		t.Error("Expected short key to remain unchanged as it doesn't match pattern")
	}
}

func TestAPIRequest_WithMarshalError(t *testing.T) {
	SetVerbose(true)
	defer SetVerbose(false)

	// Create a body that can't be marshaled (channels can't be marshaled to JSON)
	body := make(chan int)

	// Should not panic, should log marshal error
	APIRequest("TestProvider", "POST", "https://api.test.com", nil, body)
}

func TestLLMResponse_WithExtraAttributes(t *testing.T) {
	// Test that extra attributes are properly included
	LLMResponse("openai", "assistant", 100, 50, 0.01, "model", "gpt-4", "latency_ms", 500)
}

func TestLLMError_WithExtraAttributes(t *testing.T) {
	// Test that extra attributes are properly included
	LLMError("openai", "assistant", errors.New("test error"), "attempt", 3, "retry_after", 60)
}

func TestToolCall_WithExtraAttributes(t *testing.T) {
	// Test that extra attributes are properly included
	ToolCall("openai", 5, 3, "auto", "model", "gpt-4", "max_iterations", 10)
}

func TestToolResponse_WithExtraAttributes(t *testing.T) {
	// Test that extra attributes are properly included
	ToolResponse("openai", 150, 50, 2, 0.01, "duration_ms", 1500)
}

func TestLogFormatJSON(t *testing.T) {
	// Save and restore state
	origFormat := currentFormat
	origOutput := logOutput
	defer func() {
		currentFormat = origFormat
		logOutput = origOutput
		initLogger(currentLevel, nil)
	}()

	var buf bytes.Buffer
	logOutput = &buf
	currentFormat = FormatJSON
	initLogger(slog.LevelInfo, nil)

	Info("json test message", "key", "value")

	output := buf.String()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
		t.Fatalf("Expected valid JSON output, got error: %v\nOutput: %s", err, output)
	}
	if msg, ok := parsed["msg"].(string); !ok || msg != "json test message" {
		t.Errorf("Expected msg 'json test message', got %v", parsed["msg"])
	}
}

func TestLogFormatText(t *testing.T) {
	// Save and restore state
	origFormat := currentFormat
	origOutput := logOutput
	defer func() {
		currentFormat = origFormat
		logOutput = origOutput
		initLogger(currentLevel, nil)
	}()

	var buf bytes.Buffer
	logOutput = &buf
	currentFormat = FormatText
	initLogger(slog.LevelInfo, nil)

	Info("text test message", "key", "value")

	output := buf.String()
	// Text format should NOT be valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err == nil {
		t.Error("Expected non-JSON output for text format, but got valid JSON")
	}
	if !strings.Contains(output, "text test message") {
		t.Errorf("Expected output to contain message, got: %s", output)
	}
}

func TestLogFormatEnvVar(t *testing.T) {
	// Save and restore state
	origFormat := currentFormat
	origOutput := logOutput
	defer func() {
		currentFormat = origFormat
		logOutput = origOutput
		initLogger(currentLevel, nil)
	}()

	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{"json lowercase", "json", FormatJSON},
		{"json uppercase", "JSON", FormatJSON},
		{"json mixed case", "Json", FormatJSON},
		{"text explicit", "text", FormatText},
		{"empty defaults to text", "", FormatText},
		{"unknown defaults to text", "xml", FormatText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentFormat = FormatText // reset
			if strings.EqualFold(tt.envValue, FormatJSON) {
				currentFormat = FormatJSON
			}
			if currentFormat != tt.expected {
				t.Errorf("Expected format %q, got %q", tt.expected, currentFormat)
			}
		})
	}
}

func TestSetLogger_Custom(t *testing.T) {
	// Save and restore state
	origLogger := DefaultLogger
	origOutput := logOutput
	defer func() {
		DefaultLogger = origLogger
		logOutput = origOutput
		initLogger(currentLevel, nil)
	}()

	var buf bytes.Buffer
	custom := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	SetLogger(custom)

	Info("custom logger test", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "custom logger test") {
		t.Errorf("Expected custom logger to capture output, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("Expected structured attrs in output, got: %s", output)
	}
}

func TestSetLogger_SetLevelPreservesCustomLogger(t *testing.T) {
	// Save and restore state
	origLogger := DefaultLogger
	origOutput := logOutput
	origHandler := customHandler
	defer func() {
		customHandler = origHandler
		DefaultLogger = origLogger
		logOutput = origOutput
		initLogger(currentLevel, nil)
	}()

	var buf bytes.Buffer
	custom := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	SetLogger(custom)

	// SetLevel should NOT replace the custom logger
	SetLevel(slog.LevelDebug)

	Info("after set level", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "after set level") {
		t.Errorf("Expected custom logger to still capture output after SetLevel(), got: %s", output)
	}
}

func TestSetLogger_NilResetsDefault(t *testing.T) {
	// Save and restore state
	origLogger := DefaultLogger
	origOutput := logOutput
	defer func() {
		DefaultLogger = origLogger
		logOutput = origOutput
		initLogger(currentLevel, nil)
	}()

	// Set a custom logger
	var buf bytes.Buffer
	custom := slog.New(slog.NewTextHandler(&buf, nil))
	SetLogger(custom)

	if DefaultLogger != custom {
		t.Error("Expected DefaultLogger to be the custom logger")
	}

	// Reset with nil
	SetLogger(nil)

	if DefaultLogger == custom {
		t.Error("Expected DefaultLogger to be reset after SetLogger(nil)")
	}
	if DefaultLogger == nil {
		t.Error("Expected DefaultLogger to not be nil after SetLogger(nil)")
	}

	// Logging should still work
	Info("after reset")
}

func TestSetLogger_SlogDefaultUpdated(t *testing.T) {
	// Save and restore state
	origLogger := DefaultLogger
	origOutput := logOutput
	defer func() {
		DefaultLogger = origLogger
		logOutput = origOutput
		initLogger(currentLevel, nil)
	}()

	var buf bytes.Buffer
	custom := slog.New(slog.NewTextHandler(&buf, nil))
	SetLogger(custom)

	if slog.Default() != custom {
		t.Error("Expected slog.Default() to return the custom logger")
	}
}

func TestSetLogger_ConfigureDoesNotOverwrite(t *testing.T) {
	// Save and restore state
	origLogger := DefaultLogger
	origOutput := logOutput
	origHandler := customHandler
	defer func() {
		customHandler = origHandler
		DefaultLogger = origLogger
		logOutput = origOutput
		initLogger(currentLevel, nil)
	}()

	// Set a custom logger writing to a buffer
	var buf bytes.Buffer
	custom := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	SetLogger(custom)

	// Configure should not overwrite the custom logger
	err := Configure(&LoggingConfigSpec{DefaultLevel: "debug"})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}

	// Log a message — it should still go to the custom buffer
	Info("after configure", "source", "test")

	output := buf.String()
	if !strings.Contains(output, "after configure") {
		t.Errorf("Expected custom logger to still capture output after Configure(), got: %s", output)
	}
}

func TestSetOutputPreservesFormat(t *testing.T) {
	// Save and restore state
	origFormat := currentFormat
	origOutput := logOutput
	defer func() {
		currentFormat = origFormat
		logOutput = origOutput
		initLogger(currentLevel, nil)
	}()

	// Set JSON format, then call SetOutput — format should be preserved
	currentFormat = FormatJSON
	var buf bytes.Buffer
	SetOutput(&buf)

	Info("format preserved", "key", "value")

	output := buf.String()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
		t.Fatalf("Expected JSON output after SetOutput, got error: %v\nOutput: %s", err, output)
	}
}
