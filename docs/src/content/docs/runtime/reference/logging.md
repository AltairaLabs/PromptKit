---
title: Logging
sidebar:
  order: 7
---
Structured logging with context enrichment and per-module log level control.

## Overview

The PromptKit logging system provides:

- **Structured logging**: Built on Go's `log/slog` for JSON and text output
- **Context enrichment**: Automatic field extraction from context (turn ID, provider, scenario)
- **Per-module levels**: Configure different log levels for different modules
- **PII redaction**: Automatic redaction of API keys and sensitive data
- **Common fields**: Add fields that appear in every log entry

## Import Path

```go
import "github.com/AltairaLabs/PromptKit/runtime/logger"
```

## Quick Start

### Basic Logging

```go
import "github.com/AltairaLabs/PromptKit/runtime/logger"

// Log at different levels
logger.Info("Processing request", "user_id", "12345")
logger.Debug("Request details", "method", "POST", "path", "/api/chat")
logger.Warn("Rate limit approaching", "remaining", 10)
logger.Error("Request failed", "error", err)
```

### Context-Aware Logging

```go
// Add context fields that appear in all logs within this context
ctx := logger.WithLoggingContext(ctx, &logger.LoggingFields{
    Scenario:  "customer-support",
    Provider:  "openai",
    SessionID: "sess-123",
})

// Fields automatically included in log output
logger.InfoContext(ctx, "Processing turn")
// Output includes: scenario=customer-support provider=openai session_id=sess-123
```

## Configuration

### LoggingConfig Schema

Configuration follows the K8s-style resource format:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: LoggingConfig
metadata:
  name: production-logging
spec:
  defaultLevel: info
  format: json
  commonFields:
    service: my-app
    environment: production
  modules:
    - name: runtime.pipeline
      level: debug
    - name: providers
      level: warn
```

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `spec.defaultLevel` | string | `info` | Default log level for all modules |
| `spec.format` | string | `text` | Output format: `json` or `text` |
| `spec.commonFields` | map | `{}` | Fields added to every log entry |
| `spec.modules` | array | `[]` | Per-module log level overrides |

### Log Levels

| Level | Description | Use Case |
|-------|-------------|----------|
| `trace` | Most verbose | Detailed debugging, request/response bodies |
| `debug` | Debug information | Development, troubleshooting |
| `info` | Normal operations | Production default |
| `warn` | Warning conditions | Recoverable errors, deprecations |
| `error` | Error conditions | Failures requiring attention |

### Programmatic Configuration

```go
import "github.com/AltairaLabs/PromptKit/runtime/logger"

cfg := &logger.LoggingConfigSpec{
    DefaultLevel: "info",
    Format:       logger.FormatJSON,
    CommonFields: map[string]string{
        "service": "my-app",
    },
    Modules: []logger.ModuleLoggingSpec{
        {Name: "runtime", Level: "debug"},
        {Name: "providers.openai", Level: "warn"},
    },
}

if err := logger.Configure(cfg); err != nil {
    log.Fatal(err)
}
```

### Environment Variable

Set the default log level via environment variable:

```bash
export LOG_LEVEL=debug
```

## Context Enrichment

### Available Context Keys

| Key | Function | Description |
|-----|----------|-------------|
| `turn_id` | `WithTurnID()` | Conversation turn identifier |
| `scenario` | `WithScenario()` | Scenario name |
| `provider` | `WithProvider()` | LLM provider name |
| `session_id` | `WithSessionID()` | Session identifier |
| `model` | `WithModel()` | Model name |
| `stage` | `WithStage()` | Execution stage |
| `component` | `WithComponent()` | Component name |

### Adding Context Fields

```go
// Individual fields
ctx = logger.WithTurnID(ctx, "turn-1")
ctx = logger.WithProvider(ctx, "openai")
ctx = logger.WithModel(ctx, "gpt-4o")

