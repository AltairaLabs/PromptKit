---
layout: docs
title: Middleware Design
parent: Runtime Explanation
grand_parent: Runtime
nav_order: 3
---

# Middleware Design

Understanding Runtime's composable middleware architecture.

## Overview

Middleware is the core abstraction in Runtime. Every component that processes requests is middleware.

## Core Concept

Middleware wraps execution:

```go
type Middleware interface {
    Before(ctx *ExecutionContext) error
    After(ctx *ExecutionContext) error
}
```

**Before**: Called before next middleware  
**After**: Called after next middleware

Think of it like layers:

```
Request
  ↓
[Middleware 1 Before]
  ↓
[Middleware 2 Before]
  ↓
[Middleware 3 Before]
  ↓
Execute
  ↓
[Middleware 3 After]
  ↓
[Middleware 2 After]
  ↓
[Middleware 1 After]
  ↓
Response
```

## Why Middleware?

### Problem: Cross-Cutting Concerns

Applications need many features:

- Logging
- Authentication
- Validation
- Rate limiting
- Metrics
- Error handling
- State management

Without middleware, code becomes tangled:

```go
func execute(messages) {
    // Logging
    log("Starting...")
    
    // Auth
    if !checkAuth() {
        return error
    }
    
    // Rate limit
    if !checkRate() {
        return error
    }
    
    // Actual work
    result = doWork(messages)
    
    // More logging
    log("Finished")
    
    // Metrics
    recordMetric()
    
    return result
}
```

### Solution: Middleware Chain

With middleware, concerns are separate:

```go
pipeline := NewPipeline(
    LoggingMiddleware(),
    AuthMiddleware(),
    RateLimitMiddleware(),
    ProviderMiddleware(),
)
```

Each middleware focuses on one thing.

## Design Principles

### 1. Single Responsibility

Each middleware does one thing:

**StateMiddleware**: Only manages state  
**TemplateMiddleware**: Only processes templates  
**ValidatorMiddleware**: Only validates content

### 2. Composability

Middleware combines easily:

```go
// Simple pipeline
pipeline := NewPipeline(
    StateMiddleware(...),
    ProviderMiddleware(...),
)

// Add validation
pipeline := NewPipeline(
    StateMiddleware(...),
    ValidatorMiddleware(...),  // Added
    ProviderMiddleware(...),
)

// Add templates
pipeline := NewPipeline(
    StateMiddleware(...),
    TemplateMiddleware(...),   // Added
    ValidatorMiddleware(...),
    ProviderMiddleware(...),
)
```

### 3. Explicit Ordering

Order matters. User controls it:

```go
// Load state first
pipeline := NewPipeline(
    StateMiddleware(...),      // 1. Load history
    TemplateMiddleware(...),   // 2. Apply templates
    ValidatorMiddleware(...),  // 3. Validate
    ProviderMiddleware(...),   // 4. Execute
)
```

Wrong order breaks functionality.

### 4. Immutability

Middleware doesn't mutate input:

```go
// Bad: Mutates
func (m *Middleware) Before(ctx *ExecutionContext) error {
    ctx.Messages[0].Content = "Modified"  // Don't do this!
    return nil
}

// Good: Adds new messages
func (m *Middleware) Before(ctx *ExecutionContext) error {
    ctx.Messages = append(ctx.Messages, newMessage)
    return nil
}
```

## Execution Model

### Pipeline Execution

Pipeline runs middleware in sequence:

```go
func (p *Pipeline) Execute(ctx context.Context, role, content string) (*PipelineResult, error) {
    execCtx := &ExecutionContext{
        Context:  ctx,
        Messages: []Message{{Role: role, Content: content}},
    }
    
    // Run Before hooks
    for _, mw := range p.middleware {
        if err := mw.Before(execCtx); err != nil {
            return nil, err
        }
    }
    
    // Run After hooks (reverse order)
    for i := len(p.middleware) - 1; i >= 0; i-- {
        if err := p.middleware[i].After(execCtx); err != nil {
            return nil, err
        }
    }
    
    return execCtx.Response, nil
}
```

### ExecutionContext

Context carries data through pipeline:

```go
type ExecutionContext struct {
    Context    context.Context
    Messages   []Message
    Response   *ProviderResponse
    Metadata   map[string]any
    SessionID  string
    TemplateID string
}
```

Middleware reads and writes to context.

## Built-In Middleware

### StateMiddleware

**Purpose**: Manage conversation history

**Before**:
- Load history from store
- Add to Messages

**After**:
- Save new messages
- Update store

**Configuration**:
```go
StateMiddleware(store, &StateMiddlewareConfig{
    MaxMessages: 10,
    TTL:         24 * time.Hour,
})
```

