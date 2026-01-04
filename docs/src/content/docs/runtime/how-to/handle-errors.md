---
title: Handle Errors
sidebar:
  order: 5
---
Implement robust error handling for production pipelines.

## Goal

Handle errors gracefully and implement retry logic.

## Quick Start

```go
result, err := pipe.Execute(ctx, "user", "Your message")
if err != nil {
    // Check error type
    if isRateLimitError(err) {
        log.Println("Rate limited, retry later")
    } else if isTimeoutError(err) {
        log.Println("Request timed out")
    } else {
        log.Printf("Error: %v", err)
    }
}
```

## Common Error Types

### Timeout Errors

```go
import "context"

// Set timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

result, err := pipe.Execute(ctx, "user", "Your message")
if err != nil {
    if errors.Is(err, context.DeadlineExceeded) {
        log.Println("Request timed out")
        // Retry with longer timeout or simplify request
    }
}
```

### Rate Limit Errors

```go
result, err := provider.Complete(ctx, messages, config)
if err != nil {
    if strings.Contains(err.Error(), "rate_limit") {
        log.Println("Rate limited")
        // Wait and retry
        time.Sleep(5 * time.Second)
        result, err = provider.Complete(ctx, messages, config)
    }
}
```

### Authentication Errors

```go
result, err := provider.Complete(ctx, messages, config)
if err != nil {
    if strings.Contains(err.Error(), "authentication") ||
       strings.Contains(err.Error(), "invalid_api_key") {
        log.Fatal("Invalid API key - check environment variable")
    }
}
```

### Tool Execution Errors

```go
result, err := pipe.Execute(ctx, "user", "Use the search tool")
if err != nil {
    if strings.Contains(err.Error(), "tool") {
        log.Printf("Tool error: %v", err)
        // Check tool configuration
        // Verify tool is registered
        // Test tool separately
    }
}
```

## Retry Strategies

### Exponential Backoff

```go
func executeWithRetry(pipe *pipeline.Pipeline, ctx context.Context, role, content string, maxRetries int) (*pipeline.PipelineResult, error) {
    var result *pipeline.PipelineResult
    var err error
    
    for i := 0; i < maxRetries; i++ {
        result, err = pipe.Execute(ctx, role, content)
        if err == nil {
            return result, nil
        }
        
        // Check if retryable
        if !isRetryableError(err) {
            return nil, err
        }
        
        // Exponential backoff
        backoff := time.Duration(1<<uint(i)) * time.Second
        log.Printf("Retry %d/%d after %v: %v", i+1, maxRetries, backoff, err)
        time.Sleep(backoff)
    }
    
    return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, err)
}

func isRetryableError(err error) bool {
    errStr := err.Error()
    return strings.Contains(errStr, "rate_limit") ||
           strings.Contains(errStr, "timeout") ||
           strings.Contains(errStr, "503") ||
           strings.Contains(errStr, "502")
}
```

### Fixed Retry Delay

```go
func executeWithFixedRetry(pipe *pipeline.Pipeline, ctx context.Context, role, content string, maxRetries int, delay time.Duration) (*pipeline.PipelineResult, error) {
    for i := 0; i < maxRetries; i++ {
        result, err := pipe.Execute(ctx, role, content)
        if err == nil {
            return result, nil
        }
        
        if i < maxRetries-1 {
            log.Printf("Attempt %d failed, retrying in %v: %v", i+1, delay, err)
            time.Sleep(delay)
        }
    }
    
    return nil, fmt.Errorf("failed after %d retries", maxRetries)
}
```

## Provider Fallback

### Multi-Provider Strategy

```go
type ProviderPool struct {
    providers []types.Provider
    current   int
}

func (p *ProviderPool) Execute(ctx context.Context, messages []types.Message, config *types.ProviderConfig) (*types.ProviderResponse, error) {
    var lastErr error
    
    // Try each provider
    for i := 0; i < len(p.providers); i++ {
        provider := p.providers[(p.current+i)%len(p.providers)]
        
        result, err := provider.Complete(ctx, messages, config)
        if err == nil {
            // Success - update current
            p.current = (p.current + i) % len(p.providers)
            return result, nil
        }
        
        lastErr = err
        log.Printf("Provider %d failed: %v", i, err)
    }
    
    return nil, fmt.Errorf("all providers failed: %w", lastErr)
}
```

### With Circuit Breaker

