---
title: Pipeline Architecture
docType: explanation
order: 1
---
# Pipeline Architecture

Understanding Runtime's middleware-based pipeline design.

## Overview

Runtime uses a **middleware pattern** for processing LLM requests. This architecture provides flexibility, composability, and clear separation of concerns.

## Core Concept

A pipeline is a stack of middleware layers that process requests in sequence:

```
Request → [Middleware 1] → [Middleware 2] → [Middleware N] → Provider → Response
```

Each middleware can:
- Inspect and modify the request
- Pass control to the next middleware
- Process the response
- Handle errors

## Pipeline Structure

### Basic Pipeline

```go
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, nil, config),
)
```

This creates a minimal pipeline with just provider middleware.

### Multi-Layer Pipeline

```go
pipe := pipeline.NewPipeline(
    middleware.StateMiddleware(store),           // Layer 1: Load state
    middleware.TemplateMiddleware(templates),    // Layer 2: Apply templates
    middleware.ValidatorMiddleware(validators),  // Layer 3: Validate content
    middleware.ProviderMiddleware(provider, toolRegistry, policy, config),  // Layer 4: Call LLM
)
```

Middleware executes in order:
1. **State**: Loads conversation history
2. **Template**: Applies prompt templates
3. **Validator**: Checks content safety
4. **Provider**: Sends request to LLM

## Why Middleware?

### Problem: Monolithic Design

Without middleware, you'd need:

```go
// Bad: Everything in one function
func Execute(prompt string) string {
    // Load state
    history := loadFromRedis(sessionID)
    
    // Apply template
    templated := applyTemplate(prompt, history)
    
    // Validate
    if containsBannedWords(templated) {
        return "Error: banned content"
    }
    
    // Call LLM
    response := openai.Complete(templated)
    
    // Save state
    saveToRedis(sessionID, response)
    
    return response
}
```

**Problems**:
- Hard to test individual components
- Can't reuse logic
- Difficult to add features
- No flexibility in ordering

### Solution: Middleware Pattern

With middleware:

```go
// Good: Composable layers
pipe := pipeline.NewPipeline(
    StateMiddleware(store),
    TemplateMiddleware(templates),
    ValidatorMiddleware(validators),
    ProviderMiddleware(provider, nil, nil, config),
)

result, err := pipe.Execute(ctx, "user", prompt)
```

**Benefits**:
- Each middleware is independent
- Easy to test in isolation
- Reusable across pipelines
- Flexible ordering
- Add/remove layers easily

## Middleware Interface

All middleware implements:

```go
type Middleware interface {
    Process(ctx *ExecutionContext, msg *Message) (*ProviderResponse, error)
}
```

### Execution Context

The context flows through all middleware:

```go
type ExecutionContext struct {
    Context         context.Context
    SessionID       string
    Messages        []Message
    ExecutionResult *PipelineResult
    Metadata        map[string]interface{}
}
```

This provides:
- Request context (timeout, cancellation)
- Session identification
- Message history
- Execution metadata

### Message Structure

```go
type Message struct {
    Role    string
    Content string
    ToolCalls []MessageToolCall
}
```

Messages represent the conversation.

## Middleware Types

### State Middleware

**Purpose**: Manage conversation history

```go
StateMiddleware(store StateStore)
```

**Behavior**:
1. Before: Loads previous messages from store
2. Adds new message to history
3. Passes to next middleware
4. After: Saves updated history

**Use case**: Multi-turn conversations, chatbots

### Template Middleware

**Purpose**: Apply prompt templates

```go
TemplateMiddleware(templates TemplateStore)
```

**Behavior**:
1. Looks up template by name
2. Renders template with variables
3. Replaces message content
4. Passes to next middleware

**Use case**: Consistent prompt formatting, dynamic prompts

### Validator Middleware

**Purpose**: Content safety and validation

```go
ValidatorMiddleware(validators ...Validator)
```

**Behavior**:
1. Runs all validators on message
2. If any fails, returns error
3. If all pass, continues to next middleware

**Use case**: Banned word filtering, length limits, content moderation

### Provider Middleware

**Purpose**: Call LLM and handle tools

```go
ProviderMiddleware(provider Provider, tools *ToolRegistry, policy *ToolPolicy, config *ProviderConfig)
```

**Behavior**:
1. Receives all messages
2. Calls provider (OpenAI, Claude, Gemini)
3. Handles tool calls if present
4. Returns LLM response

**Use case**: Core LLM interaction, function calling

## Execution Flow

### Request Path