### TemplateMiddleware

**Purpose**: Apply variable substitution

**Before**:
- Look up template
- Substitute variables
- Update Messages

**After**: Nothing

**Configuration**:
```go
TemplateMiddleware(templates, &TemplateConfig{
    DefaultTemplate: "assistant",
})
```

### ValidatorMiddleware

**Purpose**: Content validation

**Before**:
- Run validators on input
- Check for violations

**After**:
- Run validators on output
- Check for violations

**Configuration**:
```go
ValidatorMiddleware(validators, &ValidatorConfig{
    FailOnViolation: true,
})
```

### ProviderMiddleware

**Purpose**: LLM execution

**Before**: Nothing

**After**:
- Call provider
- Set Response
- Track cost

**Configuration**:
```go
ProviderMiddleware(provider, rateLimiter, costTracker, &ProviderConfig{
    MaxTokens:   4096,
    Temperature: 0.7,
})
```

## Custom Middleware

### Implementing Middleware

Create a struct that implements the interface:

```go
type LoggingMiddleware struct {
    logger *log.Logger
}

func (m *LoggingMiddleware) Before(ctx *ExecutionContext) error {
    m.logger.Printf("Request: %d messages", len(ctx.Messages))
    return nil
}

func (m *LoggingMiddleware) After(ctx *ExecutionContext) error {
    if ctx.Response != nil {
        m.logger.Printf("Response: %s", ctx.Response.Content)
    }
    return nil
}
```

### Example: Metrics Middleware

Track execution time:

```go
type MetricsMiddleware struct {
    startTime time.Time
}

func (m *MetricsMiddleware) Before(ctx *ExecutionContext) error {
    m.startTime = time.Now()
    return nil
}

func (m *MetricsMiddleware) After(ctx *ExecutionContext) error {
    duration := time.Since(m.startTime)
    metrics.RecordLatency(duration)
    return nil
}
```

### Example: Auth Middleware

Check authentication:

```go
type AuthMiddleware struct {
    allowedUsers map[string]bool
}

func (m *AuthMiddleware) Before(ctx *ExecutionContext) error {
    userID := ctx.Metadata["user_id"].(string)
    if !m.allowedUsers[userID] {
        return errors.New("unauthorized")
    }
    return nil
}

func (m *AuthMiddleware) After(ctx *ExecutionContext) error {
    return nil  // Nothing to do after
}
```

### Example: Retry Middleware

Retry on failure:

```go
type RetryMiddleware struct {
    maxRetries int
    inner      Middleware
}

func (m *RetryMiddleware) Before(ctx *ExecutionContext) error {
    var err error
    for i := 0; i < m.maxRetries; i++ {
        err = m.inner.Before(ctx)
        if err == nil {
            return nil
        }
        time.Sleep(time.Second * time.Duration(i+1))
    }
    return err
}

func (m *RetryMiddleware) After(ctx *ExecutionContext) error {
    return m.inner.After(ctx)
}
```

## Design Patterns

### Pre-Processing

Middleware that modifies input before execution:

- **Authentication**: Check credentials
- **Rate Limiting**: Throttle requests
- **Input Validation**: Check message format
- **Template Expansion**: Substitute variables
- **State Loading**: Load conversation history

### Post-Processing

Middleware that processes output after execution:

- **Response Validation**: Check output content
- **Logging**: Record responses
- **Metrics**: Track performance
- **State Saving**: Persist conversation
- **Caching**: Store responses

### Pass-Through

Middleware that only observes:

```go
type ObserverMiddleware struct{}

func (m *ObserverMiddleware) Before(ctx *ExecutionContext) error {
    log.Printf("Observed: %d messages", len(ctx.Messages))
    return nil  // Don't modify anything
}

func (m *ObserverMiddleware) After(ctx *ExecutionContext) error {
    return nil
}
```

### Early Exit

Middleware can stop execution:

```go
type CacheMiddleware struct {
    cache map[string]string
}

func (m *CacheMiddleware) Before(ctx *ExecutionContext) error {
    key := hash(ctx.Messages)
    if cached, ok := m.cache[key]; ok {
        ctx.Response = &ProviderResponse{Content: cached}
        return ErrSkipRemaining  // Stop pipeline
    }
    return nil
}
```

## Ordering Best Practices

### Recommended Order

```go
pipeline := NewPipeline(
    1. AuthMiddleware(),           // Security first
    2. RateLimitMiddleware(),      // Throttle early
    3. CacheMiddleware(),          // Check cache
    4. StateMiddleware(),          // Load history
    5. TemplateMiddleware(),       // Expand templates
    6. InputValidatorMiddleware(), // Validate input
    7. ProviderMiddleware(),       // Execute
    8. OutputValidatorMiddleware(),// Validate output
    9. MetricsMiddleware(),        // Track metrics
)
```