// Multiple fields at once
ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
    TurnID:    "turn-1",
    Scenario:  "support-chat",
    Provider:  "openai",
    Model:     "gpt-4o",
    SessionID: "sess-abc123",
    Stage:     "execution",
    Component: "pipeline",
})
```

### Extracting Context Fields

```go
fields := logger.ExtractLoggingFields(ctx)
fmt.Printf("Current turn: %s\n", fields.TurnID)
fmt.Printf("Provider: %s\n", fields.Provider)
```

## Per-Module Log Levels

### Module Names

Module names are derived from package paths within PromptKit:

| Package | Module Name |
|---------|-------------|
| `runtime/pipeline` | `runtime.pipeline` |
| `runtime/logger` | `runtime.logger` |
| `providers/openai` | `providers.openai` |
| `tools/arena/engine` | `tools.arena.engine` |

### Hierarchical Matching

Module levels are matched hierarchically. A module inherits the log level of its parent if not explicitly configured:

```go
mc := logger.NewModuleConfig(slog.LevelInfo)
mc.SetModuleLevel("runtime", slog.LevelWarn)
mc.SetModuleLevel("runtime.pipeline", slog.LevelDebug)

// Results:
// - "runtime" -> Warn
// - "runtime.pipeline" -> Debug
// - "runtime.pipeline.stage" -> Debug (inherits from runtime.pipeline)
// - "runtime.streaming" -> Warn (inherits from runtime)
// - "providers.openai" -> Info (default)
```

### Using Module Config

```go
mc := logger.GetModuleConfig()

// Check current level for a module
level := mc.LevelFor("runtime.pipeline")

// Update levels dynamically
mc.SetModuleLevel("providers", slog.LevelDebug)
mc.SetDefaultLevel(slog.LevelWarn)
```

## PII Redaction

### Automatic Redaction

The logger automatically redacts sensitive data in debug logs:

```go
// API keys are redacted
logger.APIRequest("openai", "POST", url, headers, body)
// Output: sk-1234...[REDACTED] instead of full key
```

### Supported Patterns

| Pattern | Example | Redacted Form |
|---------|---------|---------------|
| OpenAI keys | `sk-abc123...` | `sk-a...[REDACTED]` |
| Google keys | `AIzaXYZ...` | `AIza...[REDACTED]` |
| Bearer tokens | `Bearer xyz...` | `Bearer [REDACTED]` |

### Manual Redaction

```go
redacted := logger.RedactSensitiveData(sensitiveString)
```

## LLM-Specific Logging

### Log LLM Calls

```go
// Log API call
logger.LLMCall("openai", "assistant", 5, 0.7,
    "model", "gpt-4o",
    "stream", true,
)

// Log response
logger.LLMResponse("openai", "assistant", 150, 200, 0.0001,
    "model", "gpt-4o",
    "finish_reason", "stop",
)

// Log error
logger.LLMError("openai", "assistant", err,
    "model", "gpt-4o",
    "status_code", 429,
)
```

### Log Tool Calls

```go
// Log tool request
logger.ToolCall("openai", 5, 3, "auto",
    "model", "gpt-4o",
)

// Log tool response
logger.ToolResponse("openai", 200, 150, 2, 0.00005,
    "tools_executed", []string{"get_weather", "search"},
)
```

### Log API Details

```go
// Debug-level API request logging
logger.APIRequest("openai", "POST", url, headers, body)

// Debug-level API response logging
logger.APIResponse("openai", 200, responseBody, nil)
```

## Output Formats

### Text Format (Default)

Human-readable format for development:

```
time=2024-01-15T10:30:00Z level=INFO msg="Processing request" turn_id=turn-1 provider=openai
```

### JSON Format

Structured format for log aggregation:

```json
{"time":"2024-01-15T10:30:00Z","level":"INFO","msg":"Processing request","turn_id":"turn-1","provider":"openai"}
```

## Testing

### Capture Log Output

```go
func TestLogging(t *testing.T) {
    var buf bytes.Buffer
    logger.SetOutput(&buf)
    defer logger.SetOutput(nil) // Reset to stderr

    logger.Info("test message", "key", "value")

    output := buf.String()
    if !strings.Contains(output, "test message") {
        t.Error("Expected message in output")
    }
}
```

### Set Test Log Level

```go
func TestVerbose(t *testing.T) {
    logger.SetLevel(slog.LevelDebug)
    defer logger.SetLevel(slog.LevelInfo)

    // Debug messages now visible
    logger.Debug("detailed info")
}
```

## Best Practices

### 1. Use Context for Correlation

```go
// Pass context through your call chain
func HandleRequest(ctx context.Context, req Request) {
    ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
        SessionID: req.SessionID,
        Provider:  req.Provider,
    })

    processRequest(ctx, req)
}

