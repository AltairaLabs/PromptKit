package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

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

func TestRedactSensitiveData_ShortKey(t *testing.T) {
	// OpenAI keys are required to be at least 32 chars, so short keys won't match
	input := "Short: sk-abc"
	result := RedactSensitiveData(input)

	// Should remain unchanged as it doesn't match the pattern
	if result != input {
		t.Error("Expected short key to remain unchanged as it doesn't match pattern")
	}
}

func TestLogFormatJSON(t *testing.T) {
	// Save and restore state
	origFormat := currentFormat
	origOutput := logOutput
	defer func() {
		currentFormat = origFormat
		logOutput = origOutput
		initLogger(currentLevel)
	}()

	var buf bytes.Buffer
	logOutput = &buf
	currentFormat = FormatJSON
	initLogger(slog.LevelInfo)

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
		initLogger(currentLevel)
	}()

	var buf bytes.Buffer
	logOutput = &buf
	currentFormat = FormatText
	initLogger(slog.LevelInfo)

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

func TestSetLogger_Custom(t *testing.T) {
	// Save and restore state
	origLogger := GetLogger()
	origOutput := logOutput
	defer func() {
		storeLogger(origLogger)
		logOutput = origOutput
		initLogger(currentLevel)
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
	origLogger := GetLogger()
	origOutput := logOutput
	origHandler := customHandler
	defer func() {
		customHandler = origHandler
		storeLogger(origLogger)
		logOutput = origOutput
		initLogger(currentLevel)
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
	origLogger := GetLogger()
	origOutput := logOutput
	defer func() {
		storeLogger(origLogger)
		logOutput = origOutput
		initLogger(currentLevel)
	}()

	// Set a custom logger
	var buf bytes.Buffer
	custom := slog.New(slog.NewTextHandler(&buf, nil))
	SetLogger(custom)

	if GetLogger() != custom {
		t.Error("Expected GetLogger() to return the custom logger")
	}

	// Reset with nil
	SetLogger(nil)

	if GetLogger() == custom {
		t.Error("Expected GetLogger() to be reset after SetLogger(nil)")
	}
	if GetLogger() == nil {
		t.Error("Expected GetLogger() to not be nil after SetLogger(nil)")
	}

	// Logging should still work
	Info("after reset")
}

func TestSetLogger_SlogDefaultUpdated(t *testing.T) {
	// Save and restore state
	origLogger := GetLogger()
	origOutput := logOutput
	defer func() {
		storeLogger(origLogger)
		logOutput = origOutput
		initLogger(currentLevel)
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
	origLogger := GetLogger()
	origOutput := logOutput
	origHandler := customHandler
	defer func() {
		customHandler = origHandler
		storeLogger(origLogger)
		logOutput = origOutput
		initLogger(currentLevel)
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
		initLogger(currentLevel)
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

func TestRedactSensitiveData_BearerTokenShapes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"opaque", "Authorization: Bearer abc123_-xyz", "Authorization: Bearer [REDACTED]"},
		{"jwt", "Authorization: Bearer aaa.bbb.ccc", "Authorization: Bearer [REDACTED]"},
		{"base64 standard", "Bearer ab+cd/ef==", "Bearer [REDACTED]"},
		{"followed by comma", "Bearer aaa.bbb.ccc, next", "Bearer [REDACTED], next"},
		{"followed by quote", `{"h":"Bearer aaa.bbb.ccc"}`, `{"h":"Bearer [REDACTED]"}`},
		{"sentence period", "sent Bearer aaa.bbb.ccc.", "sent Bearer [REDACTED]."},
		{"end of line", "Bearer aaa.bbb.ccc\nnext", "Bearer [REDACTED]\nnext"},
		{"tab separator", "Bearer\tabc123", "Bearer [REDACTED]"},
		{"newline separator", "Bearer\nabc123", "Bearer [REDACTED]"},
		{"multiple spaces", "Bearer    abc123", "Bearer [REDACTED]"},
		{"mixed whitespace", "x Bearer \t\n abc123 y", "x Bearer [REDACTED] y"},
		{"openai key as bearer", "Authorization: Bearer sk-1234567890abcdefghijklmnopqrstuvwxyz12345678",
			"Authorization: Bearer [REDACTED]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactSensitiveData(tt.input); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
