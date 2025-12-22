package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()

	// Test each helper function
	ctx = WithTurnID(ctx, "turn-123")
	ctx = WithScenario(ctx, "test-scenario")
	ctx = WithScenarioVersion(ctx, "v1")
	ctx = WithProvider(ctx, "openai")
	ctx = WithModel(ctx, "gpt-4")
	ctx = WithStage(ctx, "execution")
	ctx = WithSessionID(ctx, "session-456")
	ctx = WithRequestID(ctx, "request-789")
	ctx = WithCorrelationID(ctx, "corr-abc")
	ctx = WithEnvironment(ctx, "production")

	// Verify values are stored correctly
	if v := ctx.Value(ContextKeyTurnID); v != "turn-123" {
		t.Errorf("TurnID: expected turn-123, got %v", v)
	}
	if v := ctx.Value(ContextKeyScenario); v != "test-scenario" {
		t.Errorf("Scenario: expected test-scenario, got %v", v)
	}
	if v := ctx.Value(ContextKeyScenarioVersion); v != "v1" {
		t.Errorf("ScenarioVersion: expected v1, got %v", v)
	}
	if v := ctx.Value(ContextKeyProvider); v != "openai" {
		t.Errorf("Provider: expected openai, got %v", v)
	}
	if v := ctx.Value(ContextKeyModel); v != "gpt-4" {
		t.Errorf("Model: expected gpt-4, got %v", v)
	}
	if v := ctx.Value(ContextKeyStage); v != "execution" {
		t.Errorf("Stage: expected execution, got %v", v)
	}
	if v := ctx.Value(ContextKeySessionID); v != "session-456" {
		t.Errorf("SessionID: expected session-456, got %v", v)
	}
	if v := ctx.Value(ContextKeyRequestID); v != "request-789" {
		t.Errorf("RequestID: expected request-789, got %v", v)
	}
	if v := ctx.Value(ContextKeyCorrelationID); v != "corr-abc" {
		t.Errorf("CorrelationID: expected corr-abc, got %v", v)
	}
	if v := ctx.Value(ContextKeyEnvironment); v != "production" {
		t.Errorf("Environment: expected production, got %v", v)
	}
}

func TestWithLoggingContext(t *testing.T) {
	ctx := context.Background()

	fields := &LoggingFields{
		TurnID:          "turn-123",
		Scenario:        "test-scenario",
		ScenarioVersion: "v1",
		Provider:        "openai",
		Model:           "gpt-4",
		Stage:           "execution",
		SessionID:       "session-456",
		RequestID:       "request-789",
		CorrelationID:   "corr-abc",
		Environment:     "production",
	}

	ctx = WithLoggingContext(ctx, fields)

	// Verify all values are set
	if v := ctx.Value(ContextKeyTurnID); v != "turn-123" {
		t.Errorf("TurnID: expected turn-123, got %v", v)
	}
	if v := ctx.Value(ContextKeyProvider); v != "openai" {
		t.Errorf("Provider: expected openai, got %v", v)
	}
}

func TestWithLoggingContext_PartialFields(t *testing.T) {
	ctx := context.Background()

	// Set some pre-existing values
	ctx = WithTurnID(ctx, "existing-turn")

	// Only set some fields
	fields := &LoggingFields{
		Provider: "anthropic",
		Model:    "claude-3",
	}

	ctx = WithLoggingContext(ctx, fields)

	// Verify new values are set
	if v := ctx.Value(ContextKeyProvider); v != "anthropic" {
		t.Errorf("Provider: expected anthropic, got %v", v)
	}

	// Verify existing value is NOT overwritten when empty in LoggingFields
	// Note: WithLoggingContext only sets non-empty values
	if v := ctx.Value(ContextKeyTurnID); v != "existing-turn" {
		t.Errorf("TurnID should still be existing-turn, got %v", v)
	}
}

func TestExtractLoggingFields(t *testing.T) {
	ctx := context.Background()
	ctx = WithTurnID(ctx, "turn-123")
	ctx = WithScenario(ctx, "test-scenario")
	ctx = WithProvider(ctx, "openai")
	ctx = WithStage(ctx, "streaming")

	fields := ExtractLoggingFields(ctx)

	if fields.TurnID != "turn-123" {
		t.Errorf("TurnID: expected turn-123, got %s", fields.TurnID)
	}
	if fields.Scenario != "test-scenario" {
		t.Errorf("Scenario: expected test-scenario, got %s", fields.Scenario)
	}
	if fields.Provider != "openai" {
		t.Errorf("Provider: expected openai, got %s", fields.Provider)
	}
	if fields.Stage != "streaming" {
		t.Errorf("Stage: expected streaming, got %s", fields.Stage)
	}
	// Unset fields should be empty
	if fields.Model != "" {
		t.Errorf("Model: expected empty, got %s", fields.Model)
	}
}

func TestExtractLoggingFields_EmptyContext(t *testing.T) {
	ctx := context.Background()

	fields := ExtractLoggingFields(ctx)

	// All fields should be empty
	if fields.TurnID != "" || fields.Scenario != "" || fields.Provider != "" {
		t.Error("Expected all fields to be empty for empty context")
	}
}

func TestWithLoggingContext_Nil(t *testing.T) {
	ctx := context.Background()

	// Should handle nil fields gracefully
	result := WithLoggingContext(ctx, nil)

	// Should return the original context unchanged
	if result != ctx {
		t.Error("Expected original context when fields is nil")
	}
}

