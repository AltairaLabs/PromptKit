---
title: API Design Philosophy
docType: explanation
order: 4
---
# API Design Philosophy

Understanding the rationale behind SDK design choices.

## Core Principles

### 1. Progressive Disclosure

**Principle:** Simple things should be simple, complex things should be possible.

**Implementation:**

**Level 1: Quick Start** (5 minutes)
```go
manager, _ := sdk.NewConversationManager(sdk.WithProvider(provider))
pack, _ := manager.LoadPack("./assistant.pack.json")
conv, _ := manager.NewConversation(ctx, pack, config)
response, _ := conv.Send(ctx, "Hello")
```

**Level 2: Configuration** (Production)
```go
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithStateStore(store),
    sdk.WithToolRegistry(registry),
    sdk.WithConfig(cfg),
)
```

**Level 3: Custom Pipeline** (Advanced)
```go
pipe := sdk.NewPipelineBuilder().
    WithProvider(provider).
    Use(customMiddleware).
    WithValidator(customValidator).
    Build()
```

### 2. Functional Options Pattern

**Why:** Flexible, extensible, backward-compatible configuration.

```go
// Bad: Parameter explosion
func NewManager(provider Provider, store StateStore, registry ToolRegistry, logger Logger, metrics Metrics) (*Manager, error)

// Good: Functional options
func NewConversationManager(opts ...ManagerOption) (*Manager, error)
```

**Benefits:**
- Add options without breaking changes
- Optional parameters with defaults
- Self-documenting at call site
- Type-safe

**Example:**
```go
type ManagerOption func(*ManagerConfig)

func WithProvider(p Provider) ManagerOption {
    return func(cfg *ManagerConfig) {
        cfg.Provider = p
    }
}

func WithStateStore(s StateStore) ManagerOption {
    return func(cfg *ManagerConfig) {
        cfg.StateStore = s
    }
}

// Usage
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),     // Required
    sdk.WithStateStore(store),      // Optional
    sdk.WithToolRegistry(registry), // Optional
)
```

### 3. Context-First

**Principle:** All operations accept `context.Context` as first parameter.

**Why:**
- Cancellation support
- Timeout control
- Request-scoped values
- Tracing integration

```go
// All major operations
conv.Send(ctx, message)
conv.SendStream(ctx, message)
pipe.Execute(ctx, request)
store.Get(ctx, key)
```

**Example:**
```go
// Timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
response, _ := conv.Send(ctx, message)

// Cancellation
ctx, cancel := context.WithCancel(context.Background())
go func() {
    time.Sleep(5 * time.Second)
    cancel()  // Cancel if user navigates away
}()
response, _ := conv.Send(ctx, message)
```

### 4. Explicit Error Handling

**Principle:** Errors are values, not exceptions.

**Why:**
- Go idiom
- Explicit error handling
- No hidden control flow
- Easier to reason about

```go
// All operations return error
conv, err := manager.NewConversation(ctx, pack, config)
if err != nil {
    return fmt.Errorf("failed to create conversation: %w", err)
}

response, err := conv.Send(ctx, message)
if err != nil {
    return fmt.Errorf("failed to send message: %w", err)
}
```

**Error Types:**
```go
// Specific error types for different failures
type ValidationError struct { /* ... */ }
type ProviderError struct { /* ... */ }
type StateError struct { /* ... */ }

// Check error types
if errors.As(err, &ValidationError{}) {
    // Handle validation error
}
```

### 5. Immutability Where Possible

**Principle:** Immutable objects are thread-safe and easier to reason about.

**Immutable Components:**
- **ConversationManager** - After construction
- **Pack** - After loading
- **Pipeline** - After building
- **Provider** - Configuration frozen

**Mutable Components:**
- **Conversation** - State changes with each message
- **State** - Updated during conversation

**Example:**
```go
// Manager is immutable - thread-safe
manager, _ := sdk.NewConversationManager(opts...)

// Create multiple conversations concurrently
go conv1.Send(ctx, msg1)
go conv2.Send(ctx, msg2)
```

### 6. Fail Fast

**Principle:** Detect errors at build/load time, not runtime.

**Examples:**

**Pack Validation:**
```go
// Invalid pack fails at load time
pack, err := manager.LoadPack("./invalid.pack.json")
// err: "invalid pack: missing required field 'prompts'"
```

**Configuration Validation:**
```go
// Missing provider fails at construction
manager, err := sdk.NewConversationManager()
// err: "provider is required"
```

**Type Safety:**
```go
// Compile-time type checking
conv.Send(ctx, "message")  // ✓ Correct
conv.Send(ctx, 123)        // ✗ Compile error
```

### 7. Provider Abstraction

**Principle:** Provider-agnostic API.

**Why:**
- Switch providers without code changes
- Multi-provider support
- Testing with mocks
- Future-proof

**Example:**
```go
// Same code works with any provider
var provider Provider

if useOpenAI {
    provider = providers.NewOpenAIProvider(key, "gpt-4o", false)
} else if useAnthropic {
    provider = providers.NewAnthropicProvider(key, "claude-3-5-sonnet-20241022", false)
} else {
    provider = providers.NewGeminiProvider(key, "gemini-1.5-pro", false)
}

// Rest of code unchanged
manager, _ := sdk.NewConversationManager(sdk.WithProvider(provider))
```

### 8. PromptPack-First

**Principle:** Configuration as data, not code.

**Why:**
- Non-developers can edit prompts
- Version control prompts separately
- A/B test different prompts
- Deploy prompt changes without code