```go
type CircuitBreaker struct {
    failures    int
    maxFailures int
    timeout     time.Duration
    lastFailure time.Time
}

func (cb *CircuitBreaker) Call(fn func() error) error {
    // Check if circuit is open
    if cb.failures >= cb.maxFailures {
        if time.Since(cb.lastFailure) < cb.timeout {
            return fmt.Errorf("circuit breaker open")
        }
        // Reset after timeout
        cb.failures = 0
    }
    
    // Call function
    err := fn()
    if err != nil {
        cb.failures++
        cb.lastFailure = time.Now()
        return err
    }
    
    // Success - reset
    cb.failures = 0
    return nil
}

// Usage
cb := &CircuitBreaker{maxFailures: 3, timeout: 30 * time.Second}
err := cb.Call(func() error {
    _, err := provider.Complete(ctx, messages, config)
    return err
})
```

## Partial Results

### Handle Incomplete Responses

```go
result, err := pipe.Execute(ctx, "user", "Generate a long document")
if err != nil {
    if strings.Contains(err.Error(), "timeout") && result != nil {
        // Use partial result
        log.Println("Request timed out, using partial response")
        log.Printf("Partial: %s", result.Response.Content)
        return result, nil
    }
    return nil, err
}
```

### Streaming with Error Recovery

```go
stream, err := pipe.ExecuteStream(ctx, "user", "Long request")
if err != nil {
    return err
}
defer stream.Close()

var accumulated string
for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Printf("Stream error: %v", err)
        // Use accumulated content
        if len(accumulated) > 0 {
            log.Printf("Recovered partial content: %s", accumulated)
            return nil
        }
        return err
    }
    
    accumulated += chunk.Content
}
```

## Error Monitoring

### Log Structured Errors

```go
import "github.com/AltairaLabs/PromptKit/runtime/logger"

func executeWithLogging(pipe *pipeline.Pipeline, ctx context.Context, sessionID, role, content string) (*pipeline.PipelineResult, error) {
    start := time.Now()
    result, err := pipe.ExecuteWithContext(ctx, sessionID, role, content)
    duration := time.Since(start)
    
    if err != nil {
        logger.Error("Pipeline execution failed",
            "session_id", sessionID,
            "role", role,
            "duration", duration,
            "error", err,
        )
        return nil, err
    }
    
    logger.Info("Pipeline execution succeeded",
        "session_id", sessionID,
        "duration", duration,
        "tokens", result.Response.Usage.TotalTokens,
        "cost", result.Cost.TotalCost,
    )
    
    return result, nil
}
```

### Error Metrics

```go
type ErrorMetrics struct {
    timeouts       int64
    rateLimits     int64
    authErrors     int64
    toolErrors     int64
    otherErrors    int64
    totalRequests  int64
    successRequests int64
}

func (m *ErrorMetrics) RecordError(err error) {
    atomic.AddInt64(&m.totalRequests, 1)
    
    if err == nil {
        atomic.AddInt64(&m.successRequests, 1)
        return
    }
    
    errStr := err.Error()
    switch {
    case strings.Contains(errStr, "timeout"):
        atomic.AddInt64(&m.timeouts, 1)
    case strings.Contains(errStr, "rate_limit"):
        atomic.AddInt64(&m.rateLimits, 1)
    case strings.Contains(errStr, "authentication"):
        atomic.AddInt64(&m.authErrors, 1)
    case strings.Contains(errStr, "tool"):
        atomic.AddInt64(&m.toolErrors, 1)
    default:
        atomic.AddInt64(&m.otherErrors, 1)
    }
}

func (m *ErrorMetrics) Report() {
    total := atomic.LoadInt64(&m.totalRequests)
    success := atomic.LoadInt64(&m.successRequests)
    successRate := float64(success) / float64(total) * 100
    
    log.Printf("Error Metrics:")
    log.Printf("  Total: %d", total)
    log.Printf("  Success: %d (%.2f%%)", success, successRate)
    log.Printf("  Timeouts: %d", atomic.LoadInt64(&m.timeouts))
    log.Printf("  Rate Limits: %d", atomic.LoadInt64(&m.rateLimits))
    log.Printf("  Auth Errors: %d", atomic.LoadInt64(&m.authErrors))
    log.Printf("  Tool Errors: %d", atomic.LoadInt64(&m.toolErrors))
    log.Printf("  Other: %d", atomic.LoadInt64(&m.otherErrors))
}
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "strings"
    "time"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/providers/anthropic"
)

func main() {
    // Create providers
    openaiProvider := openai.NewOpenAIProvider(
        "openai", "gpt-4o-mini", "", openai.DefaultProviderDefaults(), false,
    )
    defer openaiProvider.Close()
    
    claudeProvider := anthropic.NewAnthropicProvider(
        "claude", "claude-3-5-haiku-20241022", "", anthropic.DefaultProviderDefaults(), false,
    )
    defer claudeProvider.Close()
    
    // Provider pool with fallback
    providers := []types.Provider{openaiProvider, claudeProvider}
    
    config := &middleware.ProviderMiddlewareConfig{
        MaxTokens:   1000,
        Temperature: 0.7,
    }
    
    // Try each provider with retry
    var result *pipeline.PipelineResult
    var err error
    
    for _, provider := range providers {
        pipe := pipeline.NewPipeline(
            middleware.ProviderMiddleware(provider, nil, nil, config),
        )
        defer pipe.Shutdown(context.Background())
        
        // Retry with backoff
        result, err = executeWithRetry(pipe, context.Background(), "user", "Hello!", 3)
        if err == nil {
            break
        }
        
        log.Printf("Provider %s failed: %v", provider.GetProviderName(), err)
    }
    
    if err != nil {
        log.Fatal("All providers failed")
    }
    
    log.Printf("Success: %s", result.Response.Content)
}

func executeWithRetry(pipe *pipeline.Pipeline, ctx context.Context, role, content string, maxRetries int) (*pipeline.PipelineResult, error) {
    for i := 0; i < maxRetries; i++ {
        result, err := pipe.Execute(ctx, role, content)
        if err == nil {
            return result, nil
        }
        
        if !isRetryableError(err) {
            return nil, err
        }
        
        if i < maxRetries-1 {
            backoff := time.Duration(1<<uint(i)) * time.Second
            log.Printf("Retry %d/%d after %v", i+1, maxRetries, backoff)
            time.Sleep(backoff)
        }
    }
    
    return nil, fmt.Errorf("failed after %d retries", maxRetries)
}

func isRetryableError(err error) bool {
    errStr := err.Error()
    return strings.Contains(errStr, "rate_limit") ||
           strings.Contains(errStr, "timeout") ||
           strings.Contains(errStr, "503")
}
```

