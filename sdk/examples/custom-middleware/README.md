# Custom Middleware Example

This example demonstrates how to build custom middleware components for the PromptKit SDK pipeline to add observability, logging, and context injection capabilities.

## Overview

The SDK's `PipelineBuilder` provides a low-level API that allows you to create custom middleware that intercepts and processes execution at various stages of the pipeline. This example shows three practical middleware implementations:

- **MetricsMiddleware** - Tracks execution time, token usage, and costs
- **LoggingMiddleware** - Logs input/output messages for debugging
- **CustomContextMiddleware** - Injects custom context into system prompts

## Use Case

Common scenarios where custom middleware is valuable:

- **Observability**: Track latency, token consumption, and API costs
- **Debugging**: Log request/response pairs for troubleshooting
- **Context Injection**: Add dynamic context (user info, session data, etc.)
- **Rate Limiting**: Control request frequency and throttling
- **Content Filtering**: Pre/post-process messages for compliance
- **Caching**: Implement response caching strategies

## Running the Example

```bash
cd sdk/examples/custom-middleware
go run main.go
```

## Code Structure

- `main.go` - Complete middleware implementation and pipeline setup

## Middleware Interface

All middleware must implement the `pipeline.Middleware` interface:

```go
type Middleware interface {
    Process(execCtx *pipeline.ExecutionContext, next func() error) error
    StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error
}
```

## Implementation Examples

### MetricsMiddleware

Tracks execution metrics and reports token usage and costs:

```go
type MetricsMiddleware struct {
    serviceName string
}

func (m *MetricsMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
    start := time.Now()
    
    // Execute pipeline
    err := next()
    
    duration := time.Since(start)
    totalTokens := execCtx.CostInfo.InputTokens + execCtx.CostInfo.OutputTokens
    
    // Record and log metrics
    fmt.Printf("[%s] Duration: %v, Tokens: %d, Cost: $%.4f\n",
        m.serviceName, duration, totalTokens, execCtx.CostInfo.TotalCost)
    
    return err
}
```

**Use Cases**:

- Cost tracking and budgeting
- Performance monitoring
- Usage analytics
- Billing integration

### LoggingMiddleware

Logs input messages and response content for debugging:

```go
type LoggingMiddleware struct{}

func (m *LoggingMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
    // Log inputs
    for i, msg := range execCtx.Messages {
        fmt.Printf("  %d. [%s] %s\n", i+1, msg.Role, msg.Content)
    }
    
    err := next()
    
    // Log response
    if execCtx.Response != nil {
        fmt.Printf("[Logging] Response: %s\n", execCtx.Response.Content)
    }
    
    return err
}
```

**Use Cases**:

- Development debugging
- Request/response auditing
- Compliance logging
- Error diagnosis

### CustomContextMiddleware

Injects custom context into the system prompt:

```go
type CustomContextMiddleware struct {
    context string
}

func (m *CustomContextMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
    // Prepend or append context to system prompt
    if m.context != "" {
        if execCtx.SystemPrompt == "" {
            execCtx.SystemPrompt = m.context
        } else {
            execCtx.SystemPrompt = execCtx.SystemPrompt + "\n\n" + m.context
        }
    }
    
    return next()
}
```

**Use Cases**:

- Session-specific context
- User role/permissions injection
- Dynamic business rules
- Personalization

## Building a Pipeline with Middleware

Middleware is added in order and executes in a chain:

```go
pipe := sdk.NewPipelineBuilder().
    // Add custom context first (runs before others)
    WithMiddleware(&CustomContextMiddleware{
        context: "Context: This is a demo conversation.",
    }).
    // Add logging (runs second)
    WithMiddleware(&LoggingMiddleware{}).
    // Add metrics tracking (runs last, wraps entire execution)
    WithMiddleware(&MetricsMiddleware{
        serviceName: "predict-api",
    }).
    // Add provider
    WithSimpleProvider(provider).
    Build()
```

**Execution Order**:

1. MetricsMiddleware starts timer
2. LoggingMiddleware logs input
3. CustomContextMiddleware adds context
4. Provider executes LLM call
5. LoggingMiddleware logs output
6. MetricsMiddleware records metrics

## Middleware Execution Context

The `ExecutionContext` provides access to:

```go
type ExecutionContext struct {
    Messages      []types.Message    // Conversation history
    SystemPrompt  string            // System prompt (modifiable)
    Response      *types.Message    // LLM response
    CostInfo      types.CostInfo    // Token counts and costs
    Trace         types.Trace       // Execution trace with LLM calls
    // ... other fields
}
```

## Streaming Support

For streaming responses, implement `StreamChunk`:

```go
func (m *MetricsMiddleware) StreamChunk(
    execCtx *pipeline.ExecutionContext, 
    chunk *providers.StreamChunk,
) error {
    // Process each chunk as it arrives
    // E.g., track streaming latency, log chunks, etc.
    return nil
}
```

## Integration with Real Systems

In production applications, middleware can integrate with:

### Observability Platforms

```go
type DatadogMiddleware struct {
    client *statsd.Client
}

func (m *DatadogMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
    start := time.Now()
    err := next()
    m.client.Timing("llm.latency", time.Since(start))
    m.client.Incr("llm.calls", []string{fmt.Sprintf("model:%s", execCtx.Model)}, 1)
    return err
}
```

### Logging Frameworks

```go
type StructuredLoggingMiddleware struct {
    logger *zap.Logger
}

func (m *StructuredLoggingMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
    m.logger.Info("pipeline_execution_start",
        zap.String("user_id", execCtx.UserID),
        zap.Int("message_count", len(execCtx.Messages)),
    )
    err := next()
    m.logger.Info("pipeline_execution_end",
        zap.Error(err),
        zap.Float64("cost", execCtx.CostInfo.TotalCost),
    )
    return err
}
```

### Authentication/Authorization

```go
type AuthMiddleware struct {
    authService AuthService
}

func (m *AuthMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
    if !m.authService.HasPermission(execCtx.UserID, "llm.access") {
        return errors.New("unauthorized")
    }
    return next()
}
```

## Best Practices

1. **Keep middleware focused**: Each middleware should have a single responsibility
2. **Order matters**: Place context-modifying middleware before logging/metrics
3. **Error handling**: Always propagate errors from `next()`
4. **Performance**: Minimize overhead in middleware, especially for streaming
5. **Immutability**: Be careful when modifying `ExecutionContext` fields
6. **Nil checks**: Always check if `Response` is nil before accessing

## Advanced Patterns

### Conditional Execution

```go
func (m *CachingMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
    cacheKey := generateKey(execCtx.Messages)
    
    // Check cache
    if cached, found := m.cache.Get(cacheKey); found {
        execCtx.Response = cached
        return nil // Skip LLM call
    }
    
    // Execute and cache
    err := next()
    if err == nil && execCtx.Response != nil {
        m.cache.Set(cacheKey, execCtx.Response)
    }
    return err
}
```

### Error Handling with Retry

```go
func (m *RetryMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
    var err error
    for attempt := 0; attempt < m.maxRetries; attempt++ {
        err = next()
        if err == nil {
            return nil
        }
        if !isRetryable(err) {
            return err
        }
        time.Sleep(m.backoff * time.Duration(attempt+1))
    }
    return err
}
```

## See Also

- [SDK Documentation](../../README.md)
- [PipelineBuilder API](../../../docs/api/sdk.md)
- [Runtime Pipeline Architecture](../../../docs/architecture/runtime-pipeline.md)
- [Streaming Example](../streaming/README.md)
- [Observability Example](../observability/README.md)
