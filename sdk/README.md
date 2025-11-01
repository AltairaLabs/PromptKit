# PromptKit SDK

High-level Go SDK for building LLM applications with PromptKit. The SDK provides two API levels:

- **High-Level API** (`ConversationManager`): Simple, opinionated interface for common use cases
- **Low-Level API** (`PipelineBuilder`): Full control over pipeline construction and middleware

## Features

✅ **PromptPack-First**: Load compiled `.pack.json` files with prompts, tools, and validators  
✅ **Full Pipeline Integration**: Uses PromptKit's pipeline architecture with middleware  
✅ **State Persistence**: Built-in support for Redis, Postgres, or in-memory state stores  
✅ **Multi-Turn Conversations**: Automatic conversation history management  
✅ **Tool Execution**: Register and execute tools with LLM guidance  
✅ **Custom Middleware**: Inject custom logic for context building, observability, etc.  
✅ **Thread-Safe**: Designed for concurrent multi-tenant web APIs  
✅ **Provider Agnostic**: Works with OpenAI, Claude, Gemini, and custom providers  

## Installation

```bash
go get github.com/AltairaLabs/PromptKit/sdk
```

## Quick Start

### High-Level API

```go
// 1. Create provider
provider := providers.NewOpenAIProvider("your-api-key", "gpt-4", false)

// 2. Create manager
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
)

// 3. Load pack
pack, _ := manager.LoadPack("./support.pack.json")

// 4. Create conversation
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:     "user123",
    PromptName: "support",
    Variables: map[string]interface{}{
        "role": "customer support",
    },
})

// 5. Send messages
resp, _ := conv.Send(ctx, "I need help")
fmt.Printf("Assistant: %s (Cost: $%.4f)\n", resp.Content, resp.Cost)
```

### Low-Level API

```go
// Build custom pipeline with middleware
pipe := sdk.NewPipelineBuilder().
    WithMiddleware(&MyCustomMiddleware{}).
    WithSimpleProvider(provider).  // Simple provider (no tools)
    Build()

// Or use full provider with tools
registry := sdk.NewToolRegistry()
registry.Register("search", searchTool)

pipe := sdk.NewPipelineBuilder().
    WithProvider(provider, registry, nil).  // Provider with tools
    WithTemplate().  // Add template substitution
    Build()

// Execute
result, _ := pipe.Execute(ctx, "user", "Hello!")
fmt.Println(result.Response.Content)
```

**Convenience Methods:**

- `WithSimpleProvider(provider)` - Provider without tool support
- `WithProvider(provider, registry, policy)` - Provider with tools and execution policy
- `WithTemplate()` - Template variable substitution ({{variable}})
- `WithMiddleware(m)` - Add custom middleware

These methods leverage battle-tested middleware from `runtime/pipeline/middleware`.

## Examples

See [examples/](examples/) directory:

- [basic/](examples/basic/) - Simple conversation
- [custom-middleware/](examples/custom-middleware/) - Custom middleware with metrics and logging

Run examples:

```bash
cd examples/custom-middleware
go run main.go
```

## Documentation

- **[Full API Documentation](https://pkg.go.dev/github.com/AltairaLabs/PromptKit/sdk)**
- **[Design Proposal](../docs/sdk-design-proposal.md)** - Architecture and design decisions
- **[Pack Format Spec](../docs/pack-format-spec.md)** - PromptPack file format

## Key Concepts

### PromptPacks

Compiled JSON files containing prompts, variables, tools, and validators:

```bash
packc compile -c prompts/ -o support.pack.json
```

### State Persistence

```go
// In-memory (default)
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
)

// Redis
redisStore := statestore.NewRedisStore(...)
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithStateStore(redisStore),
)
```

### Custom Middleware

```go
type MetricsMiddleware struct{}

func (m *MetricsMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
    start := time.Now()
    err := next()
    recordMetric("duration", time.Since(start))
    recordMetric("tokens", execCtx.CostInfo.InputTokens + execCtx.CostInfo.OutputTokens)
    return err
}

func (m *MetricsMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
    return nil
}

// Use it
pipe := sdk.NewPipelineBuilder().
    WithMiddleware(&MetricsMiddleware{}).
    WithSimpleProvider(provider).
    Build()
```

## Testing

```bash
cd sdk
go test -v ./...
```

**Test Results:**
- ✅ PackManager: 12/12 tests passing
- ✅ ConversationManager: 4/4 tests passing
- ✅ PipelineBuilder: 4/4 tests passing
- ✅ ToolRegistry: Included in conversation tests

## Architecture

The SDK is built on PromptKit's runtime components:

```
┌─────────────────────────────────────────┐
│           SDK (High-Level)              │
│  ┌─────────────────────────────────┐   │
│  │   ConversationManager           │   │
│  │   - Load PromptPacks            │   │
│  │   - Create conversations        │   │
│  │   - Auto pipeline construction  │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────┐
│          SDK (Low-Level)                │
│  ┌─────────────────────────────────┐   │
│  │   PipelineBuilder               │   │
│  │   - Custom middleware           │   │
│  │   - Full pipeline control       │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────┐
│         Runtime Components              │
│  - Pipeline & Middleware                │
│  - Providers (OpenAI/Claude/Gemini)     │
│  - StateStore (Redis/Postgres/Memory)   │
│  - Tools, Validators, Types             │
└─────────────────────────────────────────┘
```

## License

MIT License
