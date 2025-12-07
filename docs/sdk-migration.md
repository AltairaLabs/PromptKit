# SDK v2 Migration Guide

This guide helps you migrate from the current SDK to the new pack-first SDK v2.

## Overview

SDK v2 is a complete redesign that reduces boilerplate by ~80% while maintaining full functionality. The pack file becomes the single source of truth for prompts, tools, validators, and pipeline configuration.

**Key Changes:**
- Pack-first design: Load configuration from pack files
- Simplified API: 5 lines for hello world, 10 for tools
- Built-in streaming, validation, and tool handling
- Automatic provider setup from pack configuration

## Quick Comparison

### Before (Current SDK)

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
)

// 1. Create provider manually
provider := openai.NewToolProvider("openai", "gpt-4o", "https://api.openai.com/v1", nil, false, nil)

// 2. Create tool registry manually
toolRegistry := tools.NewRegistry()
toolRegistry.RegisterExecutor(&myExecutor{})
toolRegistry.Register(&tools.ToolDescriptor{
    Name: "get_weather",
    Description: "Get weather",
    InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}}}`),
    Mode: "local",
})

// 3. Create manager with options
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithToolRegistry(toolRegistry),
)

// 4. Load pack and create conversation
pack, _ := manager.LoadPack("./assistant.pack.json")
conv, _ := manager.CreateConversation(ctx, pack, sdk.ConversationConfig{
    PromptName: "chat",
    Variables: map[string]string{"user_name": "Alice"},
})

// 5. Send message
resp, _ := conv.Send(ctx, "Hello!")
fmt.Println(resp.Content)
```

### After (SDK v2)

```go
import "github.com/AltairaLabs/PromptKit/sdk"

// Everything in one call - provider, tools, config all from pack
conv, _ := sdk.Open("./assistant.pack.json", "chat")
defer conv.Close()

conv.SetVar("user_name", "Alice")

resp, _ := conv.Send(ctx, "Hello!")
fmt.Println(resp.Text())
```

## Migration Steps

### Step 1: Update Imports

**Before:**
```go
import (
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
    "github.com/AltairaLabs/PromptKit/runtime/types"
)
```

**After:**
```go
import "github.com/AltairaLabs/PromptKit/sdk"

// Optional sub-packages for advanced features:
import "github.com/AltairaLabs/PromptKit/sdk/tools"   // Typed handlers, HTTP tools
import "github.com/AltairaLabs/PromptKit/sdk/stream"  // Advanced streaming
import "github.com/AltairaLabs/PromptKit/sdk/hooks"   // Event subscriptions
```

### Step 2: Replace Conversation Creation

**Before:**
```go
manager, _ := sdk.NewConversationManager(opts...)
pack, _ := manager.LoadPack(packPath)
conv, _ := manager.CreateConversation(ctx, pack, config)
```

**After:**
```go
conv, _ := sdk.Open(packPath, promptName, opts...)
defer conv.Close()
```

### Step 3: Update Message Sending

**Before:**
```go
resp, _ := conv.Send(ctx, &types.Message{
    Role: "user",
    Content: []types.ContentPart{{Type: "text", Text: "Hello"}},
})
fmt.Println(resp.Content)
```

**After:**
```go
resp, _ := conv.Send(ctx, "Hello!")
fmt.Println(resp.Text())
```

### Step 4: Update Tool Handlers

**Before:**
```go
type MyExecutor struct{}

func (e *MyExecutor) Name() string { return "local" }

func (e *MyExecutor) Execute(desc *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
    var input MyInput
    json.Unmarshal(args, &input)
    result := doSomething(input)
    return json.Marshal(result)
}

toolRegistry.RegisterExecutor(&MyExecutor{})
```

**After:**
```go
conv.OnTool("my_tool", func(args map[string]any) (any, error) {
    city := args["city"].(string)
    return doSomething(city), nil
})

// Or with typed arguments:
tools.OnTyped(conv, "my_tool", func(args MyInput) (any, error) {
    return doSomething(args), nil
})
```

### Step 5: Update Streaming

**Before:**
```go
ch, _ := conv.StreamSend(ctx, msg)
for chunk := range ch {
    if chunk.Error != nil {
        break
    }
    fmt.Print(chunk.Content)
}
```

**After:**
```go
for chunk := range conv.Stream(ctx, "Tell me a story") {
    if chunk.Error != nil {
        break
    }
    if chunk.Type == sdk.ChunkDone {
        break
    }
    fmt.Print(chunk.Text)
}
```

### Step 6: Update Variable Handling

**Before:**
```go
config := sdk.ConversationConfig{
    Variables: map[string]string{
        "user_name": "Alice",
        "context": "support",
    },
}
conv, _ := manager.CreateConversation(ctx, pack, config)
```

**After:**
```go
conv, _ := sdk.Open(packPath, "chat")
conv.SetVar("user_name", "Alice")
conv.SetVar("context", "support")

// Or bulk set:
conv.SetVars(map[string]any{
    "user_name": "Alice",
    "context": "support",
})
```