```
User Input
  ↓
pipe.Execute(ctx, "user", "Hello")
  ↓
StateMiddleware.Process()
  ├─ Load history from Redis
  ├─ Add new message
  └─ Pass to next →
  ↓
ValidatorMiddleware.Process()
  ├─ Check banned words
  ├─ Check length
  └─ Pass to next →
  ↓
ProviderMiddleware.Process()
  ├─ Call OpenAI API
  ├─ Handle tool calls
  └─ Return response ←
  ↓
StateMiddleware (return path)
  └─ Save updated history
  ↓
Result to caller
```

### Response Path

Middleware processes responses in reverse order, allowing for:
- Response transformation
- Logging
- Metrics collection
- State updates

## Design Decisions

### Why Sequential Processing?

**Decision**: Middleware executes in strict order

**Rationale**:
- Predictable behavior
- Easy to reason about
- Clear dependencies (state before validation)
- Simple debugging

**Alternative considered**: Parallel middleware execution was considered but rejected due to complexity and unclear ordering.

### Why Immutable Context?

**Decision**: ExecutionContext is passed by pointer but treated as immutable

**Rationale**:
- Prevents surprising mutations
- Clear data flow
- Easier testing
- Thread-safe patterns

**Trade-off**: Slight performance cost, but worth it for safety.

### Why No Middleware Skipping?

**Decision**: Can't skip middleware based on runtime conditions

**Rationale**:
- Pipeline structure is defined at creation
- Conditional logic belongs inside middleware
- Simpler mental model
- Easier optimization

**Example**:
```go
// Don't skip middleware
if needsValidation {
    pipe := NewPipeline(StateMiddleware(), ValidatorMiddleware(), ProviderMiddleware())
} else {
    pipe := NewPipeline(StateMiddleware(), ProviderMiddleware())
}

// Instead, use conditional middleware
type ConditionalValidator struct {
    enabled bool
    next Middleware
}

func (m *ConditionalValidator) Process(ctx *ExecutionContext, msg *Message) (*ProviderResponse, error) {
    if m.enabled {
        // validate
    }
    return m.next.Process(ctx, msg)
}
```

## Custom Middleware

You can create custom middleware for domain-specific needs:

```go
type LoggingMiddleware struct {
    next Middleware
}

func (m *LoggingMiddleware) Process(ctx *ExecutionContext, msg *Message) (*ProviderResponse, error) {
    start := time.Now()
    log.Printf("Processing: %s", msg.Content)
    
    response, err := m.next.Process(ctx, msg)
    
    duration := time.Since(start)
    if err != nil {
        log.Printf("Error after %v: %v", duration, err)
    } else {
        log.Printf("Success in %v", duration)
    }
    
    return response, err
}
```

## Performance Considerations

### Middleware Overhead

Each middleware adds minimal overhead:
- Function call
- Context passing
- Error checking

For typical workloads (LLM calls taking 1-5 seconds), middleware overhead is negligible (<1ms).

### Memory Usage

Middleware doesn't copy messages by default, reducing memory usage. However:
- State middleware loads full history (can be large)
- Template middleware may create temporary strings
- Validator middleware processes content

**Optimization**: Limit message history to recent messages.

### Concurrency

Pipelines are **not thread-safe**. Create separate pipelines for concurrent requests:

```go
// Don't share pipeline across goroutines
pipe := NewPipeline(...)
go func() { pipe.Execute(...) }()  // ❌ Race condition
go func() { pipe.Execute(...) }()  // ❌ Race condition

// Do create one pipeline per goroutine
func worker() {
    pipe := NewPipeline(...)  // ✓ Thread-local
    pipe.Execute(...)
}
go worker()
go worker()
```

## Comparison to Other Patterns

### vs. Decorator Pattern

**Decorator**: Wraps objects to add behavior

```go
validator := NewValidator(provider)
template := NewTemplate(validator)
state := NewState(template)
```

**Middleware**: Explicitly ordered chain

```go
pipe := NewPipeline(state, template, validator, provider)
```

**Why middleware?** More explicit ordering, easier composition.

### vs. Chain of Responsibility

**Chain of Responsibility**: Each handler decides whether to pass to next

**Middleware**: All handlers process in sequence

**Why middleware?** Simpler for this use case - we always want full processing.

## Summary

Pipeline architecture provides:

✅ **Composability**: Mix and match middleware  
✅ **Separation of Concerns**: Each layer has one job  
✅ **Testability**: Test middleware independently  
✅ **Flexibility**: Add custom middleware easily  
✅ **Maintainability**: Clear, predictable flow  

## Related Topics

- [Middleware Design](middleware-design) - Deep dive into middleware patterns
- [Provider System](provider-system) - How providers work
- [Pipeline Reference](../reference/pipeline) - Complete API

## Further Reading

- Middleware pattern in web frameworks (Express.js, ASP.NET Core)
- Chain of Responsibility pattern (Gang of Four)
- Interceptor pattern in RPC systems (gRPC)
