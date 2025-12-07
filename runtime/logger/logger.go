// Package logger provides structured logging with automatic PII redaction.
//
// This package wraps Go's standard log/slog with convenience functions for:
//   - LLM API call logging (requests, responses, errors)
//   - Tool execution logging
//   - Automatic API key and sensitive data redaction
//   - Contextual logging with request tracing
//   - Level-based verbosity control
//
// All exported functions use the global DefaultLogger which can be configured
// for different output formats and log levels.
package logger

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var (
	// DefaultLogger is the global structured logger instance.
	// It is safe for concurrent use and initialized with slog.LevelInfo by default.
	DefaultLogger *slog.Logger
)

func init() {
	// Check LOG_LEVEL environment variable
	level := slog.LevelInfo
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		switch strings.ToLower(envLevel) {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn", "warning":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}

	// Initialize with text handler writing to stderr
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	DefaultLogger = slog.New(handler)
}

// SetLevel changes the logging level for all subsequent log operations.
// This is safe for concurrent use as it replaces the entire logger instance.
func SetLevel(level slog.Level) {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	DefaultLogger = slog.New(handler)
}

// SetVerbose enables debug-level logging when verbose is true, otherwise sets info-level.
// This is a convenience wrapper around SetLevel for command-line verbose flags.
func SetVerbose(verbose bool) {
	if verbose {
		SetLevel(slog.LevelDebug)
	} else {
		SetLevel(slog.LevelInfo)
	}
}

// Info logs an informational message with structured key-value attributes.
// Args should be provided in key-value pairs: key1, value1, key2, value2, ...
func Info(msg string, args ...any) {
	DefaultLogger.Info(msg, args...)
}

// InfoContext logs an informational message with context and structured attributes.
// The context can be used for request tracing and cancellation.
func InfoContext(ctx context.Context, msg string, args ...any) {
	DefaultLogger.InfoContext(ctx, msg, args...)
}

// Debug logs a debug-level message with structured attributes.
// Debug messages are only output when the log level is set to LevelDebug or lower.
func Debug(msg string, args ...any) {
	DefaultLogger.Debug(msg, args...)
}

// DebugContext logs a debug message with context and structured attributes.
func DebugContext(ctx context.Context, msg string, args ...any) {
	DefaultLogger.DebugContext(ctx, msg, args...)
}

// Warn logs a warning message with structured attributes.
// Use for recoverable errors or unexpected but non-critical situations.
func Warn(msg string, args ...any) {
	DefaultLogger.Warn(msg, args...)
}

// WarnContext logs a warning message with context and structured attributes.
func WarnContext(ctx context.Context, msg string, args ...any) {
	DefaultLogger.WarnContext(ctx, msg, args...)
}

// Error logs an error message with structured attributes.
// Use for errors that affect operation but don't cause complete failure.
func Error(msg string, args ...any) {
	DefaultLogger.Error(msg, args...)
}

// ErrorContext logs an error message with context and structured attributes.
func ErrorContext(ctx context.Context, msg string, args ...any) {
	DefaultLogger.ErrorContext(ctx, msg, args...)
}

// LLMCall logs an LLM API call with structured fields for observability.
// Additional attributes can be passed as key-value pairs after the required parameters.
func LLMCall(provider, role string, messages int, temperature float64, attrs ...any) {
	allAttrs := make([]any, 0, 8+len(attrs))
	allAttrs = append(allAttrs,
		"provider", provider,
		"role", role,
		"messages", messages,
		"temperature", temperature,
	)
	allAttrs = append(allAttrs, attrs...)
	Info("🤖 LLM API Call", allAttrs...)
}

// LLMResponse logs an LLM API response with token usage and cost tracking.
// Cost should be provided in USD (e.g., 0.0001 for $0.0001).
func LLMResponse(provider, role string, tokensIn, tokensOut int, cost float64, attrs ...any) {
	allAttrs := make([]any, 0, 10+len(attrs))
	allAttrs = append(allAttrs,
		"provider", provider,
		"role", role,
		"tokens_in", tokensIn,
		"tokens_out", tokensOut,
		"cost", cost,
	)
	allAttrs = append(allAttrs, attrs...)
	Info("✅ LLM API Response", allAttrs...)
}

// LLMError logs an LLM API error for debugging and monitoring.
func LLMError(provider, role string, err error, attrs ...any) {
	allAttrs := make([]any, 0, 6+len(attrs))
	allAttrs = append(allAttrs,
		"provider", provider,
		"role", role,
		"error", err,
	)
	allAttrs = append(allAttrs, attrs...)
	Error("❌ LLM API Call Failed", allAttrs...)
}

// ToolCall logs a tool execution request with context about available tools.
// The choice parameter indicates the tool selection mode (e.g., "auto", "required", "none").
func ToolCall(provider string, messages, tools int, choice string, attrs ...any) {
	allAttrs := make([]any, 0, 8+len(attrs))
	allAttrs = append(allAttrs,
		"provider", provider,
		"messages", messages,
		"tools", tools,
		"choice", choice,
	)
	allAttrs = append(allAttrs, attrs...)
	Info("🔧 LLM Tool Call", allAttrs...)
}

// ToolResponse logs the result of tool executions with token usage and cost.
func ToolResponse(provider string, tokensIn, tokensOut, toolCalls int, cost float64, attrs ...any) {
	allAttrs := make([]any, 0, 10+len(attrs))
	allAttrs = append(allAttrs,
		"provider", provider,
		"tokens_in", tokensIn,
		"tokens_out", tokensOut,
		"tool_calls", toolCalls,
		"cost", cost,
	)
	allAttrs = append(allAttrs, attrs...)
	Info("✅ LLM Tool Response", allAttrs...)
}

var (
	// apiKeyPatterns contains compiled regular expressions for detecting sensitive data.
	// Patterns match common API key formats from various providers.
	apiKeyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`sk-[a-zA-Z0-9]{32,}`),     // OpenAI API keys
		regexp.MustCompile(`AIza[a-zA-Z0-9_-]{35}`),   // Google API keys
		regexp.MustCompile(`Bearer\s+[a-zA-Z0-9_-]+`), // Bearer tokens
	}
)

