---
layout: default
title: SDK Architecture
nav_order: 1
parent: SDK Explanation
grand_parent: SDK
---

# SDK Architecture

Understanding the overall design and component relationships of the PromptKit SDK.

## Design Goals

The SDK is designed with these principles:

1. **Simplicity First** - Easy tasks should be easy, complex tasks should be possible
2. **Type Safety** - Leverage Go's type system for compile-time guarantees
3. **Composability** - Small, focused components that work together
4. **Extensibility** - Support custom middleware, validators, and stores
5. **Production Ready** - Built-in logging, metrics, error handling

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                   Application Layer                      │
│  ┌─────────────┐    ┌──────────────┐   ┌────────────┐  │
│  │ Your App    │────│ Conversation │───│ Response   │  │
│  └─────────────┘    │  Manager     │   └────────────┘  │
└─────────────────────┴──────────────┴─────────────────── │
                             │                              
┌─────────────────────────────────────────────────────────┐
│                   SDK Core Layer                         │
│  ┌─────────────┐    ┌──────────────┐   ┌────────────┐  │
│  │ Conversation│────│   Pipeline   │───│   Pack     │  │
│  │  Instance   │    │   Builder    │   │  Manager   │  │
│  └─────────────┘    └──────────────┘   └────────────┘  │
└──────────────────────────────────────────────────────────┘
                             │                              
┌─────────────────────────────────────────────────────────┐
│                   Runtime Layer                          │
│  ┌──────────┐  ┌───────────┐  ┌─────────┐  ┌─────────┐ │
│  │ Provider │  │ Middleware│  │  Tools  │  │  State  │ │
│  └──────────┘  └───────────┘  └─────────┘  └─────────┘ │
└──────────────────────────────────────────────────────────┘
```

## Core Components

### ConversationManager

The main entry point for SDK usage. Manages conversation lifecycle and configuration.

**Responsibilities:**
- Load and validate PromptPacks
- Create conversation instances
- Configure providers and state stores
- Register tools and validators

**Key Design:**
- Immutable after construction (thread-safe)
- Factory pattern for conversations
- Builder pattern for configuration

```go
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithStateStore(store),
    sdk.WithToolRegistry(registry),
)
```

### Conversation

Represents a single conversation thread with state and history.

**Responsibilities:**
- Send messages and receive responses
- Maintain conversation state
- Handle streaming
- Execute pipeline for each message

**Key Design:**
- Each conversation is isolated
- State is automatically persisted
- Thread-safe for concurrent use

```go
conv, _ := manager.NewConversation(ctx, pack, config)
response, _ := conv.Send(ctx, "Hello")
```

### Pipeline

Processes requests through a chain of middleware.

**Responsibilities:**
- Execute middleware chain
- Call provider
- Handle errors
- Collect metrics

**Key Design:**
- Middleware uses handler pattern
- Pipeline is immutable
- Supports both built-in and custom middleware

```go
pipe := sdk.NewPipelineBuilder().
    WithProvider(provider).
    Use(loggingMiddleware).
    Use(validationMiddleware).
    Build()
```

### Pack

Compiled prompt configuration and metadata.

**Responsibilities:**
- Define prompt templates
- Configure model parameters
- Specify available tools
- Validate configurations

**Key Design:**
- JSON-based format
- Versioned schema
- Compiled and validated at load time

```json
{
  "version": "1.0",
  "prompts": {
    "assistant": {
      "system_prompt": "You are helpful...",
      "available_tools": ["search"],
      "model_config": {"temperature": 0.7}
    }
  }
}
```

## Component Interactions

### Message Flow

1. **Application → ConversationManager**
   ```go
   conv, _ := manager.NewConversation(ctx, pack, config)
   ```
   - Manager creates conversation with pack configuration
   - Loads state if ConversationID provided

2. **Application → Conversation**
   ```go
   response, _ := conv.Send(ctx, "Hello")
   ```
   - Conversation creates pipeline request
   - Adds message to conversation history

3. **Conversation → Pipeline**
   ```
   Request → Middleware → Provider → Response
   ```
   - Pipeline executes middleware chain
   - Calls provider with processed request
   - Returns response through middleware

4. **Pipeline → Provider**
   ```go
   response, _ := provider.Complete(ctx, request)
   ```
   - Provider makes API call
   - Handles streaming if requested
   - Returns normalized response

5. **Provider → Conversation**
   ```
   Response → Update State → Save State
   ```
   - Conversation receives response
   - Updates message history
   - Persists state if StateStore configured

## Two API Levels

The SDK provides two levels of abstraction:

### High-Level API (ConversationManager)

**Use When:**
- Building chat applications
- Need automatic state management
- Want pack-based configuration
- Simple tool integration

**Benefits:**
- Minimal boilerplate
- Automatic state persistence
- Pack-based configuration
- Built-in best practices

```go
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithStateStore(store),
)
pack, _ := manager.LoadPack("./assistant.pack.json")
conv, _ := manager.NewConversation(ctx, pack, config)
response, _ := conv.Send(ctx, "Hello")
```

### Low-Level API (PipelineBuilder)

**Use When:**
- Need full control over processing
- Custom middleware requirements
- Non-conversational use cases
- Advanced validation needs

**Benefits:**
- Maximum flexibility
- Custom middleware
- Fine-grained control
- No pack requirement

```go
pipe := sdk.NewPipelineBuilder().
    WithProvider(provider).
    WithSystemPrompt("You are...").
    Use(customMiddleware).
    WithValidator(customValidator).
    Build()