func TestContextHandler_ExtractsContextFields(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create a text handler that writes to the buffer
	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	// Wrap with our context handler
	contextHandler := NewContextHandler(textHandler)
	logger := slog.New(contextHandler)

	// Create context with logging fields
	ctx := context.Background()
	ctx = WithTurnID(ctx, "turn-123")
	ctx = WithScenario(ctx, "test-scenario")
	ctx = WithProvider(ctx, "openai")

	// Log a message with context
	logger.InfoContext(ctx, "test message", "custom_field", "custom_value")

	output := buf.String()

	// Verify context fields are present in output
	if !strings.Contains(output, "turn_id=turn-123") {
		t.Errorf("Expected turn_id in output, got: %s", output)
	}
	if !strings.Contains(output, "scenario=test-scenario") {
		t.Errorf("Expected scenario in output, got: %s", output)
	}
	if !strings.Contains(output, "provider=openai") {
		t.Errorf("Expected provider in output, got: %s", output)
	}
	// Verify custom field is also present
	if !strings.Contains(output, "custom_field=custom_value") {
		t.Errorf("Expected custom_field in output, got: %s", output)
	}
}

func TestContextHandler_WithCommonFields(t *testing.T) {
	var buf bytes.Buffer

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	// Create handler with common fields
	contextHandler := NewContextHandler(textHandler,
		slog.String("service", "promptkit"),
		slog.String("version", "1.0.0"),
	)
	logger := slog.New(contextHandler)

	// Log without any context
	logger.Info("test message")

	output := buf.String()

	// Verify common fields are present
	if !strings.Contains(output, "service=promptkit") {
		t.Errorf("Expected service in output, got: %s", output)
	}
	if !strings.Contains(output, "version=1.0.0") {
		t.Errorf("Expected version in output, got: %s", output)
	}
}

func TestContextHandler_ContextOverridesCommonFields(t *testing.T) {
	var buf bytes.Buffer

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	// Create handler with common provider field
	contextHandler := NewContextHandler(textHandler,
		slog.String("provider", "default-provider"),
	)
	logger := slog.New(contextHandler)

	// Log with context that has different provider
	ctx := WithProvider(context.Background(), "openai")
	logger.InfoContext(ctx, "test message")

	output := buf.String()

	// The context value should appear (last one wins in slog)
	if !strings.Contains(output, "provider=openai") {
		t.Errorf("Expected provider=openai in output, got: %s", output)
	}
}

func TestContextHandler_EmptyContextValues(t *testing.T) {
	var buf bytes.Buffer

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	contextHandler := NewContextHandler(textHandler)
	logger := slog.New(contextHandler)

	// Log with empty context
	logger.Info("test message")

	output := buf.String()

	// Should not contain any context keys with empty values
	if strings.Contains(output, "turn_id=") {
		t.Errorf("Should not include empty turn_id, got: %s", output)
	}
	if strings.Contains(output, "scenario=") {
		t.Errorf("Should not include empty scenario, got: %s", output)
	}
}

func TestContextHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	contextHandler := NewContextHandler(textHandler)
	// Create a logger with pre-set attrs
	logger := slog.New(contextHandler).With("component", "test")

	ctx := WithTurnID(context.Background(), "turn-123")
	logger.InfoContext(ctx, "test message")

	output := buf.String()

	// Both should be present
	if !strings.Contains(output, "component=test") {
		t.Errorf("Expected component in output, got: %s", output)
	}
	if !strings.Contains(output, "turn_id=turn-123") {
		t.Errorf("Expected turn_id in output, got: %s", output)
	}
}

func TestContextHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	contextHandler := NewContextHandler(textHandler)
	// Create a logger with a group
	logger := slog.New(contextHandler).WithGroup("request")

	ctx := WithTurnID(context.Background(), "turn-123")
	logger.InfoContext(ctx, "test message", "path", "/api/v1")

	output := buf.String()

	// Group should be present
	if !strings.Contains(output, "request.path=/api/v1") {
		t.Errorf("Expected grouped path in output, got: %s", output)
	}
}

func TestContextHandler_Enabled(t *testing.T) {
	textHandler := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})

	contextHandler := NewContextHandler(textHandler)

	ctx := context.Background()

	// Debug should not be enabled
	if contextHandler.Enabled(ctx, slog.LevelDebug) {
		t.Error("Debug should not be enabled when level is Warn")
	}

	// Warn should be enabled
	if !contextHandler.Enabled(ctx, slog.LevelWarn) {
		t.Error("Warn should be enabled")
	}

	// Error should be enabled
	if !contextHandler.Enabled(ctx, slog.LevelError) {
		t.Error("Error should be enabled")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"trace", slog.LevelDebug - 4},
		{"TRACE", slog.LevelDebug - 4},
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo}, // defaults to info
		{"", slog.LevelInfo},        // empty defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestContextHandler_Unwrap(t *testing.T) {
	textHandler := slog.NewTextHandler(&bytes.Buffer{}, nil)
	contextHandler := NewContextHandler(textHandler)

	unwrapped := contextHandler.Unwrap()

	if unwrapped != textHandler {
		t.Error("Unwrap should return the inner handler")
	}
}