// RedactSensitiveData removes API keys and other sensitive information from strings.
// It replaces matched patterns with a redacted form that preserves the first few characters
// for debugging while hiding the sensitive portion.
//
// Supported patterns:
//   - OpenAI keys (sk-...): Shows first 4 chars
//   - Google keys (AIza...): Shows first 4 chars
//   - Bearer tokens: Shows only "Bearer [REDACTED]"
//
// This function is safe for concurrent use as it only reads from the compiled patterns.
func RedactSensitiveData(input string) string {
	result := input

	for _, pattern := range apiKeyPatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			if strings.HasPrefix(match, "Bearer ") {
				return "Bearer [REDACTED]"
			}
			// Show first 4 characters for debugging context
			if len(match) > 8 {
				return match[:4] + "...[REDACTED]"
			}
			return "[REDACTED]"
		})
	}

	return result
}

// APIRequest logs HTTP API request details at debug level with automatic PII redaction.
// This function is a no-op when debug logging is disabled for performance.
//
// Parameters:
//   - provider: The API provider name (e.g., "OpenAI", "Anthropic")
//   - method: HTTP method (GET, POST, etc.)
//   - url: Request URL (will be redacted for sensitive data)
//   - headers: HTTP headers map (will be redacted)
//   - body: Request body (will be marshaled to JSON and redacted)
//
// Sensitive data in URL, headers, and body are automatically redacted.
func APIRequest(provider, method, url string, headers map[string]string, body interface{}) {
	// Early return if debug logging is disabled for performance
	if !DefaultLogger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}

	attrs := make([]any, 0, 8)
	attrs = append(attrs,
		"provider", provider,
		"method", method,
		"url", RedactSensitiveData(url),
	)

	// Redact sensitive data in headers
	if len(headers) > 0 {
		redactedHeaders := make(map[string]string, len(headers))
		for key, value := range headers {
			redactedHeaders[key] = RedactSensitiveData(value)
		}
		attrs = append(attrs, "headers", redactedHeaders)
	}

	// Marshal and redact request body
	if body != nil {
		bodyJSON, err := json.Marshal(body)
		if err != nil {
			attrs = append(attrs, "body_error", err.Error())
		} else {
			redactedBody := RedactSensitiveData(string(bodyJSON))
			attrs = append(attrs, "body", redactedBody)
		}
	}

	Debug("🔵 API Request", attrs...)
}

// APIResponse logs HTTP API response details at debug level with automatic PII redaction.
// This function is a no-op when debug logging is disabled for performance.
//
// Parameters:
//   - provider: The API provider name
//   - statusCode: HTTP status code
//   - body: Response body as string (will be redacted)
//   - err: Error if the request failed (takes precedence over body logging)
//
// Response bodies are attempted to be parsed as JSON for pretty formatting.
// Status codes are logged with emoji indicators: 🟢 (2xx), 🟡 (3xx), 🔴 (4xx/5xx).
func APIResponse(provider string, statusCode int, body string, err error) {
	// Early return if debug logging is disabled for performance
	if !DefaultLogger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}

	attrs := make([]any, 0, 6)
	attrs = append(attrs,
		"provider", provider,
		"status_code", statusCode,
	)

	// Log errors at error level
	if err != nil {
		attrs = append(attrs, "error", err.Error())
		Error("🔴 API Response Error", attrs...)
		return
	}

	// Determine emoji based on status code
	var emoji string
	switch {
	case statusCode >= 200 && statusCode < 300:
		emoji = "🟢"
	case statusCode >= 400:
		emoji = "🔴"
	default:
		emoji = "🟡"
	}

	// Pretty-format JSON responses when possible
	if body != "" {
		var jsonObj interface{}
		if json.Unmarshal([]byte(body), &jsonObj) == nil {
			prettyJSON, _ := json.MarshalIndent(jsonObj, "", "  ") // NOSONAR: Formatting error falls back to original body
			redactedBody := RedactSensitiveData(string(prettyJSON))
			attrs = append(attrs, "body", redactedBody)
		} else {
			// Not JSON, log as-is with redaction
			redactedBody := RedactSensitiveData(body)
			attrs = append(attrs, "body", redactedBody)
		}
	}

	Debug(emoji+" API Response", attrs...)
}
