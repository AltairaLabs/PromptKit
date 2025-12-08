---
title: SDK Architecture
docType: explanation
order: 1
---
# SDK v2 Architecture

Understanding the pack-first design and component relationships.

## Design Principles

1. **Pack-First** - Pack file is the single source of truth
2. **Minimal Boilerplate** - 5 lines to hello world
3. **Type Safety** - Leverage Go's type system
4. **Thread Safe** - All operations are concurrent-safe
5. **Extensible** - Hooks and custom handlers

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                    Application                           │
│                                                          │
│   conv, _ := sdk.Open("pack.json", "chat")              │
│   resp, _ := conv.Send(ctx, "Hello")                    │
│                                                          │
└─────────────────────────────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│                    SDK Layer                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │ PackLoader   │  │ Conversation │  │    Hooks     │  │
│  │              │  │              │  │              │  │
│  │ - Load pack  │  │ - Variables  │  │ - Events     │  │
│  │ - Validate   │  │ - Tools      │  │ - Subscribe  │  │
│  │ - Cache      │  │ - Messages   │  │              │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│                   Runtime Layer                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  Providers   │  │   Pipeline   │  │    Tools     │  │
│  │              │  │              │  │              │  │
│  │ - OpenAI     │  │ - Execute    │  │ - Registry   │  │
│  │ - Anthropic  │  │ - Stream     │  │ - Handlers   │  │
│  │ - Google     │  │ - Context    │  │ - HTTP       │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
```

## Core Components

### PackLoader

Loads and validates pack files:

```go
// Internal - used by sdk.Open()
loader := packloader.New()
pack, err := loader.Load("./app.pack.json")
```

**Responsibilities:**
- Parse JSON pack files
- Validate schema
- Cache loaded packs
- Resolve prompt references

### Conversation

The main interaction point:

```go
type Conversation struct {
    pack      *pack.Pack
    prompt    *pack.Prompt
    state     *ConversationState
    handlers  map[string]ToolHandler
    eventBus  *events.EventBus
    // ...
}
```

**Responsibilities:**
- Manage variables
- Register tool handlers
- Execute messages
- Maintain history
- Emit events

### Hooks (EventBus)

Observability through events:

```go
type Event struct {
    Type      string
    Timestamp time.Time
    Data      map[string]any
}
```

**Event Flow:**
1. `conv.Send()` emits `EventSend`
2. Provider call executes
3. Response emits `EventResponse`
4. Tool calls emit `EventToolCall`
5. Errors emit `EventError`

## Request Flow

```
conv.Send(ctx, "Hello")
        │
        ▼
┌───────────────────┐
│ Emit EventSend    │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│ Build Context     │
│ - System prompt   │
│ - Variables       │
│ - History         │
│ - Tool defs       │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│ Provider Call     │
│ (OpenAI, etc.)    │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│ Process Response  │
│ - Tool calls?     │
│ - Update history  │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│ Emit EventResponse│
└─────────┬─────────┘
          │
          ▼
     Return Response
```

## Tool Execution

```
LLM Response with tool_call
          │
          ▼
┌───────────────────┐
│ Lookup Handler    │
│ conv.handlers[name]│
└─────────┬─────────┘
          │
    ┌─────┴─────┐
    │           │
    ▼           ▼
OnTool      OnToolAsync
    │           │
    │     ┌─────┴─────┐
    │     │           │
    │     ▼           ▼
    │  Auto-OK    Pending
    │     │           │
    ▼     ▼           ▼
Execute Handler   Wait for
    │             Resolve/Reject
    ▼                 │
Emit EventToolResult  │
    │                 │
    ▼─────────────────┘
Continue conversation
```

## State Management

### Variable Storage

```go
type ConversationState struct {
    mu        sync.RWMutex
    variables map[string]any
    messages  []types.Message
}
```

Thread-safe access:
- `SetVar()` acquires write lock
- `GetVar()` acquires read lock
- Concurrent access safe

### Message History

```go
// Append message
state.AddMessage(types.Message{
    Role:    "user",
    Content: "Hello",
})

// Get all messages
messages := state.GetMessages()
```

## Thread Safety

All Conversation methods are thread-safe:

```go
// Safe concurrent access
var wg sync.WaitGroup

for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(n int) {
        defer wg.Done()
        conv.SetVar(fmt.Sprintf("key_%d", n), n)
    }(i)
}

wg.Wait()
```

## See Also

- [Observability](observability)
- [API Reference](../reference/)
