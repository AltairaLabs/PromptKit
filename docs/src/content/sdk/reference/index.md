---
title: SDK Reference
docType: reference
order: 1
---
# SDK API Reference

Complete reference documentation for the PromptKit SDK Go API.

## Overview

The SDK provides two API levels for building LLM applications:

### High-Level API

For common use cases with minimal boilerplate:

- **[ConversationManager](conversation-manager)** - Manage multi-turn conversations
- **[Conversation](conversation)** - Individual conversation instances
- **[PackManager](pack-manager)** - Load and manage PromptPacks

### Low-Level API

For advanced customization and control:

- **[PipelineBuilder](pipeline-builder)** - Build custom pipelines
- **[ToolRegistry](tool-registry)** - Register and manage tools
- **[Middleware](middleware)** - Custom middleware interfaces

### Core Types

Shared types and structures:

- **[Pack Format](pack-format)** - PromptPack JSON structure
- **[Types](types)** - Core type definitions
- **[Errors](errors)** - Error types and handling

## Quick Reference

### ConversationManager

```go
// Create manager
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithStateStore(stateStore),
    sdk.WithToolRegistry(registry),
)

// Load pack
pack, _ := manager.LoadPack("./support.pack.json")

// Create conversation
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:     "user123",
    PromptName: "support",
    Variables:  map[string]interface{}{"role": "agent"},
})

// Send message
resp, _ := conv.Send(ctx, "Hello")

// Stream message
ch, _ := conv.SendStream(ctx, "Tell me a story")
for event := range ch {
    fmt.Print(event.Chunk.Text)
}
```

### PipelineBuilder

```go
// Build custom pipeline
pipe := sdk.NewPipelineBuilder().
    WithMiddleware(&MyMiddleware{}).
    WithTemplate().
    WithProvider(provider, registry, toolPolicy).
    Build()

// Execute
result, _ := pipe.Execute(ctx, "user", "Hello!")
```

### PackManager

```go
// Load pack
pm := sdk.NewPackManager()
pack, _ := pm.LoadPack("./prompts.pack.json")

// Get prompt
prompt, _ := pack.GetPrompt("support")

// List prompts
names := pack.ListPrompts()

// Get tools
tools := pack.GetTools()

// Create registry
registry := pack.CreateRegistry()
```

