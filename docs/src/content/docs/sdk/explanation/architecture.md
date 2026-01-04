---
title: SDK Architecture
sidebar:
  order: 1
---
Understanding the pack-first design and component relationships.

## Design Principles

1. **Pack-First** - Pack file is the single source of truth
2. **Minimal Boilerplate** - 5 lines to hello world
3. **Type Safety** - Leverage Go's type system
4. **Thread Safe** - All operations are concurrent-safe
5. **Extensible** - Hooks and custom handlers

## Architecture Overview

```d2
direction: down

Application: {
  label: "Application\n\nconv, _ := sdk.Open(\"pack.json\", \"chat\")\nresp, _ := conv.Send(ctx, \"Hello\")"
}

SDK Layer: {
  PackLoader: {
    label: "PackLoader\n- Load pack\n- Validate\n- Cache"
  }
  Conversation: {
    label: "Conversation\n- Variables\n- Tools\n- Messages"
  }
  Hooks: {
    label: "Hooks\n- Events\n- Subscribe"
  }
}

Runtime Layer: {
  Providers: {
    label: "Providers\n- OpenAI\n- Anthropic\n- Google"
  }
  Pipeline: {
    label: "Pipeline\n- Execute\n- Stream\n- Context"
  }
  Tools: {
    label: "Tools\n- Registry\n- Handlers\n- HTTP"
  }
}

Application -> SDK Layer -> Runtime Layer
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

```d2
direction: down

send: conv.Send(ctx, "Hello")
emit_send: Emit EventSend
build: Build Context {
  label: "Build Context\n- System prompt\n- Variables\n- History\n- Tool defs"
}
provider: Provider Call (OpenAI, etc.)
process: Process Response {
  label: "Process Response\n- Tool calls?\n- Update history"
}
emit_response: Emit EventResponse
return: Return Response

send -> emit_send -> build -> provider -> process -> emit_response -> return
```

## Tool Execution

```d2
direction: down

response: LLM Response with tool_call
lookup: Lookup Handler {
  label: "Lookup Handler\nconv.handlers[name]"
}

on_tool: OnTool
on_tool_async: OnToolAsync
auto_ok: Auto-OK
pending: Pending
execute: Execute Handler
wait: Wait for Resolve/Reject
emit: Emit EventToolResult
continue: Continue conversation

response -> lookup
lookup -> on_tool
lookup -> on_tool_async
on_tool -> execute
on_tool_async -> auto_ok
on_tool_async -> pending
auto_ok -> execute
execute -> emit
pending -> wait
wait -> continue
emit -> continue
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