**Pack Example:**
```json
{
  "version": "1.0",
  "prompts": {
    "assistant": {
      "system_prompt": "You are helpful...",
      "model_config": {
        "temperature": 0.7,
        "max_tokens": 1000
      },
      "available_tools": ["search"]
    }
  }
}
```

**Benefits:**
- Prompts live with application
- Type-safe loading
- Validated at load time
- Hot-reload capable

## API Consistency

### Naming Conventions

**Constructors:**
```go
NewConversationManager()  // Create new manager
NewConversation()         // Create new conversation
NewPipelineBuilder()      // Create new builder
```

**Actions:**
```go
Send()        // Synchronous action
SendStream()  // Streaming action
Load()        // Load from storage
Get()         // Retrieve existing
Set()         // Update existing
Delete()      // Remove existing
```

**Configuration:**
```go
WithProvider()      // Configure component
WithStateStore()    // Configure component
WithToolRegistry()  // Configure component
```

### Method Signatures

**Consistent Patterns:**
```go
// Create operations
func New*(ctx context.Context, deps..., config Config) (*Type, error)

// Action operations
func (t *Type) Action(ctx context.Context, params...) (*Result, error)

// Query operations
func (t *Type) Get(ctx context.Context, id string) (*Entity, error)
```

## Type Design

### Struct Tags

Use struct tags for serialization:

```go
type Message struct {
    Role      string    `json:"role"`
    Content   string    `json:"content"`
    Timestamp time.Time `json:"timestamp"`
}
```

### Embedded Interfaces

Compose with interfaces:

```go
type Provider interface {
    Completer
    Streamer
    Namer
}

type Completer interface {
    Complete(ctx context.Context, req *Request) (*Response, error)
}

type Streamer interface {
    CompleteStream(ctx context.Context, req *Request) (<-chan *StreamChunk, error)
}
```

### Pointer vs Value

**Pointers for:**
- Large structs
- Mutable types
- Optional fields

**Values for:**
- Small structs
- Immutable types
- Enum-like types

```go
// Pointer - mutable, large
type Conversation struct { /* ... */ }
func (c *Conversation) Send(ctx context.Context, msg string) (*Response, error)

// Value - immutable, small
type Role string
func (r Role) String() string
```

## Backward Compatibility

### Versioning Strategy

**Pack Versioning:**
```json
{
  "version": "1.0",  // Semantic versioning
  "prompts": { /* ... */ }
}
```

**API Versioning:**
```go
// v1 - stable
import "github.com/AltairaLabs/PromptKit/sdk"

// v2 - major breaking changes (future)
import "github.com/AltairaLabs/PromptKit/sdk/v2"
```

### Adding Features

**Good: Backward compatible**
```go
// Add optional parameter
func WithNewFeature(enabled bool) ManagerOption {
    return func(cfg *ManagerConfig) {
        cfg.NewFeature = enabled
    }
}
```

**Bad: Breaking change**
```go
// Changed signature - breaks existing code
func NewConversationManager(provider Provider, newParam string) (*Manager, error)
```

## Testing Considerations

### Testable Design

**Interfaces for Mocking:**
```go
type Provider interface {
    Complete(ctx context.Context, req *Request) (*Response, error)
}

// Mock for testing
type MockProvider struct {
    Response *Response
    Error    error
}

func (m *MockProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
    return m.Response, m.Error
}
```

**Dependency Injection:**
```go
// Inject dependencies
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(mockProvider),
    sdk.WithStateStore(mockStore),
)
```

## Performance Considerations

### Zero Allocations Where Possible

**Connection Pooling:**
```go
// Providers reuse HTTP connections
provider := providers.NewOpenAIProvider(key, model, false)
```

**Buffer Reuse:**
```go
// Reuse buffers in hot paths
var bufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}
```

### Efficient Data Structures

**Use Appropriate Types:**
```go
// Map for fast lookup
type ToolRegistry struct {
    tools map[string]*Tool  // O(1) lookup
}

// Slice for ordered iteration
type State struct {
    Messages []Message  // Preserve order
}
```

## Documentation Philosophy

### Self-Documenting Code

**Clear Naming:**
```go
// Good
func (c *Conversation) Send(ctx context.Context, message string) (*Response, error)

// Bad
func (c *Conversation) Exec(ctx context.Context, m string) (*Resp, error)
```

**Package Comments:**
```go
// Package sdk provides a high-level API for building
// LLM-powered applications with PromptKit.
//
// Example usage:
//
//     manager, _ := sdk.NewConversationManager(
//         sdk.WithProvider(provider),
//     )
//     pack, _ := manager.LoadPack("./assistant.pack.json")
//     conv, _ := manager.NewConversation(ctx, pack, config)
//     response, _ := conv.Send(ctx, "Hello")
//
package sdk
```

## Trade-offs Made

### Flexibility vs Simplicity

**Choice:** Two API levels (high/low)
**Trade-off:** More API surface, but better ergonomics

### Type Safety vs Dynamism

**Choice:** Strongly typed API
**Trade-off:** More verbose, but compile-time safety

### Performance vs Convenience

**Choice:** Convenience first, optimize later
**Trade-off:** Some overhead, but much easier to use

## Future-Proofing

### Extension Points

- **Middleware** - Add custom processing
- **Validators** - Add custom validation
- **State Stores** - Add custom storage
- **Providers** - Add new LLM providers

### Versioning Strategy

- Semantic versioning (MAJOR.MINOR.PATCH)
- Breaking changes only in major versions
- Deprecation warnings before removal
- Migration guides for breaking changes

## See Also

- [SDK Architecture](architecture)
- [Pipeline Architecture](pipeline-architecture)
- [PromptPack Format](promptpack-format)