## New Features in v2

### Human-in-the-Loop (HITL)

Tools requiring approval:

```go
conv.OnToolAsync("process_refund",
    // Check if approval needed
    func(args map[string]any) tools.PendingResult {
        if args["amount"].(float64) > 1000 {
            return tools.PendingResult{
                Reason: "high_value",
                Message: "Requires supervisor approval",
            }
        }
        return tools.PendingResult{} // Auto-approve
    },
    // Execute after approval
    func(args map[string]any) (any, error) {
        return processRefund(args)
    },
)

resp, _ := conv.Send(ctx, "Refund $5000")
if len(resp.PendingTools()) > 0 {
    // Get human approval...
    conv.ResolveTool(resp.PendingTools()[0].ID)
}
```

### MCP Integration

Connect to MCP servers:

```go
conv, _ := sdk.Open(packPath, "chat",
    sdk.WithMCPServer("filesystem", "npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"),
)
// Tools from MCP server are automatically available
```

### HTTP Tools

Call external APIs:

```go
conv.OnToolHTTP("create_ticket", tools.NewHTTPToolConfig(
    "https://api.tickets.com/create",
    tools.WithMethod("POST"),
    tools.WithHeader("Authorization", "Bearer "+token),
    tools.WithTimeout(5000),
))
```

### Event Hooks

Subscribe to pipeline events:

```go
hooks.OnToolCall(conv, func(name string, args map[string]any) {
    log.Printf("Tool called: %s", name)
})

hooks.OnValidation(conv, func(field, message string) {
    log.Printf("Validation failed: %s - %s", field, message)
})
```

## Pack File Updates

SDK v2 reads all configuration from the pack file. Make sure your pack includes:

```json
{
  "id": "my-assistant",
  "version": "1.0.0",
  "provider": {
    "name": "openai",
    "model": "gpt-4o",
    "base_url": "https://api.openai.com/v1",
    "env_var": "OPENAI_API_KEY"
  },
  "prompts": {
    "chat": {
      "id": "chat",
      "system_template": "You are {{role}}. Help {{user_name}}."
    }
  },
  "tools": {
    "get_weather": {
      "name": "get_weather",
      "description": "Get weather for a city",
      "parameters": {
        "type": "object",
        "properties": {
          "city": {"type": "string"}
        },
        "required": ["city"]
      }
    }
  }
}
```

## API Reference

### Core Functions

| Function | Description |
|----------|-------------|
| `sdk.Open(pack, prompt, opts...)` | Open a conversation |
| `sdk.Resume(pack, prompt, state, opts...)` | Resume from saved state |
| `conv.Send(ctx, message, opts...)` | Send a message |
| `conv.Stream(ctx, message, opts...)` | Stream a response |
| `conv.Close()` | Close and cleanup |

### Variable Methods

| Method | Description |
|--------|-------------|
| `conv.SetVar(name, value)` | Set a single variable |
| `conv.SetVars(map)` | Set multiple variables |
| `conv.SetVarsFromEnv(prefix)` | Load from environment |
| `conv.GetVar(name)` | Get variable value |

### Tool Methods

| Method | Description |
|--------|-------------|
| `conv.OnTool(name, handler)` | Register tool handler |
| `conv.OnToolCtx(name, handler)` | Handler with context |
| `conv.OnTools(handlers)` | Register multiple |
| `conv.OnToolAsync(name, check, exec)` | HITL tool handler |
| `conv.OnToolHTTP(name, config)` | HTTP tool |
| `conv.OnToolExecutor(name, executor)` | Custom executor |

### HITL Methods

| Method | Description |
|--------|-------------|
| `conv.ResolveTool(id)` | Approve pending tool |
| `conv.RejectTool(id, reason)` | Reject pending tool |
| `resp.PendingTools()` | Get pending tools |

### Options

| Option | Description |
|--------|-------------|
| `sdk.WithEventBus(bus)` | Share event bus |
| `sdk.WithMCP(registry)` | Use MCP registry |
| `sdk.WithMCPServer(name, cmd, args...)` | Add MCP server |

## Troubleshooting

### "provider not found"

Make sure your pack file has a valid `provider` section with the correct `name` (openai, anthropic, gemini, etc.).

### "prompt not found"

Check that the prompt name passed to `sdk.Open()` matches a key in your pack's `prompts` section.

### "tool handler not registered"

Register handlers for all tools defined in your pack before calling `Send()`:

```go
conv.OnTool("get_weather", weatherHandler)
conv.OnTool("search", searchHandler)
```

### Race conditions

SDK v2 is thread-safe. If you see race conditions, ensure you're not sharing `*Response` objects across goroutines without synchronization.

## Getting Help

- [Examples](../sdk/examples/) - Working code examples
- [API Documentation](https://pkg.go.dev/github.com/AltairaLabs/PromptKit/sdk) - Full API reference
- [GitHub Issues](https://github.com/AltairaLabs/PromptKit/issues) - Report bugs or request features