## Package Import

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
)
```

## API Documentation

### By Category

**Conversation Management:**
- [ConversationManager](conversation-manager) - High-level conversation API
- [Conversation](conversation) - Conversation instance methods
- [SendOptions](types.md#sendoptions) - Configure message sending
- [Response](types.md#response) - Conversation response type

**PromptPack Management:**
- [PackManager](pack-manager) - Load and validate packs
- [Pack](pack-format) - Pack structure
- [Prompt](pack-format.md#prompt) - Prompt configuration
- [Tool](pack-format.md#tool) - Tool definition

**Pipeline Construction:**
- [PipelineBuilder](pipeline-builder) - Build pipelines
- [Middleware](middleware) - Custom middleware
- [ToolRegistry](tool-registry) - Tool registration

**Configuration:**
- [ManagerConfig](conversation-manager.md#managerconfig) - Manager settings
- [ConversationConfig](conversation.md#conversationconfig) - Conversation settings
- [ContextBuilderPolicy](types.md#contextbuilderpolicy) - Context management

**State Management:**
- [StateStore](types.md#statestore) - State persistence interface
- [ConversationState](types.md#conversationstate) - State structure

**Error Handling:**
- [Error Types](errors) - SDK error types
- [Error Helpers](errors.md#helpers) - Error utilities

## Common Patterns

### Pattern: Simple Conversation

```go
func simpleConversation() {
    provider := providers.NewOpenAIProvider("key", "gpt-4o-mini", false)
    
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(provider),
    )
    
    pack, _ := manager.LoadPack("./support.pack.json")
    
    conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "support",
    })
    
    resp, _ := conv.Send(ctx, "How can I return an item?")
    fmt.Println(resp.Content)
}
```

### Pattern: Streaming with Tools

```go
func streamingWithTools() {
    registry := sdk.NewToolRegistry()
    registry.Register("search", searchTool)
    
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithToolRegistry(registry),
    )
    
    pack, _ := manager.LoadPack("./assistant.pack.json")
    conv, _ := manager.NewConversation(ctx, pack, config)
    
    ch, _ := conv.SendStream(ctx, "Search for latest news")
    for event := range ch {
        if event.Chunk != nil {
            fmt.Print(event.Chunk.Text)
        }
        if event.Error != nil {
            log.Printf("Error: %v", event.Error)
        }
    }
}
```

### Pattern: Custom Pipeline

```go
func customPipeline() {
    pipe := sdk.NewPipelineBuilder().
        WithMiddleware(&MetricsMiddleware{}).
        WithMiddleware(&LoggingMiddleware{}).
        WithTemplate().
        WithProvider(provider, registry, nil).
        Build()
    
    result, _ := pipe.Execute(ctx, "user", "Hello!")
    fmt.Println(result.Response.Content)
}
```

### Pattern: Persistent State

```go
func persistentState() {
    redisStore := statestore.NewRedisStore(redisClient)
    
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithStateStore(redisStore),
    )
    
    // State automatically persisted to Redis
    conv, _ := manager.NewConversation(ctx, pack, config)
    resp, _ := conv.Send(ctx, "Remember: my name is Alice")
    
    // Later: retrieve conversation
    retrieved, _ := manager.GetConversation(ctx, conv.ID())
    resp2, _ := retrieved.Send(ctx, "What's my name?")
    // Should reference "Alice" from previous turn
}
```

## Thread Safety

All SDK types are thread-safe for concurrent use:

- **ConversationManager**: Safe for multiple goroutines
- **Conversation**: Safe for concurrent message sends
- **Pack**: Read-only after load, safe to share
- **PackManager**: Thread-safe pack management

```go
// Safe concurrent usage
manager, _ := sdk.NewConversationManager(...)
pack, _ := manager.LoadPack("./prompts.pack.json")

// Multiple goroutines can create conversations
var wg sync.WaitGroup
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        conv, _ := manager.NewConversation(ctx, pack, config)
        resp, _ := conv.Send(ctx, "Hello")
    }(i)
}
wg.Wait()
```

## Performance Considerations

### Memory Management

```go
// Reuse packs across conversations
pack, _ := manager.LoadPack("./prompts.pack.json")

// Create multiple conversations from same pack (efficient)
for i := 0; i < 100; i++ {
    conv, _ := manager.NewConversation(ctx, pack, config)
    // Use conversation...
}
```

### Connection Pooling

```go
// StateStore handles connection pooling internally
redisStore := statestore.NewRedisStore(redisClient) // Pooled

// Provider manages HTTP client pooling
provider := providers.NewOpenAIProvider(...) // Pooled
```

### Context Management

```go
// Configure token budget for context
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    ContextPolicy: &middleware.ContextBuilderPolicy{
        MaxInputTokens: 8000,  // Limit context size
        Strategy:       middleware.StrategyTruncateOldest,
    },
})
```

## Error Handling

```go
// Check for specific error types
resp, err := conv.Send(ctx, "Hello")
if err != nil {
    if sdk.IsRetryableError(err) {
        // Retry logic
        resp, err = conv.Send(ctx, "Hello")
    } else if sdk.IsTemporaryError(err) {
        // Wait and retry
        time.Sleep(time.Second)
        resp, err = conv.Send(ctx, "Hello")
    } else {
        // Fatal error
        return err
    }
}
```

## Versioning

The SDK follows semantic versioning:

```go
import "github.com/AltairaLabs/PromptKit/sdk"

// Current version
version := sdk.Version // "1.0.0"
```

Compatible with:
- PromptPack format v1.0+
- Runtime pipeline v1.0+
- PackC compiler v1.0+

## Next Steps

- **[ConversationManager Reference](conversation-manager)** - Start with high-level API
- **[How-To Guides](../how-to/)** - Task-focused guides
- **[Tutorials](../tutorials/)** - Step-by-step learning
- **[Go Package Documentation](https://pkg.go.dev/github.com/AltairaLabs/PromptKit/sdk)** - Full API docs