func processRequest(ctx context.Context, req Request) {
    // Logs automatically include session_id and provider
    logger.InfoContext(ctx, "Processing")
}
```

### 2. Configure Module Levels for Debugging

```yaml
# Quiet down noisy modules, verbose for the one you're debugging
spec:
  defaultLevel: warn
  modules:
    - name: runtime.pipeline
      level: debug
```

### 3. Use Appropriate Log Levels

```go
// Trace: Very detailed, typically only for development
logger.Debug("Request body", "body", requestJSON) // Use trace-level sparingly

// Debug: Useful for troubleshooting
logger.Debug("Cache miss", "key", cacheKey)

// Info: Normal operations
logger.Info("Request completed", "duration_ms", elapsed)

// Warn: Something unexpected but recoverable
logger.Warn("Retry succeeded", "attempt", 3)

// Error: Something went wrong
logger.Error("Request failed", "error", err)
```

### 4. Include Structured Data

```go
// Good: Structured fields for querying
logger.Info("User action",
    "user_id", userID,
    "action", "login",
    "ip", remoteAddr,
)

// Avoid: Unstructured string formatting
logger.Info(fmt.Sprintf("User %s logged in from %s", userID, remoteAddr))
```

## API Reference

### Package Functions

| Function | Description |
|----------|-------------|
| `Info(msg, args...)` | Log at info level |
| `InfoContext(ctx, msg, args...)` | Log at info level with context |
| `Debug(msg, args...)` | Log at debug level |
| `DebugContext(ctx, msg, args...)` | Log at debug level with context |
| `Warn(msg, args...)` | Log at warn level |
| `WarnContext(ctx, msg, args...)` | Log at warn level with context |
| `Error(msg, args...)` | Log at error level |
| `ErrorContext(ctx, msg, args...)` | Log at error level with context |
| `SetLevel(level)` | Set the global log level |
| `SetVerbose(bool)` | Set debug (true) or info (false) level |
| `SetOutput(writer)` | Set log output destination |
| `Configure(cfg)` | Apply logging configuration |
| `ParseLevel(string)` | Parse string to slog.Level |

### Context Functions

| Function | Description |
|----------|-------------|
| `WithTurnID(ctx, id)` | Add turn ID to context |
| `WithScenario(ctx, name)` | Add scenario name to context |
| `WithProvider(ctx, name)` | Add provider name to context |
| `WithSessionID(ctx, id)` | Add session ID to context |
| `WithModel(ctx, name)` | Add model name to context |
| `WithStage(ctx, name)` | Add execution stage to context |
| `WithComponent(ctx, name)` | Add component name to context |
| `WithLoggingContext(ctx, fields)` | Add multiple fields to context |
| `ExtractLoggingFields(ctx)` | Get all logging fields from context |

### LLM Logging Functions

| Function | Description |
|----------|-------------|
| `LLMCall(provider, role, messages, temp, attrs...)` | Log LLM API call |
| `LLMResponse(provider, role, tokensIn, tokensOut, cost, attrs...)` | Log LLM response |
| `LLMError(provider, role, err, attrs...)` | Log LLM error |
| `ToolCall(provider, messages, tools, choice, attrs...)` | Log tool call |
| `ToolResponse(provider, tokensIn, tokensOut, toolCalls, cost, attrs...)` | Log tool response |
| `APIRequest(provider, method, url, headers, body)` | Log API request (debug) |
| `APIResponse(provider, statusCode, body, err)` | Log API response (debug) |
| `RedactSensitiveData(input)` | Redact API keys from string |

## See Also

- [Pipeline Reference](pipeline) - Pipeline execution and middleware
- [Providers Reference](providers) - LLM provider implementations
- [Arena Config Reference](/arena/reference/config-schema) - Arena configuration including logging