request := &pipeline.Request{UserMessage: "Hello"}
response, _ := pipe.Execute(ctx, request)
```

## State Management Design

### State Store Interface

```go
type StateStore interface {
    Get(ctx context.Context, key string) (*State, error)
    Set(ctx context.Context, key string, state *State) error
    Delete(ctx context.Context, key string) error
    List(ctx context.Context) ([]*State, error)
}
```

### State Structure

```go
type State struct {
    ConversationID string
    UserID         string
    Messages       []Message
    TokenCount     int
    TotalCost      float64
    Metadata       map[string]interface{}
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

### State Implementations

**Memory Store**
- In-process storage
- Fast, no external dependencies
- Lost on restart

**Redis Store**
- Distributed storage
- Scales across instances
- Persistent and fast

**PostgreSQL Store**
- Database-backed
- Queryable history
- Full persistence

## Provider Abstraction

### Provider Interface

```go
type Provider interface {
    Complete(ctx context.Context, req *Request) (*Response, error)
    CompleteStream(ctx context.Context, req *Request) (<-chan *StreamChunk, error)
    Name() string
    SupportsStreaming() bool
}
```

### Design Rationale

**Why Provider Interface?**
1. Multiple LLM support without code changes
2. Easy testing with mock providers
3. Provider-agnostic application code
4. Future-proof for new providers

**Provider Implementation:**
- Normalize API differences
- Handle rate limiting
- Convert errors to common format
- Support provider-specific features

## Error Handling Strategy

### Error Types

```go
// User errors - invalid input
type ValidationError struct {
    Field   string
    Message string
}

// Provider errors - API issues
type ProviderError struct {
    Provider string
    Code     int
    Message  string
    Retryable bool
}

// State errors - persistence issues
type StateError struct {
    Operation string
    Key       string
    Cause     error
}
```

### Error Propagation

```
Provider Error → Pipeline → Middleware → Conversation → Application
```

Each layer can:
- Wrap error with context
- Transform error type
- Retry if appropriate
- Log for debugging

## Extension Points

### Custom Middleware

```go
func customMiddleware(next pipeline.Handler) pipeline.Handler {
    return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
        // Pre-processing
        resp, err := next.Handle(ctx, req)
        // Post-processing
        return resp, err
    })
}
```

### Custom Validators

```go
func customValidator(ctx context.Context, req *pipeline.Request) error {
    // Validation logic
    return nil
}
```

### Custom State Store

```go
type CustomStore struct {
    // Your implementation
}

func (s *CustomStore) Get(ctx context.Context, key string) (*State, error) {
    // Your logic
}
// Implement other methods...
```

## Performance Considerations

### Request Lifecycle

1. **Validation** (~1ms)
   - Parameter checking
   - Input validation

2. **Middleware** (~5ms)
   - Logging
   - Metrics
   - Custom logic

3. **Provider Call** (500-3000ms)
   - Network request
   - LLM processing
   - Response parsing

4. **State Persistence** (~10ms)
   - Serialize state
   - Store in backend

### Optimization Strategies

**Caching:**
```go
// Cache frequently used packs
pack, _ := manager.LoadPack("./assistant.pack.json")
// Reuse across conversations
```

**Connection Pooling:**
```go
// Providers maintain HTTP connection pools
provider := providers.NewOpenAIProvider(apiKey, model, false)
```

**Parallel Processing:**
```go
// Multiple conversations can run concurrently
go conv1.Send(ctx, msg1)
go conv2.Send(ctx, msg2)
```

## Thread Safety

### Thread-Safe Components

- **ConversationManager** - Immutable after creation
- **Pack** - Immutable after loading
- **Provider** - Concurrent requests supported
- **StateStore** - Implementations must be thread-safe

### Not Thread-Safe

- **Conversation** - Use mutex if sharing across goroutines
- **PipelineBuilder** - Build in single goroutine

## Testing Strategy

### Unit Testing

```go
// Mock provider
mockProvider := &MockProvider{
    Response: &Response{Content: "Test response"},
}

manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(mockProvider),
)

// Test conversation logic
```

### Integration Testing

```go
// Use real provider with test credentials
provider := providers.NewOpenAIProvider(testAPIKey, model, false)

// Test full flow
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
)
```

## Design Patterns Used

1. **Builder Pattern** - ConversationManager, PipelineBuilder
2. **Factory Pattern** - Conversation creation
3. **Strategy Pattern** - Provider abstraction
4. **Chain of Responsibility** - Middleware pipeline
5. **Observer Pattern** - Streaming responses
6. **Repository Pattern** - State stores

## See Also

- [Pipeline Architecture](pipeline-architecture.md)
- [API Design Philosophy](api-design.md)
- [Provider Abstraction](provider-abstraction.md)