### Order Principles

**Security first**: Auth and rate limiting before anything else  
**State early**: Load history before template expansion  
**Validation twice**: Before and after execution  
**Metrics last**: Measure everything

### Common Mistakes

**Wrong**: Template before State
```go
// Bad: Templates won't see history
pipeline := NewPipeline(
    TemplateMiddleware(),
    StateMiddleware(),
    ProviderMiddleware(),
)
```

**Right**: State before Template
```go
// Good: Templates see full history
pipeline := NewPipeline(
    StateMiddleware(),
    TemplateMiddleware(),
    ProviderMiddleware(),
)
```

## Design Decisions

### Why Before/After Hooks?

**Decision**: Middleware has two methods: Before and After

**Rationale**:
- Clean separation: Setup vs cleanup
- Symmetric: Wrap execution
- Flexible: Can act on both sides

**Alternative considered**: Single Execute() method. Rejected because it's harder to compose and reason about.

### Why Ordered Chain?

**Decision**: Middleware runs in explicit order

**Rationale**:
- Predictable execution
- Clear dependencies
- Easy to debug

**Alternative considered**: Dependency injection to determine order. Rejected as too complex and implicit.

### Why Immutable Context?

**Decision**: ExecutionContext fields shouldn't be mutated

**Rationale**:
- Predictable behavior
- Safe for concurrency
- Easier debugging

**Trade-off**: Must append to Messages rather than modifying in place. This is acceptable overhead.

### Why No Middleware Registry?

**Decision**: Pipeline explicitly lists middleware

**Rationale**:
- Clear what's running
- No hidden middleware
- Explicit dependencies

**Alternative considered**: Global registry where middleware auto-registers. Rejected as too implicit and hard to test.

## Performance Considerations

### Middleware Overhead

Each middleware adds ~10-50µs:

```go
// Minimal middleware
type NoOpMiddleware struct{}

func (m *NoOpMiddleware) Before(ctx *ExecutionContext) error {
    return nil  // ~10µs
}

func (m *NoOpMiddleware) After(ctx *ExecutionContext) error {
    return nil  // ~10µs
}
```

**Impact**: For 5 middleware, ~50-250µs total. Negligible compared to LLM call (500ms-5s).

### State Loading Cost

StateMiddleware can be expensive:

```go
// Expensive: Loads all history
StateMiddleware(store, &StateMiddlewareConfig{
    MaxMessages: 1000,  // Large!
})
```

**Solution**: Limit history size:

```go
StateMiddleware(store, &StateMiddlewareConfig{
    MaxMessages: 10,  // Reasonable
    TTL:         24 * time.Hour,
})
```

### Validation Cost

ValidatorMiddleware runs on every message:

```go
// Expensive: Many validators
ValidatorMiddleware([]Validator{
    BannedWordsValidator(),
    ToxicityValidator(),
    PIIValidator(),
    LengthValidator(),
}, config)
```

**Solution**: Run validators in parallel (when independent).

## Testing Middleware

### Unit Testing

Test middleware in isolation:

```go
func TestLoggingMiddleware(t *testing.T) {
    mw := NewLoggingMiddleware()
    ctx := &ExecutionContext{
        Messages: []Message{{Role: "user", Content: "test"}},
    }
    
    err := mw.Before(ctx)
    assert.NoError(t, err)
    
    ctx.Response = &ProviderResponse{Content: "response"}
    err = mw.After(ctx)
    assert.NoError(t, err)
}
```

### Integration Testing

Test middleware in pipeline:

```go
func TestPipelineWithMiddleware(t *testing.T) {
    mockProvider := mock.NewMockProvider()
    pipeline := NewPipeline(
        StateMiddleware(store, config),
        ProviderMiddleware(mockProvider, nil, nil, providerConfig),
    )
    
    result, err := pipeline.Execute(ctx, "user", "test")
    assert.NoError(t, err)
    assert.NotNil(t, result)
}
```

## Summary

Middleware design provides:

✅ **Composability**: Mix and match middleware  
✅ **Single Responsibility**: Each middleware has one job  
✅ **Explicit Ordering**: Clear execution flow  
✅ **Testability**: Test in isolation or composed  
✅ **Extensibility**: Easy to add custom middleware  

## Related Topics

- [Pipeline Architecture](pipeline-architecture.md) - How middleware forms pipelines
- [State Management](state-management.md) - StateMiddleware details
- [Middleware Reference](../reference/middleware.md) - Complete API

## Further Reading

- Chain of Responsibility pattern (Gang of Four)
- Interceptor pattern
- Express.js middleware design
- ASP.NET Core middleware pipeline