## Troubleshooting

### Issue: Persistent Timeouts

**Problem**: Requests consistently timing out.

**Solutions**:

1. Increase timeout:
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
   defer cancel()
   ```

2. Reduce response size:
   ```go
   config := &middleware.ProviderMiddlewareConfig{
       MaxTokens: 500,  // Smaller response
   }
   ```

3. Simplify prompt:
   ```go
   // Break complex request into smaller parts
   ```

### Issue: Rate Limits

**Problem**: Frequent rate limit errors.

**Solutions**:

1. Add retry with backoff (see examples above)

2. Reduce concurrency:
   ```go
   config := &pipeline.RuntimeConfig{
       MaxConcurrentRequests: 1,  // Sequential
   }
   ```

3. Use rate limiter:
   ```go
   limiter := rate.NewLimiter(rate.Limit(10), 1)  // 10 req/sec
   limiter.Wait(ctx)
   result, err := pipe.Execute(ctx, "user", content)
   ```

### Issue: Memory Leaks

**Problem**: Memory grows over time.

**Solutions**:

1. Always close resources:
   ```go
   defer provider.Close()
   defer pipe.Shutdown(ctx)
   ```

2. Clean up state:
   ```go
   // Limit message history
   if len(messages) > 50 {
       messages = messages[len(messages)-50:]
   }
   ```

## Best Practices

1. **Set timeouts on all requests**:
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()
   ```

2. **Implement retry logic for transient errors**:
   ```go
   result, err := executeWithRetry(pipe, ctx, "user", content, 3)
   ```

3. **Use multiple providers for redundancy**:
   ```go
   providers := []types.Provider{openai, claude, gemini}
   ```

4. **Log errors with context**:
   ```go
   logger.Error("Request failed", "session", sessionID, "error", err)
   ```

5. **Monitor error rates**:
   ```go
   metrics.RecordError(err)
   if metrics.ErrorRate() > 0.1 {
       alert("High error rate")
   }
   ```

6. **Handle partial results**:
   ```go
   if err != nil && result != nil {
       // Use partial content
   }
   ```

7. **Fail fast on non-retryable errors**:
   ```go
   if strings.Contains(err.Error(), "authentication") {
       log.Fatal("Invalid API key")
   }
   ```

## Next Steps

- [Streaming Responses](streaming-responses) - Handle streams
- [Monitor Costs](monitor-costs) - Track usage
- [Setup Providers](setup-providers) - Provider config

## See Also

- [Pipeline Reference](../reference/pipeline) - Error details
- [Providers Reference](../reference/providers) - Provider errors
