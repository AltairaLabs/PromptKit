---
title: SDK Explanation
sidebar:
  order: 0
---
Deep-dive documentation explaining SDK v2 architecture and design.

## Architecture

- **[SDK Architecture](architecture)** - Pack-first design and components
- **[Observability](observability)** - Event system and monitoring

## Design Philosophy

### Pack-First Architecture

SDK v2 is built around the pack file as the single source of truth:

```
┌─────────────────┐
│   Pack File     │  ← Configuration, prompts, tools
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   sdk.Open()    │  ← Load and validate
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Conversation   │  ← Ready to use
└─────────────────┘
```

### Why Pack-First?

1. **Reduced Boilerplate** - No manual provider/manager setup
2. **Tested Configuration** - Packs are validated by Arena
3. **Portable** - Same pack works across environments
4. **Versioned** - Pack files are version controlled

### Before (v1)

```go
provider := providers.NewOpenAIProvider(apiKey, model, false)
manager, _ := sdk.NewConversationManager(sdk.WithProvider(provider))
pack, _ := manager.LoadPack("./pack.json")
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:     "user123",
    PromptName: "chat",
})
resp, _ := conv.Send(ctx, "Hello")
```

### After (v2)

```go
conv, _ := sdk.Open("./pack.json", "chat")
defer conv.Close()
resp, _ := conv.Send(ctx, "Hello")
```

## Key Concepts

### Conversation Lifecycle

1. **Open** - `sdk.Open()` loads pack, creates conversation
2. **Configure** - `SetVar()`, `OnTool()` setup
3. **Use** - `Send()`, `Stream()` interactions
4. **Close** - `Close()` cleanup

### Tool Execution

Tools are registered with handlers:

```
LLM Request
    │
    ▼
Tool Call Decision
    │
    ▼
Handler Lookup
    │
    ├─► OnTool handler → Execute immediately
    │
    └─► OnToolAsync handler
            │
            ├─► Auto-approve → Execute
            │
            └─► Pending → Wait for ResolveTool/RejectTool
```

### Event System

Events flow through the hooks package:

```
Send() ─────► EventSend
    │
    ▼
Provider Call
    │
    ▼
Response ───► EventResponse
    │
    ├─► Tool Call ───► EventToolCall
    │       │
    │       ▼
    │   Handler
    │       │
    │       ▼
    │   EventToolResult
    │
    └─► Error ───► EventError
```

## See Also

- [Tutorials](../tutorials/)
- [How-To Guides](../how-to/)
- [API Reference](../reference/)
